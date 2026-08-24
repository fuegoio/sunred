package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// backfillUserFromPDS paginates the given io.sunred.* collections from a PDS
// and ingests the records into the local cache. It is the shared backfill path
// used at login (to provision the user's own data) and on follow (to replicate
// a followee's shares + feed subscriptions). did is the repo to read; userID is
// the local user the records belong to. Failures per collection are logged and
// collected — one failing collection does not abort the others.
func backfillUserFromPDS(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string, collections []string) error {
	var errs []error
	for _, col := range collections {
		switch col {
		case atproto.CollectionFollow:
			if err := backfillFollows(ctx, c, st, userID, did); err != nil {
				slog.Warn("backfill: follows", "did", did, "err", err)
				errs = append(errs, err)
			}
		case atproto.CollectionShare:
			if err := backfillShares(ctx, c, st, userID, did); err != nil {
				slog.Warn("backfill: shares", "did", did, "err", err)
				errs = append(errs, err)
			}
		case atproto.CollectionStar:
			if err := backfillStars(ctx, c, st, userID, did); err != nil {
				slog.Warn("backfill: stars", "did", did, "err", err)
				errs = append(errs, err)
			}
		case atproto.CollectionSubscription:
			if err := backfillFeedSubscriptions(ctx, c, st, userID, did); err != nil {
				slog.Warn("backfill: feed subs", "did", did, "err", err)
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// --- Per-collection backfill (all use the unauthenticated atproto.Client) ---

func backfillFollows(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string) error {
	cursor := ""
	var count int
	for {
		out, err := c.ListRecords(ctx, did, atproto.CollectionFollow, 100, cursor)
		if err != nil {
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
			if followeeID, _ := st.GetUserIDByDID(ctx, f.Subject); followeeID != 0 {
				if err := st.UpsertFollowWithRkey(ctx, userID, followeeID, rkey); err != nil {
					slog.Warn("sync: upsert follow", "user_id", userID, "followee_did", f.Subject, "rkey", rkey, "err", err)
				}
				count++
			}
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	slog.Info("sync: follows backfilled", "user_id", userID, "count", count)
	return nil
}

func backfillShares(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string) error {
	cursor := ""
	var count int
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
			// Backfilled shares are not marked unread for followers — following
			// someone marks their past shares as read by default. Only new shares
			// arriving after the follow (live relay events) create unread state.
			if _, err := st.UpsertShareWithRkey(ctx, userID,
				s.ArticleURL, s.Title, s.Description,
				s.FeedURL, s.FeedTitle, s.FeedSiteURL,
				s.Author, publishedAt, rkey,
			); err != nil {
				slog.Warn("backfill: upsert share", "article_url", s.ArticleURL, "err", err)
			}
			count++
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	slog.Info("sync: shares backfilled", "user_id", userID, "count", count)
	return nil
}

func backfillStars(ctx context.Context, c *atproto.Client, st *store.Store, userID int, did string) error {
	cursor := ""
	var count int
	for {
		out, err := c.ListRecords(ctx, did, atproto.CollectionStar, 100, cursor)
		if err != nil {
			return fmt.Errorf("list stars: %w", err)
		}
		for _, rec := range out.Records {
			var s atproto.StarRecord
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
			if err := st.UpsertStarWithRkey(ctx, userID,
				s.ArticleURL, s.Title, s.Description,
				s.FeedURL, s.FeedTitle, s.FeedSiteURL,
				s.Author, publishedAt, rkey,
			); err != nil {
				slog.Warn("backfill: upsert star", "article_url", s.ArticleURL, "err", err)
			}
			count++
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	slog.Info("sync: stars backfilled", "user_id", userID, "count", count)
	return nil
}

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

// rkeyFromURI extracts the record key from an at:// URI (at://did/collection/rkey).
func rkeyFromURI(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '/' {
			return uri[i+1:]
		}
	}
	return uri
}
