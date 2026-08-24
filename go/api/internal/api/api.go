// Package api registers the Sunred REST API on a huma router.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/config"
	"github.com/fuegoio/sunred/go/api/internal/reader/discoverer"
	"github.com/fuegoio/sunred/go/api/internal/reader/fetcher"
	"github.com/fuegoio/sunred/go/api/internal/reader/parser"
	"github.com/fuegoio/sunred/go/api/internal/reader/processor"
	"github.com/fuegoio/sunred/go/api/internal/reader/sanitizer"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// API wires the store and auth to a huma router and registers the REST routes.
type API struct {
	huma      huma.API
	store     *store.Store
	auth      *auth.Auth
	cfg       *config.Config
	fetcher   *fetcher.Fetcher
	processor *processor.Processor

	// oauthApp resumes a user's OAuth session (DPoP-bound) to write
	// io.sunred.* records to their PDS. nil when OAuth is not configured
	// (e.g. --openapi generation); in that case writerForUser falls back to
	// an unauthenticated client built from the stored pds_url.
	oauthApp atproto.OAuthApp

	// writerForUser returns an authenticated Writer for a user's PDS, or nil
	// if the user has no AT Proto connection. Tests override it to build a
	// Writer from seeded pds_url credentials without an OAuth session.
	writerForUser func(ctx context.Context, userID int) (*atproto.Writer, error)
}

// New returns an API bound to the given huma router, store, auth, config,
// and fetcher. The fetcher may be nil when only generating the OpenAPI spec
// (--openapi flag).
func New(humaAPI huma.API, st *store.Store, authInst *auth.Auth, cfg *config.Config, f *fetcher.Fetcher) *API {
	var proc *processor.Processor
	if st != nil && f != nil {
		proc = processor.New(st, f)
	}
	return &API{huma: humaAPI, store: st, auth: authInst, cfg: cfg, fetcher: f, processor: proc}
}

// SetOAuthApp wires the OAuth app used to resume user sessions for PDS record
// writes. Called once after New once the indigo OAuth ClientApp is built.
// When not set, writerForUser falls back to an unauthenticated client built
// from the stored pds_url (used by tests against a mock PDS).
func (a *API) SetOAuthApp(app atproto.OAuthApp) {
	a.oauthApp = app
}

// OpenAPITags returns the ordered tag list for the OpenAPI spec.
func OpenAPITags() []*huma.Tag {
	return []*huma.Tag{
		{Name: "feeds", Description: "Feed subscriptions"},
		{Name: "entries", Description: "Feed entries/articles"},
		{Name: "folders", Description: "Feed folders"},
		{Name: "users", Description: "User accounts"},
		{Name: "tokens", Description: "API tokens"},
		{Name: "opml", Description: "OPML import/export"},
		{Name: "social", Description: "Social features: handles, follows, article sharing"},
		{Name: "atproto", Description: "AT Protocol identity and PDS integration"},
		{Name: "device", Description: "Device-flow login (CLI/TUI)"},
	}
}

// RegisterRoutes registers all Sunred REST routes on the huma router.
func (a *API) RegisterRoutes() {
	a.registerHealthRoutes()
	a.registerMeRoutes()
	a.registerFolderRoutes()
	a.registerFeedRoutes()
	a.registerEntryRoutes()
	a.registerTokenRoutes()
	a.registerOPMLRoutes()
	a.registerSocialRoutes()
	a.registerATProtoRoutes()
	a.registerDeviceRoutes()
}

// --- Health ---

func (a *API) registerHealthRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/v1/health",
		Summary:     "Health check",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Status string `json:"status"`
		}
	}, error) {
		return &struct {
			Body struct {
				Status string `json:"status"`
			}
		}{Body: struct {
			Status string `json:"status"`
		}{Status: "ok"}}, nil
	})
}

// --- Me ---

type MeOutput struct {
	Body store.User
}

type UpdateMeInput struct {
	Body struct {
		DisplayName string `json:"display_name" maxLength:"255"`
	}
}

