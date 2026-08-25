package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// --- Input / output types ---

type UpdateHandleInput struct {
	Body struct {
		Handle string `json:"handle" minLength:"3" maxLength:"64"`
		Bio    string `json:"bio,omitempty" maxLength:"500"`
	}
}

type UserProfileOutput struct {
	Body store.UserProfile
}

type UserProfileListOutput struct {
	Body []store.UserProfile
}

type FollowInput struct {
	Handle string `path:"handle"`
}

type ShareArticleInput struct {
	Body struct {
		ArticleURL  string     `json:"article_url" minLength:"1" maxLength:"2048"`
		Title       string     `json:"title" maxLength:"1024"`
		Description string     `json:"description,omitempty" maxLength:"1000"`
		FeedURL     string     `json:"feed_url,omitempty" maxLength:"2048"`
		FeedTitle   string     `json:"feed_title,omitempty" maxLength:"512"`
		FeedSiteURL string     `json:"feed_site_url,omitempty" maxLength:"2048"`
		Author      string     `json:"author,omitempty" maxLength:"255"`
		PublishedAt *time.Time `json:"published_at,omitempty"`
	}
}

type SharedArticleOutput struct {
	Body store.SharedArticle
}

type SharedArticleListOutput struct {
	Body []store.SharedArticle
}

type PublicProfileResponse struct {
	Profile              store.UserProfile     `json:"profile"`
	GlobalFollowerCount  int                   `json:"global_follower_count"`
	GlobalFollowingCount int                   `json:"global_following_count"`
	SharedArticles       []store.SharedArticle `json:"shared_articles"`
	Feeds                []store.Feed          `json:"feeds"`
}

type PublicProfileOutput struct {
	Body PublicProfileResponse
}

type FeedSubscribersResponse struct {
	Count       int                 `json:"count"`
	GlobalCount int                 `json:"global_count"`
	Subscribers []store.UserProfile `json:"subscribers"`
}

type FeedSubscribersOutput struct {
	Body FeedSubscribersResponse
}

// ArticleShareCountResponse is the body for the article-share-count endpoint.
type ArticleShareCountResponse struct {
	ArticleURL string `json:"article_url"`
	Count      int64  `json:"count"`
}

type ArticleShareCountOutput struct {
	Body ArticleShareCountResponse
}

// ArticleStarCountResponse is the body for the article-star-count endpoint.
type ArticleStarCountResponse struct {
	ArticleURL string `json:"article_url"`
	Count      int64  `json:"count"`
}

type ArticleStarCountOutput struct {
	Body ArticleStarCountResponse
}

