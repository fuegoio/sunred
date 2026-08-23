package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/danielgtaylor/huma/v2"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// --- Input/output types ---

type ATProtoStatusOutput struct {
	Body struct {
		Connected bool   `json:"connected"`
		DID       string `json:"did,omitempty"`
		Handle    string `json:"handle,omitempty"`
	}
}

// registerATProtoRoutes registers AT Protocol integration endpoints.
//
// Identity is now established via OAuth at login (see oauth_handlers.go), so
// there is no app-password "connect" endpoint. The status endpoint reports the
// DID linked to the current user.
func (a *API) registerATProtoRoutes() {
	// GET /.well-known/atproto-did — lets users point an AT Proto handle at
	// this instance. Registered on the bare mux in main.go; here we register
	// the XRPC-namespaced version for discoverability.
	huma.Register(a.huma, huma.Operation{
		OperationID: "atproto-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/me/atproto",
		Summary:     "Get AT Proto identity for the current user",
		Tags:        []string{"social"},
	}, func(ctx context.Context, _ *struct{}) (*ATProtoStatusOutput, error) {
		userID := auth.UserIDFromCtx(ctx)
		did, handle, err := a.store.GetUserDID(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &ATProtoStatusOutput{}
		if did != "" {
			out.Body.Connected = true
			out.Body.DID = did
			out.Body.Handle = handle
		}
		return out, nil
	})
}

// --- AT Proto side-effects called from other handlers ---

// writerForUserOrFallback returns a Writer for writing io.sunred.* records to
// the user's PDS, or nil if the user has no AT Proto identity. In production it
// resumes the user's OAuth session (DPoP-bound) so the PDS accepts the writes;
// the access token from createSession can't be used as a bearer because OAuth
// tokens require a DPoP proof. When no OAuth app is wired (tests), it falls
// back to an unauthenticated APIClient pointed at the stored pds_url — enough
// for a mock PDS that ignores auth.
func (a *API) writerForUserOrFallback(ctx context.Context, userID int) (*atproto.Writer, error) {
	if a.writerForUser != nil {
		return a.writerForUser(ctx, userID)
	}
	if a.oauthApp != nil {
		return a.writerFromOAuthSession(ctx, userID)
	}
	return a.writerFromStoredPDS(ctx, userID)
}

// writerFromOAuthSession resumes the user's persisted OAuth session and returns
// a Writer backed by the session's DPoP-bound APIClient.
func (a *API) writerFromOAuthSession(ctx context.Context, userID int) (*atproto.Writer, error) {
	did, sessionID, _, err := a.store.GetUserOAuthSession(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get oauth session: %w", err)
	}
	if did == "" || sessionID == "" {
		return nil, nil // not connected
	}
	c, err := a.oauthApp.WriterClient(ctx, did, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume oauth session: %w", err)
	}
	return atproto.NewWriter(c, did), nil
}

// writerFromStoredPDS builds an unauthenticated Writer from the stored pds_url.
// Used by tests against a mock PDS, and as a last-resort fallback when OAuth is
// not configured. Returns nil if the user has no DID / pds_url.
func (a *API) writerFromStoredPDS(ctx context.Context, userID int) (*atproto.Writer, error) {
	did, _, pdsURL, err := a.store.GetUserOAuthSession(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get oauth session: %w", err)
	}
	if did == "" || pdsURL == "" {
		return nil, nil // not connected
	}
	return atproto.NewWriter(atclient.NewAPIClient(pdsURL), did), nil
}

// ATProtoSyncFollow writes or deletes a follow record on the PDS.
// Called fire-and-forget from FollowUser / UnfollowUser handlers.
func (a *API) ATProtoSyncFollow(userID, followeeUserID int, followeeHandle string, isFollow bool) {
	ctx := context.Background()
	w, err := a.writerForUserOrFallback(ctx, userID)
	if err != nil {
		slog.Debug("atproto: skip follow sync, writer error", "user_id", userID, "is_follow", isFollow, "err", err)
		return
	}
	if w == nil {
		slog.Debug("atproto: skip follow sync, no credentials", "user_id", userID, "is_follow", isFollow)
		return
	}

	// Resolve followee DID — they may or may not have connected AT Proto.
	followeeProfile, err := a.store.GetProfileByHandle(ctx, followeeHandle, 0)
	if err != nil || followeeProfile == nil || followeeProfile.DID == "" {
		slog.Debug("atproto: skip follow sync, followee has no DID", "user_id", userID, "handle", followeeHandle)
		return
	}

	if isFollow {
		slog.Info("atproto: writing follow to PDS", "user_id", userID, "subject_did", followeeProfile.DID)
		rkey, err := w.PutFollow(ctx, followeeProfile.DID)
		if err != nil {
			slog.Warn("atproto: put follow failed", "user_id", userID, "err", err)
			return
		}
		slog.Info("atproto: follow written", "user_id", userID, "rkey", rkey)
		_ = a.store.SetFollowATProtoRkey(ctx, userID, followeeUserID, rkey)
	} else {
		rkey, err := a.store.GetFollowATProtoRkey(ctx, userID, followeeUserID)
		if err != nil || rkey == "" {
			return
		}
		slog.Info("atproto: deleting follow from PDS", "user_id", userID, "rkey", rkey)
		if err := w.DeleteFollow(ctx, rkey); err != nil {
			slog.Warn("atproto: delete follow failed", "user_id", userID, "rkey", rkey, "err", err)
		} else {
			slog.Info("atproto: follow deleted", "user_id", userID, "rkey", rkey)
		}
		_ = a.store.SetFollowATProtoRkey(ctx, userID, followeeUserID, "")
	}
}

// ATProtoSyncShare writes or deletes a share record on the PDS.
func (a *API) ATProtoSyncShare(userID int, sa *store.SharedArticle, isShare bool) {
	ctx := context.Background()
	w, err := a.writerForUserOrFallback(ctx, userID)
	if err != nil {
		slog.Debug("atproto: skip share sync, writer error", "user_id", userID, "is_share", isShare, "err", err)
		return
	}
	if w == nil {
		slog.Debug("atproto: skip share sync, no credentials", "user_id", userID, "is_share", isShare)
		return
	}

	if isShare {
		slog.Info("atproto: writing share to PDS", "user_id", userID, "article_url", sa.ArticleURL)
		rkey, err := w.PutShare(ctx,
			sa.ArticleURL, sa.Title, sa.Description,
			sa.FeedURL, sa.FeedTitle, sa.FeedSiteURL,
			sa.Author, sa.PublishedAt, sa.SharedAt,
		)
		if err != nil {
			slog.Warn("atproto: put share failed", "user_id", userID, "err", err)
			return
		}
		slog.Info("atproto: share written", "user_id", userID, "rkey", rkey)
		_ = a.store.SetShareATProtoRkey(ctx, sa.ID, rkey)
	} else {
		rkey, err := a.store.GetShareATProtoRkey(ctx, sa.ID)
		if err != nil || rkey == "" {
			return
		}
		slog.Info("atproto: deleting share from PDS", "user_id", userID, "rkey", rkey)
		if err := w.DeleteShare(ctx, rkey); err != nil {
			slog.Warn("atproto: delete share failed", "user_id", userID, "rkey", rkey, "err", err)
		} else {
			slog.Info("atproto: share deleted", "user_id", userID, "rkey", rkey)
			_ = a.store.SetShareATProtoRkey(ctx, sa.ID, "")
		}
	}
}

// ATProtoSyncProfile writes or replaces the app.bsky.actor.profile record on
// the user's PDS, mirroring their display name and bio onto their Bluesky
// profile so it is searchable on the network. Fire-and-forget; a no-op when
// the user has no AT Proto connection. createdAt is preserved across updates
// to keep the record's creation timestamp stable.
func (a *API) ATProtoSyncProfile(userID int, displayName, bio string, createdAt time.Time) {
	ctx := context.Background()
	w, err := a.writerForUserOrFallback(ctx, userID)
	if err != nil {
		slog.Debug("atproto: skip profile sync, writer error", "user_id", userID, "err", err)
		return
	}
	if w == nil {
		slog.Debug("atproto: skip profile sync, no credentials", "user_id", userID)
		return
	}
	slog.Info("atproto: writing profile to PDS", "user_id", userID)
	if err := w.PutProfile(ctx, displayName, bio, createdAt); err != nil {
		slog.Warn("atproto: put profile failed", "user_id", userID, "err", err)
		return
	}
	slog.Info("atproto: profile written", "user_id", userID)
}

// ATProtoSyncFeedSubscription writes or deletes a feed subscription record on
// the PDS. On subscribe it creates an io.sunred.feed.subscription record and
// stores the rkey locally; on unsubscribe it deletes the record using the
// stored rkey.
func (a *API) ATProtoSyncFeedSubscription(userID, feedID int, feedURL, siteURL, title string, isSubscribe bool, createdAt time.Time) {
	ctx := context.Background()
	w, err := a.writerForUserOrFallback(ctx, userID)
	if err != nil {
		slog.Debug("atproto: skip feed subscription sync, writer error", "user_id", userID, "is_subscribe", isSubscribe, "err", err)
		return
	}
	if w == nil {
		slog.Debug("atproto: skip feed subscription sync, no credentials", "user_id", userID, "is_subscribe", isSubscribe)
		return
	}

	if isSubscribe {
		slog.Info("atproto: writing feed subscription to PDS", "user_id", userID, "feed_url", feedURL)
		rkey, err := w.PutFeedSubscription(ctx, feedURL, siteURL, title, createdAt)
		if err != nil {
			slog.Warn("atproto: put feed subscription failed", "user_id", userID, "feed_url", feedURL, "err", err)
			return
		}
		slog.Info("atproto: feed subscription written", "user_id", userID, "feed_url", feedURL, "rkey", rkey)
		_ = a.store.SetFeedATProtoRkey(ctx, userID, feedID, rkey)
	} else {
		rkey, err := a.store.GetFeedATProtoRkey(ctx, userID, feedID)
		if err != nil || rkey == "" {
			return
		}
		slog.Info("atproto: deleting feed subscription from PDS", "user_id", userID, "feed_url", feedURL, "rkey", rkey)
		if err := w.DeleteFeedSubscription(ctx, rkey); err != nil {
			slog.Warn("atproto: delete feed subscription failed", "user_id", userID, "rkey", rkey, "err", err)
		} else {
			slog.Info("atproto: feed subscription deleted", "user_id", userID, "rkey", rkey)
		}
	}
}

// WellKnownATProtoDIDHandler returns an http.Handler for /.well-known/atproto-did.
// It resolves the host's subdomain (or ?handle= query param) to a DID.
func (a *API) WellKnownATProtoDIDHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract handle from subdomain: "fuego.sunred.example" → "fuego"
		host := r.Host
		handle := r.URL.Query().Get("handle")
		if handle == "" {
			// Strip port from host
			for i := len(host) - 1; i >= 0; i-- {
				if host[i] == ':' {
					host = host[:i]
					break
				}
			}
			// First segment before the first dot is the handle subdomain
			if idx := strings.IndexByte(host, '.'); idx > 0 {
				handle = host[:idx]
			}
		}
		if handle == "" {
			http.Error(w, "handle not found", http.StatusNotFound)
			return
		}

		profile, err := a.store.GetProfileByHandle(r.Context(), handle, 0)
		if err != nil || profile == nil || profile.DID == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(profile.DID))
	})
}