func (a *API) registerMeRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/v1/me",
		Summary:     "Get current user",
		Tags:        []string{"users"},
	}, func(ctx context.Context, _ *struct{}) (*MeOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		user, err := a.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(fmt.Errorf("get user: %w", err).Error())
		}
		if user == nil {
			return nil, huma.Error404NotFound("user not found")
		}
		return &MeOutput{Body: *user}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "update-me",
		Method:      http.MethodPatch,
		Path:        "/v1/me",
		Summary:     "Update current user profile",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *UpdateMeInput) (*MeOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		user, err := a.store.UpdateUser(ctx, userID, input.Body.DisplayName)
		if err != nil {
			return nil, huma.Error500InternalServerError(fmt.Errorf("update user: %w", err).Error())
		}
		if user == nil {
			return nil, huma.Error404NotFound("user not found")
		}
		// Mirror display name (+ current bio) onto the Bluesky profile record.
		go a.ATProtoSyncProfile(userID, user.DisplayName, user.Bio, user.CreatedAt)
		return &MeOutput{Body: *user}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "delete-me",
		Method:      http.MethodDelete,
		Path:        "/v1/me",
		Summary:     "Delete current user account",
		Tags:        []string{"users"},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.DeleteUser(ctx, userID); err != nil {
			return nil, huma.Error500InternalServerError(fmt.Errorf("delete user: %w", err).Error())
		}
		return nil, nil
	})
}

// --- Folders ---

type CreateFolderInput struct {
	Body struct {
		Title    string `json:"title" minLength:"1" maxLength:"255"`
		ParentID *int   `json:"parent_id,omitempty"`
	}
}

type UpdateFolderInput struct {
	FolderID int `path:"folderId"`
	Body     struct {
		Title    string `json:"title" minLength:"1" maxLength:"255"`
		ParentID *int   `json:"parent_id,omitempty"`
	}
}

type FolderOutput struct {
	Body store.Folder
}

type FolderListOutput struct {
	Body []store.Folder
}

