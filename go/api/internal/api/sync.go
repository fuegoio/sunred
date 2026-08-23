package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// listRecordsOut matches com.atproto.repo.listRecords response.
type listRecordsOut struct {
	Records []struct {
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	} `json:"records"`
	Cursor string `json:"cursor"`
}

// syncFollows backfills io.sunred.graph.follow records from the PDS into the
// local follow cache. Each follow record's `subject` is a DID; we record a
// local follow edge if the followee is also a known Sunred user on this
// instance, and store the rkey for later delete-on-unfollow.
func syncFollows(ctx context.Context, c *atclient.APIClient, st *store.Store, userID int) error {
	cursor := ""
	for {
		params := map[string]any{
			"repo":       accountDID(c),
			"collection": atproto.CollectionFollow,
			"limit":      100,
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var out listRecordsOut
		if err := c.Get(ctx, "com.atproto.repo.listRecords", params, &out); err != nil {
			return fmt.Errorf("list follows: %w", err)
		}
		for _, rec := range out.Records {
			var f struct {
				Subject   string `json:"subject"`
				CreatedAt string `json:"createdAt"`
			}
			if err := json.Unmarshal(rec.Value, &f); err != nil || f.Subject == "" {
				continue
			}
			rkey := rkeyFromURI(rec.URI)
			// Record the follow edge locally if the subject is a local user.
			if followeeID, _ := st.GetUserIDByDID(ctx, f.Subject); followeeID != 0 {
				_ = st.UpsertFollowWithRkey(ctx, userID, followeeID, rkey)
			}
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	return nil
}

// syncFeedSubscriptions backfills io.sunred.feed.subscription records into the
// local feeds table, storing the rkey so later unsubscribe deletes the record.
func syncFeedSubscriptions(ctx context.Context, c *atclient.APIClient, st *store.Store, userID int) error {
	cursor := ""
	for {
		params := map[string]any{
			"repo":       accountDID(c),
			"collection": atproto.CollectionSubscription,
			"limit":      100,
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var out listRecordsOut
		if err := c.Get(ctx, "com.atproto.repo.listRecords", params, &out); err != nil {
			return fmt.Errorf("list feed subs: %w", err)
		}
		for _, rec := range out.Records {
			var fs struct {
				FeedURL   string `json:"feedUrl"`
				SiteURL   string `json:"siteUrl"`
				Title     string `json:"title"`
				CreatedAt string `json:"createdAt"`
			}
			if err := json.Unmarshal(rec.Value, &fs); err != nil || fs.FeedURL == "" {
				continue
			}
			rkey := rkeyFromURI(rec.URI)
			if err := st.UpsertFeedSubscriptionWithRkey(ctx, userID, fs.FeedURL, fs.SiteURL, fs.Title, rkey); err != nil {
				slog.Warn("sync: upsert feed sub", "feed_url", fs.FeedURL, "err", err)
			}
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	return nil
}

// backfillShares lists io.sunred.share.article records from a remote repo and
// ingests them into the local cache so a follower sees the followee's shares
// in their timeline. Uses an unauthenticated PDS client — listRecords is a
// public read. Idempotent via UpsertShareWithRkey.
func backfillShares(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string) error {
	cursor := ""
	for {
		out, err := c.ListRecords(ctx, did, atproto.CollectionShare, 100, cursor)
		if err != nil {
			return fmt.Errorf("list shares: %w", err)
		}
		for _, rec := range out.Records {
			var s atproto.ShareRecord
			if err := json.Unmarshal(rec.Value, &s); err != nil || s.ArticleURL == "" {
				continue
			}
			rkey := rkeyFromURI(rec.URI)
			var publishedAt *time.Time
			if s.PublishedAt != "" {
				if t, err := time.Parse(time.RFC3339, s.PublishedAt); err == nil {
					utc := t.UTC()
					publishedAt = &utc
				}
			}
			if err := st.UpsertShareWithRkey(ctx, userID,
				s.ArticleURL, s.Title, s.Description,
				s.FeedURL, s.FeedTitle, s.FeedSiteURL,
				s.Author, publishedAt, rkey,
			); err != nil {
				slog.Warn("backfill: upsert share", "article_url", s.ArticleURL, "err", err)
			}
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	return nil
}

// backfillFeedSubscriptions lists io.sunred.feed.subscription records from a
// remote repo and ingests them into the local cache so the followee's profile
// page shows their subscribed feeds. Idempotent via UpsertFeedSubscriptionWithRkey.
func backfillFeedSubscriptions(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string) error {
	cursor := ""
	for {
		out, err := c.ListRecords(ctx, did, atproto.CollectionSubscription, 100, cursor)
		if err != nil {
			return fmt.Errorf("list feed subs: %w", err)
		}
		for _, rec := range out.Records {
			var fs atproto.SubscriptionRecord
			if err := json.Unmarshal(rec.Value, &fs); err != nil || fs.FeedURL == "" {
				continue
			}
			rkey := rkeyFromURI(rec.URI)
			if err := st.UpsertFeedSubscriptionWithRkey(ctx, userID, fs.FeedURL, fs.SiteURL, fs.Title, rkey); err != nil {
				slog.Warn("backfill: upsert feed sub", "feed_url", fs.FeedURL, "err", err)
			}
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	return nil
}

// accountDID returns the DID string the API client is authenticated as.
func accountDID(c *atclient.APIClient) string {
	if c == nil || c.AccountDID == nil {
		return ""
	}
	return string(*c.AccountDID)
}

// rkeyFromURI extracts the record key from an at:// URI (at://did/collection/rkey).
func rkeyFromURI(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '/' {
			return uri[i+1:]
		}
	}
	return uri
}

// keep syntax import referenced (DID parsing helper for future use)
var _ = syntax.DID("")
