package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// --- Web sessions (cookie auth) ---

// CreateWebSession inserts a new web session token for a user.
func (s *Store) CreateWebSession(ctx context.Context, token string, userID int, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO web_sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, expiresAt,
	)
	return err
}

// GetWebSession resolves a session token to a user ID. Returns 0 if the token
// is unknown or expired.
func (s *Store) GetWebSession(ctx context.Context, token string) (int, error) {
	if token == "" {
		return 0, fmt.Errorf("empty session token")
	}
	var userID int
	err := s.DB.QueryRowContext(ctx,
		`SELECT user_id FROM web_sessions WHERE token = $1 AND expires_at > NOW()`, token,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("invalid session")
	}
	if err != nil {
		return 0, err
	}
	return userID, nil
}

// DeleteWebSession removes a web session token.
func (s *Store) DeleteWebSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM web_sessions WHERE token = $1`, token)
	return err
}

// --- DID-based users ---

// CreateRemoteUser creates a stub user for a remote DID that has never
// logged into this instance. The user has no PDS credentials and will be
// populated on-demand from their PDS profile.
func (s *Store) CreateRemoteUser(ctx context.Context, did, handle, pdsURL string) (int, error) {
	var userID int
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO users (did, handle, pds_url)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (did) DO UPDATE
		   SET handle = EXCLUDED.handle, pds_url = COALESCE(users.pds_url, EXCLUDED.pds_url)
		 RETURNING id`,
		did, handle, pdsURL,
	).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("create remote user: %w", err)
	}
	return userID, nil
}

// GetOrCreateUserByDID returns the user ID for a DID, creating the user if it
// does not yet exist. handle is stored on the users row.
func (s *Store) GetOrCreateUserByDID(ctx context.Context, did, handle string) (int, bool, error) {
	var userID int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE did = $1`, did,
	).Scan(&userID)
	if err == nil {
		// Existing user: refresh the handle in case it changed on the PDS.
		if handle != "" {
			if _, err := s.DB.ExecContext(ctx, `UPDATE users SET handle = $2 WHERE id = $1 AND $2 <> COALESCE(handle, '')`, userID, handle); err != nil {
				slog.Warn("store: refresh user handle", "user_id", userID, "handle", handle, "err", err)
			}
		}
		return userID, false, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, fmt.Errorf("lookup user by did: %w", err)
	}

	created := false
	err = s.DB.QueryRowContext(ctx,
		`INSERT INTO users (did, handle) VALUES ($1, $2)
		 ON CONFLICT (did) DO UPDATE SET handle = EXCLUDED.handle
		 RETURNING id, (xmax = 0)`,
		did, handle,
	).Scan(&userID, &created)
	if err != nil {
		return 0, false, fmt.Errorf("create user by did: %w", err)
	}
	return userID, created, nil
}

// --- Sync helpers (PDS → local cache) ---

// GetUserIDByDID resolves a DID to a local user ID, or 0 if unknown.
func (s *Store) GetUserIDByDID(ctx context.Context, did string) (int, error) {
	var id int
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE did = $1`, did).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// GetUserDID returns the DID and handle for a user, or empty strings if unset.
func (s *Store) GetUserDID(ctx context.Context, userID int) (did, handle string, err error) {
	var d, h sql.NullString
	err = s.DB.QueryRowContext(ctx,
		`SELECT did, handle FROM users WHERE id = $1`, userID,
	).Scan(&d, &h)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return d.String, h.String, err
}

// GetUserDIDAndPDS returns the DID and PDS URL for a user. Either may be empty
// when the user has not connected AT Proto. Used to announce a followed user
// to the relay so their shares are backfilled into the local cache.
func (s *Store) GetUserDIDAndPDS(ctx context.Context, userID int) (did, pdsURL string, err error) {
	var d, p sql.NullString
	err = s.DB.QueryRowContext(ctx,
		`SELECT did, COALESCE(pds_url, '') FROM users WHERE id = $1`, userID,
	).Scan(&d, &p)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return d.String, p.String, err
}