func (a *API) registerFolderRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "create-folder",
		Method:      http.MethodPost,
		Path:        "/v1/folders",
		Summary:     "Create a folder",
		Tags:        []string{"folders"},
	}, func(ctx context.Context, input *CreateFolderInput) (*FolderOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		folder, err := a.store.CreateFolder(ctx, userID, input.Body.Title, input.Body.ParentID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &FolderOutput{Body: *folder}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "list-folders",
		Method:      http.MethodGet,
		Path:        "/v1/folders",
		Summary:     "List folders",
		Tags:        []string{"folders"},
	}, func(ctx context.Context, _ *struct{}) (*FolderListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		folders, err := a.store.ListFolders(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if folders == nil {
			folders = []store.Folder{}
		}
		return &FolderListOutput{Body: folders}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "update-folder",
		Method:      http.MethodPatch,
		Path:        "/v1/folders/{folderId}",
		Summary:     "Update a folder",
		Description: "Update the title and/or parent folder of a folder. Set parent_id to move or nest the folder.",
		Tags:        []string{"folders"},
	}, func(ctx context.Context, input *UpdateFolderInput) (*FolderOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		folder, err := a.store.UpdateFolder(ctx, input.FolderID, userID, input.Body.Title, input.Body.ParentID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if folder == nil {
			return nil, huma.Error404NotFound("folder not found")
		}
		return &FolderOutput{Body: *folder}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "delete-folder",
		Method:      http.MethodDelete,
		Path:        "/v1/folders/{folderId}",
		Summary:     "Delete a folder",
		Tags:        []string{"folders"},
	}, func(ctx context.Context, input *struct {
		FolderID int `path:"folderId"`
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.DeleteFolder(ctx, input.FolderID, userID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return nil, nil
	})
}

// --- Feeds ---

type CreateFeedInput struct {
	Body struct {
		FeedURL  string `json:"feed_url" minLength:"1" maxLength:"2048"`
		FolderID *int   `json:"folder_id,omitempty"`
	}
}

type UpdateFeedInput struct {
	FeedID int `path:"feedId"`
	Body   struct {
		FolderID *int   `json:"folder_id,omitempty"`
		Title    string `json:"title,omitempty" maxLength:"512"`
	}
}

type FeedOutput struct {
	Body store.Feed
}

type FeedListOutput struct {
	Body []store.Feed
}

type PreviewFeedInput struct {
	Body struct {
		FeedURL string `json:"feed_url" minLength:"1" maxLength:"2048"`
	}
}

type PreviewFeedItem struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Author      string    `json:"author,omitempty"`
	Description string    `json:"description,omitempty"`
	Content     string    `json:"content"`
	PublishedAt time.Time `json:"published_at"`
	Tags        []string  `json:"tags,omitempty"`
	Status      string    `json:"status,omitempty"`
	Starred     bool      `json:"starred,omitempty"`
}

type PreviewFeedBody struct {
	ID          int                  `json:"id,omitempty"`
	Title       string               `json:"title"`
	SiteURL     string               `json:"site_url"`
	FeedURL     string               `json:"feed_url"`
	Description string               `json:"description,omitempty"`
	FaviconURL  string               `json:"favicon_url,omitempty"`
	Items       []PreviewFeedItem    `json:"items"`
	Subscribers *FeedSubscribersResp `json:"subscribers,omitempty"`
}

// FeedSubscribersResp is the subscriber summary embedded in the preview
// response when the feed is known to the instance.
type FeedSubscribersResp struct {
	Count       int                 `json:"count"`
	GlobalCount int                 `json:"global_count"`
	Subscribers []store.UserProfile  `json:"subscribers"`
}

type PreviewFeedOutput struct {
	Body PreviewFeedBody
}

func (a *API) registerFeedRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "create-feed",
		Method:      http.MethodPost,
		Path:        "/v1/feeds",
		Summary:     "Subscribe to a feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *CreateFeedInput) (*FeedOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		feed, err := a.subscribeToFeed(ctx, userID, input.Body.FeedURL, input.Body.FolderID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &FeedOutput{Body: *feed}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "list-feeds",
		Method:      http.MethodGet,
		Path:        "/v1/feeds",
		Summary:     "List feeds",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, _ *struct{}) (*FeedListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		feeds, err := a.store.ListFeeds(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feeds == nil {
			feeds = []store.Feed{}
		}
		return &FeedListOutput{Body: feeds}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "get-feed",
		Method:      http.MethodGet,
		Path:        "/v1/feeds/{feedId}",
		Summary:     "Get a feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct {
		FeedID int `path:"feedId"`
	}) (*FeedOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		feed, err := a.store.GetSubscriptionFeed(ctx, input.FeedID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		return &FeedOutput{Body: *feed}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "delete-feed",
		Method:      http.MethodDelete,
		Path:        "/v1/feeds/{feedId}",
		Summary:     "Delete a feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct {
		FeedID int `path:"feedId"`
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		// Fetch the subscription's feed + rkey before deleting so we can
		// replicate the unsubscribe to the user's PDS.
		feed, err := a.store.GetSubscriptionFeed(ctx, input.FeedID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		rkey, _ := a.store.GetFeedATProtoRkey(ctx, userID, input.FeedID)
		if err := a.store.DeleteSubscription(ctx, userID, input.FeedID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if rkey != "" {
			go a.ATProtoSyncFeedSubscription(userID, input.FeedID, feed.FeedURL, feed.SiteURL, feed.Title, false, time.Now(), rkey)
		}
		return nil, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "update-feed",
		Method:      http.MethodPatch,
		Path:        "/v1/feeds/{feedId}",
		Summary:     "Update a feed",
		Description: "Update the folder assignment and/or title override of a subscription.",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *UpdateFeedInput) (*FeedOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		feed, err := a.store.GetSubscriptionFeed(ctx, input.FeedID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		folderID := input.Body.FolderID
		if folderID == nil {
			folderID = feed.FolderID
		}
		title := input.Body.Title
		if title == "" {
			title = feed.Title
		}
		updated, err := a.store.UpdateSubscription(ctx, userID, input.FeedID, folderID, title)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if updated == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		return &FeedOutput{Body: *updated}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "mark-feed-read",
		Method:      http.MethodPost,
		Path:        "/v1/feeds/{feedId}/mark-all-read",
		Summary:     "Mark all entries in a feed as read",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct {
		FeedID int `path:"feedId"`
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.MarkFeedEntriesRead(ctx, input.FeedID, userID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return nil, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "refresh-feed",
		Method:      http.MethodPost,
		Path:        "/v1/feeds/{feedId}/refresh",
		Summary:     "Refresh a feed",
		Description: "Manually fetch and parse the feed, inserting any new entries. Use this to get the latest articles without waiting for the scheduler.",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct {
		FeedID int `path:"feedId"`
	}) (*FeedOutput, error) {
		if a.processor == nil {
			return nil, huma.Error503ServiceUnavailable("feed processor is not available")
		}
		userID := auth.UserIDFromCtx(ctx)
		// Refresh is allowed by any subscriber; process the global feed.
		if sub, err := a.store.GetSubscriptionFeed(ctx, input.FeedID, userID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		} else if sub == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		feed, err := a.store.GetFeedGlobal(ctx, input.FeedID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		if err := a.processor.ProcessFeed(ctx, feed); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		feed, err = a.store.GetFeedGlobal(ctx, input.FeedID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if feed == nil {
			return nil, huma.Error404NotFound("feed not found")
		}
		return &FeedOutput{Body: *feed}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "preview-feed",
		Method:      http.MethodPost,
		Path:        "/v1/feeds/preview",
		Summary:     "Preview a feed without subscribing",
		Description: "Fetches and parses a feed URL, returning feed metadata and recent entries without persisting anything.",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *PreviewFeedInput) (*PreviewFeedOutput, error) {
		if a.fetcher == nil {
			return nil, huma.Error503ServiceUnavailable("feed fetcher is not available")
		}

		// Try to discover the feed URL. If the input is already a feed URL,
		// discovery returns it as-is. If the input is an HTML page, discovery
		// parses <link rel="alternate"> tags to find the feed URL.
		discovery, err := discoverer.Discover(ctx, a.fetcher, input.Body.FeedURL)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("could not discover feed: %s", err.Error()))
		}

		feedURL := discovery.FeedURL
		result, err := a.fetcher.Fetch(ctx, feedURL, "", "")
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("could not fetch feed: %s", err.Error()))
		}
		if result.NotModified {
			return nil, huma.Error400BadRequest("feed returned 304 Not Modified — no content to preview")
		}

		parsed, err := parser.Parse(result.Body, result.ContentType)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("could not parse feed: %s", err.Error()))
		}

		maxItems := 20
		if len(parsed.Items) > maxItems {
			parsed.Items = parsed.Items[:maxItems]
		}

		items := make([]PreviewFeedItem, 0, len(parsed.Items))
		for _, item := range parsed.Items {
			content := item.Content
			if content == "" {
				content = item.Description
			}
			sanitized, err := sanitizer.Sanitize(content)
			if err != nil {
				sanitized = content
			}

			publishedAt, err := processor.ParseDate(item.PublishedAt)
			if err != nil {
				publishedAt = time.Now()
			}

			items = append(items, PreviewFeedItem{
				Title:       item.Title,
				URL:         item.Link,
				Author:      item.Author,
				Description: sanitizer.StripHTML(item.Description),
				Content:     sanitized,
				PublishedAt: publishedAt,
				Tags:        item.Tags,
			})
		}

		// Populate the user's existing read/star state for the preview items so
		// the UI reflects state that was set before subscribing (the state is
		// keyed by article URL, so it persists independently of the feed).
		userID := auth.UserIDFromCtx(ctx)
		if len(items) > 0 {
			urls := make([]string, len(items))
			for i, it := range items {
				urls[i] = it.URL
			}
			states, err := a.store.GetEntryStatesByURLs(ctx, userID, urls)
			if err != nil {
				slog.Warn("preview: get entry states", "user_id", userID, "err", err)
			}
			for i := range items {
				if st, ok := states[items[i].URL]; ok {
					items[i].Status = st.Status
					items[i].Starred = st.Starred
				}
			}
		}

		faviconURL := ""
		if parsed.SiteURL != "" {
			faviconURL = "https://www.google.com/s2/favicons?domain=" + parsed.SiteURL + "&sz=64"
		}

		// Look up the global feed by URL. When it exists (at least one user
		// subscribes), include the feed ID and the subscriber summary so the
		// discovery view can show the subscriber count without a separate
		// round trip.
		feedID := 0
		var subs *FeedSubscribersResp
		if global, err := a.store.GetFeedByURL(ctx, feedURL); err == nil && global != nil {
			feedID = global.ID
			count, _ := a.store.CountFeedSubscribers(ctx, global.ID)
			list, _ := a.store.ListFeedSubscribers(ctx, global.ID)
			if list == nil {
				list = []store.UserProfile{}
			}
			subs = &FeedSubscribersResp{
				Count:       count,
				GlobalCount: int(a.relayGetFeedSubscriberCount(ctx, global.FeedURL)),
				Subscribers: list,
			}
		}

		return &PreviewFeedOutput{Body: PreviewFeedBody{
			ID:          feedID,
			Title:       parsed.Title,
			SiteURL:     parsed.SiteURL,
			FeedURL:     feedURL,
			Description: parsed.Description,
			FaviconURL:  faviconURL,
			Items:       items,
			Subscribers: subs,
		}}, nil
	})
}

// --- Entries ---

type EntryListOutput struct {
	Body []store.Entry
}

type EntryOutput struct {
	Body store.Entry
}

func (a *API) registerEntryRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "list-entries",
		Method:      http.MethodGet,
		Path:        "/v1/entries",
		Summary:     "List entries",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		FeedID   int    `query:"feed_id" omitempty:""`
		FolderID int    `query:"folder_id" omitempty:""`
		Status   string `query:"status" enum:"unread,read,removed" omitempty:""`
		Starred  bool   `query:"starred" omitempty:""`
		Search   string `query:"search" omitempty:""`
		Source   string `query:"source" enum:"feeds,follows" omitempty:""`
		Limit    int    `query:"limit" default:"50" maximum:"200"`
		Offset   int    `query:"offset" default:"0"`
	}) (*EntryListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		if input.Limit == 0 {
			input.Limit = 50
		}
		var feedID *int
		if input.FeedID > 0 {
			feedID = &input.FeedID
		}
		var folderID *int
		if input.FolderID > 0 {
			folderID = &input.FolderID
		}
		var starred *bool
		if input.Starred {
			starred = &input.Starred
		}
		entries, err := a.store.ListEntries(ctx, userID, feedID, folderID, input.Status, starred, input.Search, input.Source, input.Limit, input.Offset)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if entries == nil {
			entries = []store.Entry{}
		}
		return &EntryListOutput{Body: entries}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "get-entry",
		Method:      http.MethodGet,
		Path:        "/v1/entries/{entryId}",
		Summary:     "Get an entry",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		EntryID int64 `path:"entryId"`
	}) (*EntryOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		entry, err := a.store.GetEntryByID(ctx, input.EntryID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if entry == nil {
			return nil, huma.Error404NotFound("entry not found")
		}
		return &EntryOutput{Body: *entry}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "update-entries",
		Method:      http.MethodPut,
		Path:        "/v1/entries",
		Summary:     "Bulk update entry status",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		Body struct {
			EntryIDs []int64 `json:"entry_ids"`
			Status   string  `json:"status" enum:"unread,read,removed"`
		}
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		// A null entry_ids means "all visible entries" (used by mark-all-read).
		if input.Body.EntryIDs == nil {
			if err := a.store.MarkAllEntriesRead(ctx, userID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return nil, nil
		}
		if err := a.store.UpdateEntryStatus(ctx, input.Body.EntryIDs, userID, input.Body.Status); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return nil, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "toggle-entry-starred",
		Method:      http.MethodPut,
		Path:        "/v1/entries/{entryId}/starred",
		Summary:     "Toggle starred on an entry",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		EntryID int64 `path:"entryId"`
		Body    struct {
			Starred bool `json:"starred"`
		}
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		// Fetch the entry before toggling — we need the article URL and
		// metadata for the PDS record and the rkey lookup. For unstars,
		// the rkey must be fetched before the toggle deletes the star row.
		entry, err := a.store.GetEntryByID(ctx, input.EntryID, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if entry == nil {
			return nil, huma.Error404NotFound("entry not found")
		}
		rkey, _ := a.store.GetStarATProtoRkey(ctx, userID, entry.URL)
		if err := a.store.ToggleEntryStarred(ctx, input.EntryID, userID, input.Body.Starred); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		go a.ATProtoSyncStar(userID, entry, input.Body.Starred, rkey)
		return nil, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "toggle-entry-starred-by-url",
		Method:      http.MethodPut,
		Path:        "/v1/entries/by-url/starred",
		Summary:     "Toggle starred on an article by URL (no entry ID required)",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		Body struct {
			ArticleURL  string     `json:"article_url" minLength:"1" maxLength:"2048"`
			Title       string     `json:"title" maxLength:"1024"`
			Description string     `json:"description,omitempty" maxLength:"1000"`
			FeedURL     string     `json:"feed_url,omitempty" maxLength:"2048"`
			FeedTitle   string     `json:"feed_title,omitempty" maxLength:"512"`
			FeedSiteURL string     `json:"feed_site_url,omitempty" maxLength:"2048"`
			Author      string     `json:"author,omitempty" maxLength:"255"`
			PublishedAt *time.Time `json:"published_at,omitempty"`
			Starred     bool       `json:"starred"`
		}
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		rkey, _ := a.store.GetStarATProtoRkey(ctx, userID, input.Body.ArticleURL)
		if err := a.store.ToggleEntryStarredByURL(ctx, userID,
			input.Body.ArticleURL, input.Body.Title, input.Body.Description,
			input.Body.FeedURL, input.Body.FeedTitle, input.Body.FeedSiteURL,
			input.Body.Author, input.Body.PublishedAt, input.Body.Starred,
		); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		// Build a minimal Entry for ATProtoSyncStar (it only needs URL, title,
		// description, author, published_at, and feed metadata).
		entry := &store.Entry{
			URL:         input.Body.ArticleURL,
			Title:       input.Body.Title,
			Description: input.Body.Description,
			Author:      input.Body.Author,
		}
		if input.Body.PublishedAt != nil {
			entry.PublishedAt = *input.Body.PublishedAt
		}
		if input.Body.FeedURL != "" {
			entry.Feed = &store.Feed{
				FeedURL: input.Body.FeedURL,
				Title:   input.Body.FeedTitle,
				SiteURL: input.Body.FeedSiteURL,
			}
		}
		go a.ATProtoSyncStar(userID, entry, input.Body.Starred, rkey)
		return nil, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "update-entry-status-by-url",
		Method:      http.MethodPut,
		Path:        "/v1/entries/by-url",
		Summary:     "Update an article's read status by URL (no entry ID required)",
		Tags:        []string{"entries"},
	}, func(ctx context.Context, input *struct {
		Body struct {
			ArticleURL string `json:"article_url" minLength:"1" maxLength:"2048"`
			Status     string `json:"status" enum:"unread,read,removed"`
		}
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.UpdateEntryStatusByURL(ctx, userID, input.Body.ArticleURL, input.Body.Status); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return nil, nil
	})
}

// --- API Tokens ---

type CreateTokenInput struct {
	Body struct {
		Label string `json:"label" minLength:"1" maxLength:"255"`
	}
}

type TokenOutput struct {
	Body struct {
		ID        int       `json:"id"`
		Label     string    `json:"label"`
		Token     string    `json:"token"`
		CreatedAt time.Time `json:"created_at"`
	}
}

type TokenListOutput struct {
	Body []store.APIToken
}

func (a *API) registerTokenRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "create-token",
		Method:      http.MethodPost,
		Path:        "/v1/tokens",
		Summary:     "Create an API token",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, input *CreateTokenInput) (*TokenOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		rawToken := generateToken()
		hash := auth.HashToken(rawToken)
		t, err := a.store.CreateAPIToken(ctx, userID, input.Body.Label, hash, "manual", nil)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &TokenOutput{
			Body: struct {
				ID        int       `json:"id"`
				Label     string    `json:"label"`
				Token     string    `json:"token"`
				CreatedAt time.Time `json:"created_at"`
			}{
				ID:        t.ID,
				Label:     t.Label,
				Token:     rawToken,
				CreatedAt: t.CreatedAt,
			},
		}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "list-tokens",
		Method:      http.MethodGet,
		Path:        "/v1/tokens",
		Summary:     "List API tokens",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, _ *struct{}) (*TokenListOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		tokens, err := a.store.ListAPITokens(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if tokens == nil {
			tokens = []store.APIToken{}
		}
		return &TokenListOutput{Body: tokens}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "delete-token",
		Method:      http.MethodDelete,
		Path:        "/v1/tokens/{tokenId}",
		Summary:     "Delete an API token",
		Tags:        []string{"tokens"},
	}, func(ctx context.Context, input *struct {
		TokenID int `path:"tokenId"`
	}) (*struct{}, error) {
		userID := auth.UserIDFromCtx(ctx)
		if err := a.store.DeleteAPIToken(ctx, input.TokenID, userID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return nil, nil
	})
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "pla_" + hex.EncodeToString(b)
}

// subscribeToFeed fetches and parses the feed URL to populate site URL and
// title, then creates the subscription. After creating the global feed record,
// it processes the feed to persist entries immediately — so the feed is not
// empty after subscription. If the user already subscribes to the feed URL,
// the existing subscription is returned (idempotent), making it safe for
// bulk subscription flows.
func (a *API) subscribeToFeed(ctx context.Context, userID int, feedURL string, folderID *int) (*store.Feed, error) {
	siteURL := ""
	title := feedURL
	description := ""

	// Discover the actual feed URL. If the input is already a feed URL,
	// discovery returns it as-is. If the input is an HTML page, discovery
	// parses <link rel="alternate"> tags to find the feed URL.
	if a.fetcher != nil {
		if discovery, err := discoverer.Discover(ctx, a.fetcher, feedURL); err == nil {
			feedURL = discovery.FeedURL
			if discovery.SiteURL != "" {
				siteURL = discovery.SiteURL
			}
			// Fetch and parse the feed to populate the real title and description.
			if result, err := a.fetcher.Fetch(ctx, feedURL, "", ""); err == nil && !result.NotModified {
				if parsed, err := parser.Parse(result.Body, result.ContentType); err == nil {
					if parsed.SiteURL != "" {
						siteURL = parsed.SiteURL
					}
					if parsed.Title != "" {
						title = parsed.Title
					}
					description = parsed.Description
				}
			}
		}
	}

	// If the user already subscribes to this feed, return the existing view.
	if feed, err := a.store.GetFeedByURL(ctx, feedURL); err != nil {
		return nil, err
	} else if feed != nil {
		if sub, err := a.store.GetSubscriptionFeed(ctx, feed.ID, userID); err == nil && sub != nil {
			return sub, nil
		}
	}

	// Create or update the global feed, then subscribe the user to it.
	feed, err := a.store.GetOrCreateFeed(ctx, feedURL, siteURL, title, description)
	if err != nil {
		return nil, err
	}

	sub, err := a.store.CreateSubscription(ctx, userID, feed.ID, folderID, "")
	if err != nil {
		return nil, err
	}

	// Process the feed immediately so entries are persisted on subscribe.
	// Best-effort: the scheduler will retry on failure.
	if a.processor != nil {
		_ = a.processor.ProcessFeed(ctx, feed)
	}

	// Mark all existing entries as read so the user doesn't see a backlog of
	// unread items from before they subscribed.
	_ = a.store.MarkFeedEntriesRead(ctx, feed.ID, userID)

	// Replicate the subscription to the user's PDS so it survives across
	// instances and can be backfilled by the relay on future logins.
	go a.ATProtoSyncFeedSubscription(userID, feed.ID, feed.FeedURL, feed.SiteURL, feed.Title, true, feed.CreatedAt, "")

	if sub != nil {
		return sub, nil
	}
	return a.store.GetSubscriptionFeed(ctx, feed.ID, userID)
}

// --- OPML ---

// opmlXML is the XML representation of an OPML document.
type opmlXML struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title string `xml:"title,omitempty"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Title    string        `xml:"title,attr,omitempty"`
	Text     string        `xml:"text,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline,omitempty"`
}

type OPMLExportOutput struct {
	Body []byte
}

type OPMLImportInput struct {
	RawBody []byte
}

type OPMLImportResult struct {
	Body struct {
		Imported int      `json:"imported"`
		Skipped  int      `json:"skipped"`
		Failed   int      `json:"failed"`
		FeedIDs  []int    `json:"feed_ids"`
		Errors   []string `json:"errors,omitempty"`
	}
}

func (a *API) registerOPMLRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "export-opml",
		Method:      http.MethodGet,
		Path:        "/v1/opml/export",
		Summary:     "Export feeds as OPML",
		Description: "Returns all feed subscriptions and folders as an OPML XML document.",
		Tags:        []string{"opml"},
	}, func(ctx context.Context, _ *struct{}) (*OPMLExportOutput, error) {
		userID := auth.UserIDFromCtx(ctx)

		feeds, err := a.store.ListFeeds(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		folders, err := a.store.ListFolders(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}

		// Group feeds by folder. Feeds without a folder go into the root.
		folderMap := make(map[int][]store.Feed)
		var unfiled []store.Feed
		for _, f := range feeds {
			if f.FolderID != nil {
				folderMap[*f.FolderID] = append(folderMap[*f.FolderID], f)
			} else {
				unfiled = append(unfiled, f)
			}
		}

		var outlines []opmlOutline
		for _, fo := range folders {
			folderFeeds := folderMap[fo.ID]
			if len(folderFeeds) == 0 {
				continue
			}
			folderOutline := opmlOutline{
				Title:    fo.Title,
				Text:     fo.Title,
				Outlines: feedsToOutlines(folderFeeds),
			}
			outlines = append(outlines, folderOutline)
		}
		outlines = append(outlines, feedsToOutlines(unfiled)...)

		doc := opmlXML{
			Version: "2.0",
			Head:    opmlHead{Title: "Sunred Subscriptions"},
			Body:    opmlBody{Outlines: outlines},
		}

		data, err := xml.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		data = append([]byte(xml.Header), data...)

		return &OPMLExportOutput{Body: data}, nil
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "import-opml",
		Method:      http.MethodPost,
		Path:        "/v1/opml/import",
		Summary:     "Import feeds from an OPML file",
		Description: "Parses an OPML XML document and subscribes the user to all feeds found. Folders are created as needed. Existing subscriptions are skipped.",
		Tags:        []string{"opml"},
	}, func(ctx context.Context, input *OPMLImportInput) (*OPMLImportResult, error) {
		userID := auth.UserIDFromCtx(ctx)

		var doc opmlXML
		decoder := xml.NewDecoder(bytes.NewReader(input.RawBody))
		decoder.Strict = false
		if err := decoder.Decode(&doc); err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid OPML: %s", err.Error()))
		}

		// Build folder name → ID map from existing folders.
		existingFolders, err := a.store.ListFolders(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		folderByName := make(map[string]int)
		for _, fo := range existingFolders {
			folderByName[fo.Title] = fo.ID
		}

		result := OPMLImportResult{}
		result.Body.FeedIDs = []int{}

		getOrCreateFolder := func(name string) (*int, error) {
			if name == "" {
				return nil, nil
			}
			if id, ok := folderByName[name]; ok {
				return &id, nil
			}
			folder, err := a.store.CreateFolder(ctx, userID, name, nil)
			if err != nil {
				return nil, err
			}
			folderByName[name] = folder.ID
			return &folder.ID, nil
		}

		var processOutlines func(outlines []opmlOutline, folderName string)
		processOutlines = func(outlines []opmlOutline, folderName string) {
			for _, o := range outlines {
				if o.XMLURL != "" {
					// Leaf node — a feed subscription.
					var folderID *int
					if folderName != "" {
						f, err := getOrCreateFolder(folderName)
						if err != nil {
							result.Body.Failed++
							result.Body.Errors = append(result.Body.Errors, o.XMLURL+": "+err.Error())
							continue
						}
						folderID = f
					}

					feed, fErr := a.subscribeToFeed(ctx, userID, o.XMLURL, folderID)
					if fErr != nil {
						result.Body.Failed++
						result.Body.Errors = append(result.Body.Errors, o.XMLURL+": "+fErr.Error())
						continue
					}
					result.Body.FeedIDs = append(result.Body.FeedIDs, feed.ID)
					if feed.LastFetchAt == nil {
						result.Body.Imported++
					} else {
						result.Body.Skipped++
					}
				} else {
					// Container node — a folder. Use its title, falling back to text.
					name := o.Title
					if name == "" {
						name = o.Text
					}
					processOutlines(o.Outlines, name)
				}
			}
		}

		processOutlines(doc.Body.Outlines, "")

		return &result, nil
	})
}

// feedsToOutlines converts a slice of feeds to OPML outline elements.
func feedsToOutlines(feeds []store.Feed) []opmlOutline {
	outlines := make([]opmlOutline, 0, len(feeds))
	for _, f := range feeds {
		outlines = append(outlines, opmlOutline{
			XMLURL:  f.FeedURL,
			HTMLURL: f.SiteURL,
			Title:   f.Title,
			Text:    f.Title,
			Type:    "rss",
		})
	}
	return outlines
}