// registerSocialRoutes wires all social-feature endpoints.
func (a *API) registerSocialRoutes() {
	// PATCH /api/v1/me/handle — set or update the caller's handle + bio
	huma.Register(a.huma, huma.Operation{
		OperationID: "update-handle",
		Method:      http.MethodPatch,
		Path:        "/api/v1/me/handle",
		Summary:     "Set or update your social handle",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *UpdateHandleInput) (*UserProfileOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		p, err := a.store.UpsertHandle(ctx, userID, input.Body.Handle, input.Body.Bio)
		if err != nil {
			if errors.Is(err, store.ErrHandleInvalid) {
				return nil, huma.Error422UnprocessableEntity(err.Error(), nil)
			}
			if errors.Is(err, store.ErrHandleTaken) {
				return nil, huma.Error409Conflict(err.Error())
			}
			return nil, huma.Error500InternalServerError(fmt.Errorf("upsert handle: %w", err).Error())
		}
		// Mirror the new bio (+ current display name) onto the Bluesky profile
		// record. Handle itself is AT Proto identity, updated separately.
		go a.ATProtoSyncProfile(userID, p.DisplayName, p.Bio, p.CreatedAt)
		return &UserProfileOutput{Body: *p}, nil
	})

	// GET /api/v1/users/{handle} — public profile (articles + feeds)
	huma.Register(a.huma, huma.Operation{
		OperationID: "get-user-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{handle}",
		Summary:     "Get a public user profile",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		Handle string `path:"handle"`
	}) (*PublicProfileOutput, error) {
		viewerID := auth.UserIDFromCtx(ctx)
		profile, err := a.store.GetProfileByHandle(ctx, input.Handle, viewerID)
		if err != nil {
			if errors.Is(err, store.ErrProfileNotFound) {
				return nil, huma.Error404NotFound("user not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}

		shared, err := a.store.ListSharedArticlesByUser(ctx, profile.UserID, viewerID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if shared == nil {
			shared = []store.SharedArticle{}
		}

		feeds, err := a.store.ListPublicFeedsByUser(ctx, profile.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feeds == nil {
			feeds = []store.Feed{}
		}

		return &PublicProfileOutput{Body: PublicProfileResponse{
			Profile:              *profile,
			GlobalFollowerCount:  int(a.relayGetUserFollowerCount(ctx, profile.DID)),
			GlobalFollowingCount: int(a.relayGetUserFollowingCount(ctx, profile.DID)),
			SharedArticles:       shared,
			Feeds:                feeds,
		}}, nil
	})

	// POST /api/v1/users/{handle}/follow — follow a user
	huma.Register(a.huma, huma.Operation{
		OperationID: "follow-user",
		Method:      http.MethodPost,
		Path:        "/api/v1/users/{handle}/follow",
		Summary:     "Follow a user",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *FollowInput) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.FollowUser(ctx, userID, input.Handle); err != nil {
			switch {
			case errors.Is(err, store.ErrProfileNotFound):
				// Try resolving via the relay for cross-instance follow.
				did, pdsURL := a.relayResolveHandle(ctx, input.Handle)
				if did == "" {
					return nil, huma.Error404NotFound("user not found")
				}
				followeeUserID, cerr := a.store.CreateRemoteUser(ctx, did, input.Handle, pdsURL)
				if cerr != nil {
					return nil, huma.Error500InternalServerError(fmt.Errorf("create remote user: %w", cerr).Error())
				}
				if ferr := a.store.FollowUser(ctx, userID, input.Handle); ferr != nil {
					return nil, huma.Error500InternalServerError(ferr.Error())
				}
				go a.ATProtoSyncFollow(userID, followeeUserID, input.Handle, true, "")
				go a.backfillFollowee(followeeUserID)
				return nil, nil
			case errors.Is(err, store.ErrCannotFollowSelf):
				return nil, huma.Error422UnprocessableEntity(err.Error(), nil)
			case errors.Is(err, store.ErrAlreadyFollowing):
				return nil, huma.Error409Conflict(err.Error())
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		// Resolve followee user ID for rkey tracking + share ingestion.
		followeeProfile, _ := a.store.GetProfileByHandle(ctx, input.Handle, 0)
		followeeUserID := 0
		if followeeProfile != nil {
			followeeUserID = followeeProfile.UserID
		}
		go a.ATProtoSyncFollow(userID, followeeUserID, input.Handle, true, "")
		// Backfill the followee's shares + feed subscriptions from their PDS so
		// they appear in the follower's timeline and on the followee's profile.
		// No-op if the followee has no AT Proto identity.
		if followeeUserID != 0 {
			go a.backfillFollowee(followeeUserID)
		}
		return nil, nil
	})

	// DELETE /api/v1/users/{handle}/follow — unfollow a user
	huma.Register(a.huma, huma.Operation{
		OperationID: "unfollow-user",
		Method:      http.MethodDelete,
		Path:        "/api/v1/users/{handle}/follow",
		Summary:     "Unfollow a user",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *FollowInput) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		// Resolve before deleting so we still have the followee profile + rkey.
		followeeProfile, _ := a.store.GetProfileByHandle(ctx, input.Handle, 0)
		followeeUserID := 0
		if followeeProfile != nil {
			followeeUserID = followeeProfile.UserID
		}
		// Fetch the rkey before deleting — the row is gone after UnfollowUser
		// and the fire-and-forget sync goroutine can't read it back.
		rkey, _ := a.store.GetFollowATProtoRkey(ctx, userID, followeeUserID)
		if err := a.store.UnfollowUser(ctx, userID, input.Handle); err != nil {
			if errors.Is(err, store.ErrProfileNotFound) {
				return nil, huma.Error404NotFound("user not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		go a.ATProtoSyncFollow(userID, followeeUserID, input.Handle, false, rkey)
		return nil, nil
	})

	// GET /api/v1/social/following — list users I follow
	huma.Register(a.huma, huma.Operation{
		OperationID: "list-following",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/following",
		Summary:     "List users you are following",
		Tags:        []string{"social"},
	}, func(ctx context.Context, _ *struct{}) (*UserProfileListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		profiles, err := a.store.ListFollowing(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if profiles == nil {
			profiles = []store.UserProfile{}
		}
		return &UserProfileListOutput{Body: profiles}, nil
	})

	// GET /api/v1/social/search — find users by handle or name
	huma.Register(a.huma, huma.Operation{
		OperationID: "search-users",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/search",
		Summary:     "Search users by handle or name",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		Query string `query:"q" minLength:"1" maxLength:"64"`
		Limit int    `query:"limit" default:"20" minimum:"1" maximum:"50"`
	}) (*UserProfileListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		profiles, err := a.store.SearchUsers(ctx, input.Query, userID, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if profiles == nil {
			profiles = []store.UserProfile{}
		}
		// Resolve the viewer's handle so remote relay results can't surface
		// the viewer themselves (local results already exclude them).
		viewer, err := a.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		// If local results are thin, fall back to the relay for cross-instance search.
		if len(profiles) < input.Limit {
			remaining := input.Limit - len(profiles)
			remote := a.relaySearchDIDs(ctx, input.Query, remaining)
			// Deduplicate by handle — skip the viewer, remote results already
			// in local results, or earlier in the remote batch.
			seen := make(map[string]bool, len(profiles)+1)
			if viewer != nil {
				seen[viewer.Handle] = true
			}
			for _, p := range profiles {
				seen[p.Handle] = true
			}
			for _, r := range remote {
				if !seen[r.Handle] {
					seen[r.Handle] = true
					profiles = append(profiles, r)
				}
			}
		}
		return &UserProfileListOutput{Body: profiles}, nil
	})

	// GET /api/v1/users/{handle}/followers — list followers of a user
	huma.Register(a.huma, huma.Operation{
		OperationID: "list-followers",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{handle}/followers",
		Summary:     "List followers of a user",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		Handle string `path:"handle"`
	}) (*UserProfileListOutput, error) {
		viewerID := auth.UserIDFromCtx(ctx)
		profile, err := a.store.GetProfileByHandle(ctx, input.Handle, viewerID)
		if err != nil {
			if errors.Is(err, store.ErrProfileNotFound) {
				return nil, huma.Error404NotFound("user not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		followers, err := a.store.ListFollowers(ctx, profile.UserID, viewerID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if followers == nil {
			followers = []store.UserProfile{}
		}
		return &UserProfileListOutput{Body: followers}, nil
	})

	// GET /api/v1/users/{handle}/following — list users a user follows
	huma.Register(a.huma, huma.Operation{
		OperationID: "list-user-following",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{handle}/following",
		Summary:     "List users a user is following",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		Handle string `path:"handle"`
	}) (*UserProfileListOutput, error) {
		viewerID := auth.UserIDFromCtx(ctx)
		profile, err := a.store.GetProfileByHandle(ctx, input.Handle, viewerID)
		if err != nil {
			if errors.Is(err, store.ErrProfileNotFound) {
				return nil, huma.Error404NotFound("user not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		following, err := a.store.ListFollowing(ctx, profile.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if following == nil {
			following = []store.UserProfile{}
		}
		return &UserProfileListOutput{Body: following}, nil
	})

	// POST /api/v1/social/shares — share an article
	huma.Register(a.huma, huma.Operation{
		OperationID: "share-article",
		Method:      http.MethodPost,
		Path:        "/api/v1/social/shares",
		Summary:     "Share an article to your social timeline",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *ShareArticleInput) (*SharedArticleOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		sa, err := a.store.ShareArticle(ctx, userID,
			input.Body.ArticleURL, input.Body.Title, input.Body.Description,
			input.Body.FeedURL, input.Body.FeedTitle, input.Body.FeedSiteURL,
			input.Body.Author, input.Body.PublishedAt,
		)
		if err != nil {
			return nil, huma.Error500InternalServerError(fmt.Errorf("share article: %w", err).Error())
		}
		saSnap := *sa
		go a.ATProtoSyncShare(userID, &saSnap, true, "")
		return &SharedArticleOutput{Body: *sa}, nil
	})

	// DELETE /api/v1/social/shares/{shareId} — unshare
	huma.Register(a.huma, huma.Operation{
		OperationID: "unshare-article",
		Method:      http.MethodDelete,
		Path:        "/api/v1/social/shares/{shareId}",
		Summary:     "Remove a shared article",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		ShareID int64 `path:"shareId"`
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		// Fetch the rkey before deleting — the row is gone after UnshareArticle
		// and the fire-and-forget sync goroutine can't read it back.
		rkey, _ := a.store.GetShareATProtoRkey(ctx, input.ShareID)
		shareSnap := &store.SharedArticle{ID: input.ShareID}
		if err := a.store.UnshareArticle(ctx, input.ShareID, userID); err != nil {
			if errors.Is(err, store.ErrShareNotFound) {
				return nil, huma.Error404NotFound("shared article not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		go a.ATProtoSyncShare(userID, shareSnap, false, rkey)
		return nil, nil
	})

	// GET /api/v1/social/timeline — social timeline (shares from followed users)
	huma.Register(a.huma, huma.Operation{
		OperationID: "social-timeline",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/timeline",
		Summary:     "Social timeline: shared articles from followed users",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		Limit  int `query:"limit" default:"50" minimum:"1" maximum:"100"`
		Offset int `query:"offset" default:"0" minimum:"0"`
	}) (*SharedArticleListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		articles, err := a.store.ListSocialTimeline(ctx, userID, input.Limit, input.Offset)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if articles == nil {
			articles = []store.SharedArticle{}
		}
		return &SharedArticleListOutput{Body: articles}, nil
	})

	// GET /api/v1/social/shares — my own shared articles
	huma.Register(a.huma, huma.Operation{
		OperationID: "my-shared-articles",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/shares",
		Summary:     "List your shared articles",
		Tags:        []string{"social"},
	}, func(ctx context.Context, _ *struct{}) (*SharedArticleListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		articles, err := a.store.ListSharedArticlesByUser(ctx, userID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if articles == nil {
			articles = []store.SharedArticle{}
		}
		return &SharedArticleListOutput{Body: articles}, nil
	})

	// GET /api/v1/feeds/{feedId}/subscribers — subscriber count + public profiles
	huma.Register(a.huma, huma.Operation{
		OperationID: "feed-subscribers",
		Method:      http.MethodGet,
		Path:        "/api/v1/feeds/{feedId}/subscribers",
		Summary:     "Get subscriber count and public profiles for a feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct {
		FeedID int `path:"feedId"`
	}) (*FeedSubscribersOutput, error) {
		_ = auth.UserIDFromCtx(ctx)
		feed, err := a.store.GetFeedGlobal(ctx, input.FeedID)
		if err != nil || feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}

		count, err := a.store.CountFeedSubscribers(ctx, feed.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		subs, err := a.store.ListFeedSubscribers(ctx, feed.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if subs == nil {
			subs = []store.UserProfile{}
		}
		return &FeedSubscribersOutput{Body: FeedSubscribersResponse{
			Count:       count,
			GlobalCount: int(a.relayGetFeedSubscriberCount(ctx, feed.FeedURL)),
			Subscribers: subs,
		}}, nil
	})

	// GET /api/v1/social/share-count — globally accurate share count for an
	// article URL, sourced from the relay aggregates.
	huma.Register(a.huma, huma.Operation{
		OperationID: "article-share-count",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/share-count",
		Summary:     "Get the global share count for an article URL",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		ArticleURL string `query:"article_url" required:"true"`
	}) (*ArticleShareCountOutput, error) {
		return &ArticleShareCountOutput{Body: ArticleShareCountResponse{
			ArticleURL: input.ArticleURL,
			Count:      a.relayGetArticleShareCount(ctx, input.ArticleURL),
		}}, nil
	})

	// GET /api/v1/social/star-count — globally accurate star count for an
	// article URL, sourced from the relay aggregates.
	huma.Register(a.huma, huma.Operation{
		OperationID: "article-star-count",
		Method:      http.MethodGet,
		Path:        "/api/v1/social/star-count",
		Summary:     "Get the global star count for an article URL",
		Tags:        []string{"social"},
	}, func(ctx context.Context, input *struct {
		ArticleURL string `query:"article_url" required:"true"`
	}) (*ArticleStarCountOutput, error) {
		return &ArticleStarCountOutput{Body: ArticleStarCountResponse{
			ArticleURL: input.ArticleURL,
			Count:      a.relayGetArticleStarCount(ctx, input.ArticleURL),
		}}, nil
	})
}