// UpsertFollowWithRkey records a local follow edge and its AT Proto rkey.
func (s *Store) UpsertFollowWithRkey(ctx context.Context, followerID, followeeID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO user_follows (follower_id, followee_id, atproto_rkey)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (follower_id, followee_id) DO UPDATE SET atproto_rkey = EXCLUDED.atproto_rkey`,
		followerID, followeeID, rkey,
	)
	return err
}

// UpsertFeedSubscriptionWithRkey records a feed subscription with its AT Proto
// rkey so a later unsubscribe can delete the record. The global feed is created
// if missing; the subscription is created/updated for the user.
func (s *Store) UpsertFeedSubscriptionWithRkey(ctx context.Context, userID int, feedURL, siteURL, title, rkey string) error {
	feed, err := s.GetOrCreateFeed(ctx, feedURL, siteURL, title, "")
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, feed_id, atproto_rkey)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, feed_id) DO UPDATE SET atproto_rkey = EXCLUDED.atproto_rkey, updated_at = NOW()`,
		userID, feed.ID, rkey,
	)
	return err
}

// DeleteFeedByRkey removes a user's subscription by its AT Proto rkey.
// Used by the relay consumer to process feedUnsubscription events.
func (s *Store) DeleteFeedByRkey(ctx context.Context, userID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE user_id = $1 AND atproto_rkey = $2`, userID, rkey,
	)
	return err
}

// DeleteFollowByRkey removes a follow edge by its AT Proto rkey.
// Used by the relay consumer to process unfollow events.
func (s *Store) DeleteFollowByRkey(ctx context.Context, followerID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM user_follows WHERE follower_id = $1 AND atproto_rkey = $2`,
		followerID, rkey,
	)
	return err
}

// UpsertShareWithRkey records a shared article with its AT Proto rkey so a
// later unshare can delete the record. Used by the relay consumer to process
// share events.
func (s *Store) UpsertShareWithRkey(ctx context.Context, userID int,
	articleURL, title, description, feedURL, feedTitle, feedSiteURL, author string,
	publishedAt *time.Time, rkey string,
) error {
	entryID := s.ensureSharedEntry(ctx, articleURL, title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO shared_articles
		  (user_id, article_url, title, description, feed_url, feed_title, feed_site_url, author, published_at, atproto_rkey, entry_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (user_id, article_url) DO UPDATE
		  SET title        = EXCLUDED.title,
		      description  = EXCLUDED.description,
		      feed_url     = EXCLUDED.feed_url,
		      feed_title   = EXCLUDED.feed_title,
		      feed_site_url= EXCLUDED.feed_site_url,
		      author       = EXCLUDED.author,
		      published_at = EXCLUDED.published_at,
		      atproto_rkey = EXCLUDED.atproto_rkey,
		      entry_id     = COALESCE(shared_articles.entry_id, EXCLUDED.entry_id),
		      shared_at    = NOW()`,
		userID, articleURL, title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt, rkey,
		sql.NullInt64{Int64: entryID, Valid: entryID > 0},
	)
	return err
}

// DeleteShareByRkey removes a shared article by its AT Proto rkey.
// Used by the relay consumer to process unshare events.
func (s *Store) DeleteShareByRkey(ctx context.Context, userID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM shared_articles WHERE user_id = $1 AND atproto_rkey = $2`,
		userID, rkey,
	)
	return err
}

// UpsertStarWithRkey marks an entry as starred for a user via a relay event,
// matching the entry by its article URL. If the entry doesn't exist locally
// yet (the user hasn't fetched the feed), the star is a no-op — the entry
// will be starred on the next feed refresh when the star is replayed.
func (s *Store) UpsertStarWithRkey(ctx context.Context, userID int, articleURL, rkey string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO entry_state (user_id, entry_id, starred, atproto_rkey, changed_at)
		SELECT $1, e.id, true, $3, NOW()
		FROM entries e WHERE e.url = $2
		ON CONFLICT (user_id, entry_id) DO UPDATE
			SET starred = true, atproto_rkey = EXCLUDED.atproto_rkey, changed_at = NOW()`,
		userID, articleURL, rkey,
	)
	return err
}

// DeleteStarByRkey removes a star by its AT Proto rkey. Used by the relay
// consumer to process unstar events.
func (s *Store) DeleteStarByRkey(ctx context.Context, userID int, rkey string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE entry_state SET starred = false, atproto_rkey = NULL
		 WHERE user_id = $1 AND atproto_rkey = $2`,
		userID, rkey,
	)
	return err
}