// announceUserToRelay notifies the relay of a user DID so it tracks them and
// backfills their repo (shares + feed subscriptions) into the local cache. This
// is the same call the OAuth handler makes on login; calling it for a followed
// user makes their io.sunred.share.article records flow into this instance via
// the relay fan-out, so a follower sees their shares as normal entries.
func (a *API) announceUserToRelay(ctx context.Context, did, pdsURL, handle string) {
	if a.cfg.RelayURL == "" || did == "" || pdsURL == "" {
		return
	}
	rc := atproto.NewClient(a.cfg.RelayURL, "")
	type announceIn struct {
		DID         string `json:"did"`
		PDSUrl      string `json:"pdsUrl"`
		InstanceURL string `json:"instanceUrl"`
		Handle      string `json:"handle"`
	}
	if err := rc.Procedure(ctx, "io.sunred.relay.announceUser", announceIn{
		DID:         did,
		PDSUrl:      pdsURL,
		InstanceURL: a.cfg.BaseURL,
		Handle:      handle,
	}, nil); err != nil {
		slog.Warn("relay: announce followee", "did", did, "err", err)
	}
}

// ingestFollowee announces the followed user to the relay so their shares (and
// the source feeds those shares reference) are backfilled into the local cache
// and appear in the follower's entry stream. Fire-and-forget; a no-op when the
// followee has no AT Proto identity or no relay is configured. Idempotent: the
// relay treats an already-tracked DID as a no-op.
func (a *API) ingestFollowee(followeeUserID int) {
	ctx := context.Background()
	did, pdsURL, err := a.store.GetUserDIDAndPDS(ctx, followeeUserID)
	if err != nil || did == "" || pdsURL == "" {
		return
	}
	handle, _, _ := a.store.GetUserDID(ctx, followeeUserID)
	a.announceUserToRelay(ctx, did, pdsURL, handle)
}

// relaySearchResult is a single DID returned by the relay searchDIDs endpoint.
type relaySearchResult struct {
	DID    string `json:"did"`
	Handle string `json:"handle"`
	PDSUrl string `json:"pdsUrl"`
}

// relaySearchDIDs queries the relay for DIDs matching the query string.
// Returns nil if no relay is configured or the request fails.
func (a *API) relaySearchDIDs(ctx context.Context, q string, limit int) []store.UserProfile {
	if a.cfg.RelayURL == "" || q == "" {
		return nil
	}
	rc := atproto.NewClient(a.cfg.RelayURL, "")
	var out struct {
		Results []relaySearchResult `json:"results"`
	}
	if err := rc.Query(ctx, "io.sunred.relay.searchDIDs", map[string]string{
		"q":     q,
		"limit": fmt.Sprintf("%d", limit),
	}, &out); err != nil {
		slog.Warn("relay: search dids", "err", err)
		return nil
	}
	var profiles []store.UserProfile
	for _, r := range out.Results {
		profiles = append(profiles, store.UserProfile{
			Handle:   r.Handle,
			DID:      r.DID,
			PDSUrl:   r.PDSUrl,
			IsRemote: true,
		})
	}
	return profiles
}

// relayResolveHandle queries the relay for the DID + PDS URL of a given handle.
// Returns ("", "", nil) if not found or no relay configured.
func (a *API) relayResolveHandle(ctx context.Context, handle string) (did, pdsURL string) {
	if a.cfg.RelayURL == "" || handle == "" {
		return "", ""
	}
	rc := atproto.NewClient(a.cfg.RelayURL, "")
	var out struct {
		DID    string `json:"did"`
		PDSUrl string `json:"pdsUrl"`
	}
	if err := rc.Query(ctx, "io.sunred.relay.resolveHandle", map[string]string{
		"handle": handle,
	}, &out); err != nil {
		slog.Warn("relay: resolve handle", "handle", handle, "err", err)
		return "", ""
	}
	return out.DID, out.PDSUrl
}

// relayGetFeedSubscriberCount queries the relay for the globally accurate
// subscriber count of a feed URL (unique DIDs across all tracked repos).
// Returns 0 if no relay is configured or the request fails.
func (a *API) relayGetFeedSubscriberCount(ctx context.Context, feedURL string) int64 {
	if a.cfg.RelayURL == "" || feedURL == "" {
		return 0
	}
	rc := atproto.NewClient(a.cfg.RelayURL, "")
	var out struct {
		Count int64 `json:"count"`
	}
	if err := rc.Query(ctx, "io.sunred.relay.getFeedSubscriberCount", map[string]string{
		"feedUrl": feedURL,
	}, &out); err != nil {
		slog.Warn("relay: get feed subscriber count", "feed_url", feedURL, "err", err)
		return 0
	}
	return out.Count
}

// relayGetArticleShareCount queries the relay for the globally accurate share
// count of an article URL (unique DIDs across all tracked repos).
// Returns 0 if no relay is configured or the request fails.
func (a *API) relayGetArticleShareCount(ctx context.Context, articleURL string) int64 {
	if a.cfg.RelayURL == "" || articleURL == "" {
		return 0
	}
	rc := atproto.NewClient(a.cfg.RelayURL, "")
	var out struct {
		Count int64 `json:"count"`
	}
	if err := rc.Query(ctx, "io.sunred.relay.getArticleShareCount", map[string]string{
		"articleUrl": articleURL,
	}, &out); err != nil {
		slog.Warn("relay: get article share count", "article_url", articleURL, "err", err)
		return 0
	}
	return out.Count
}
