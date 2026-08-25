// Package store wraps *sql.DB with relay-specific query helpers.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store wraps a *sql.DB with relay query helpers.
type Store struct {
	DB *sql.DB
}

// New returns a Store backed by the given database.
func New(db *sql.DB) *Store { return &Store{DB: db} }

// TrackedDID holds information about a DID the relay is subscribing to.
type TrackedDID struct {
	ID          int64
	DID         string
	PDSUrl      string
	Handle      string
	CursorSeq   int64
	Status      string
	ErrorMsg    string
	AnnouncedAt time.Time
	LastEventAt *time.Time
}

// UpsertInstance records or refreshes a Sunred instance URL.
func (s *Store) UpsertInstance(ctx context.Context, url string) (int, error) {
	var id int
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO instances (url) VALUES ($1)
		ON CONFLICT (url) DO UPDATE SET last_seen = NOW()
		RETURNING id`, url,
	).Scan(&id)
	return id, err
}

// UpsertTrackedDID registers a new DID with the relay or re-announces an
// existing one. If the DID already exists, updates pds_url, handle, and
// resets status to 'active' (clearing any error from a failed subscription).
// Returns the row id and whether the DID needs a full backfill — true for a
// newly inserted DID or a DID that was in 'error' status (recovered).
//
// The needsBackfill flag tells the caller to kick off BackfillAndSubscribe,
// which will emit a backfillComplete event. An already-active re-announce
// returns false — no backfill event is emitted, so the caller must not wait
// for one.
//
// A handle resolves to at most one DID: before upserting, any other row that
// currently holds the same non-empty handle has its handle cleared. This
// keeps the partial unique index on (handle) WHERE handle <> ” satisfied
// and prevents stale rows (e.g. from a DID rotation) from shadowing the
// current owner of a handle.
func (s *Store) UpsertTrackedDID(ctx context.Context, did, pdsURL, handle string, instanceID int) (int64, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Check whether this DID already exists and, if so, whether it was in
	// 'error' status. A DID in 'error' (from a failed PDS subscription) needs
	// a full backfill on re-announce, just like a brand-new DID.
	var oldStatus string
	if serr := tx.QueryRowContext(ctx,
		`SELECT status FROM tracked_dids WHERE did = $1`, did).Scan(&oldStatus); serr != nil && serr != sql.ErrNoRows {
		err = serr
		return 0, false, err
	}

	if handle != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE tracked_dids SET handle = '' WHERE handle = $1 AND did <> $2`,
			handle, did); err != nil {
			return 0, false, err
		}
	}

	var id int64
	var isNew bool
	err = tx.QueryRowContext(ctx, `
		INSERT INTO tracked_dids (did, pds_url, handle, instance_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (did) DO UPDATE
		  SET pds_url     = EXCLUDED.pds_url,
		      handle      = EXCLUDED.handle,
		      instance_id = COALESCE(tracked_dids.instance_id, EXCLUDED.instance_id),
		      status      = 'active',
		      error_msg   = ''
		RETURNING id, (xmax = 0)`,
		did, pdsURL, handle, instanceID,
	).Scan(&id, &isNew)
	if err != nil {
		return 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	needsBackfill := isNew || oldStatus == "error"
	return id, needsBackfill, nil
}

// ListActiveTrackedDIDs returns all DIDs with status='active'.
func (s *Store) ListActiveTrackedDIDs(ctx context.Context) ([]TrackedDID, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, did, pds_url, handle, cursor_seq, status, error_msg, announced_at, last_event_at
		FROM tracked_dids WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TrackedDID
	for rows.Next() {
		var d TrackedDID
		var lastEvent sql.NullTime
		if err := rows.Scan(&d.ID, &d.DID, &d.PDSUrl, &d.Handle,
			&d.CursorSeq, &d.Status, &d.ErrorMsg, &d.AnnouncedAt, &lastEvent); err != nil {
			return nil, err
		}
		if lastEvent.Valid {
			d.LastEventAt = &lastEvent.Time
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateTrackedDIDCursor updates the cursor sequence for a DID after processing an event.
func (s *Store) UpdateTrackedDIDCursor(ctx context.Context, did string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE tracked_dids SET cursor_seq = $2, last_event_at = NOW() WHERE did = $1`, did, seq)
	return err
}

// SetTrackedDIDError marks a DID subscription as errored.
func (s *Store) SetTrackedDIDError(ctx context.Context, did, msg string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE tracked_dids SET status = 'error', error_msg = $2 WHERE did = $1`, did, msg)
	return err
}

// --- Observed records ---

// RecordFollow inserts an observed follow record. Idempotent on (follower, followee, rkey).
// Returns true if the follow is new (count was incremented).
func (s *Store) RecordFollow(ctx context.Context, followerDID, followeeDID, rkey, pdsURL string, createdAt time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO observed_follows (follower_did, followee_did, rkey, pds_url, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (follower_did, followee_did, rkey) DO NOTHING`,
		followerDID, followeeDID, rkey, pdsURL, createdAt,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteFollow removes an observed follow by (follower, rkey).
// Returns true if a row was deleted.
func (s *Store) DeleteFollow(ctx context.Context, followerDID, rkey string) (string, bool, error) {
	var followeeDID string
	err := s.DB.QueryRowContext(ctx, `
		DELETE FROM observed_follows WHERE follower_did=$1 AND rkey=$2
		RETURNING followee_did`, followerDID, rkey,
	).Scan(&followeeDID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return followeeDID, err == nil, err
}

// CountFollowers returns the total follower count for a DID.
func (s *Store) CountFollowers(ctx context.Context, did string) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM observed_follows WHERE followee_did=$1`, did).Scan(&n)
	return n, err
}

// RecordShare inserts or updates an observed share record.
func (s *Store) RecordShare(ctx context.Context, did, rkey, articleURL, feedURL, title, pdsURL string, sharedAt *time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO observed_shares (did, rkey, article_url, feed_url, title, pds_url, shared_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (did, rkey) DO NOTHING`,
		did, rkey, articleURL, feedURL, title, pdsURL, sharedAt,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteShare removes an observed share by (did, rkey).
func (s *Store) DeleteShare(ctx context.Context, did, rkey string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM observed_shares WHERE did=$1 AND rkey=$2`, did, rkey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CountShares returns the total share count for a DID.
func (s *Store) CountShares(ctx context.Context, did string) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM observed_shares WHERE did=$1`, did).Scan(&n)
	return n, err
}

// CountArticleShares returns the number of unique DIDs that shared an article
// URL across all tracked repos.
func (s *Store) CountArticleShares(ctx context.Context, articleURL string) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT did) FROM observed_shares WHERE article_url=$1`, articleURL).Scan(&n)
	return n, err
}

// RecordFeedSubscription inserts an observed feed subscription.
func (s *Store) RecordFeedSubscription(ctx context.Context, did, rkey, feedURL, pdsURL string, createdAt *time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO observed_subscriptions (did, rkey, feed_url, pds_url, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (did, rkey) DO NOTHING`,
		did, rkey, feedURL, pdsURL, createdAt,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteFeedSubscription removes an observed feed subscription.
func (s *Store) DeleteFeedSubscription(ctx context.Context, did, rkey string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM observed_subscriptions WHERE did=$1 AND rkey=$2`, did, rkey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CountFeedSubscriptions returns the number of unique DIDs subscribed to a feed URL.
func (s *Store) CountFeedSubscriptions(ctx context.Context, feedURL string) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT did) FROM observed_subscriptions WHERE feed_url=$1`, feedURL).Scan(&n)
	return n, err
}

// RecordStar inserts an observed star record. Idempotent on (did, rkey).
func (s *Store) RecordStar(ctx context.Context, did, rkey, articleURL, pdsURL string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO observed_stars (did, rkey, article_url, pds_url)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (did, rkey) DO NOTHING`,
		did, rkey, articleURL, pdsURL,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteStar removes an observed star by (did, rkey).
func (s *Store) DeleteStar(ctx context.Context, did, rkey string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM observed_stars WHERE did=$1 AND rkey=$2`, did, rkey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetCounts returns the global follower, share, and feed subscription counts for a DID.
func (s *Store) GetCounts(ctx context.Context, did string) (followers, shares, feedSubs int64, err error) {
	err = s.DB.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM observed_follows WHERE followee_did=$1),
		  (SELECT COUNT(*) FROM observed_shares WHERE did=$1),
		  (SELECT COUNT(*) FROM observed_subscriptions WHERE did=$1)`,
		did,
	).Scan(&followers, &shares, &feedSubs)
	return
}

// --- Relay event log ---

// AppendEvent writes a new event to the relay_events log and returns its sequence number.
func (s *Store) AppendEvent(ctx context.Context, eventType, did string, payload any) (int64, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal event payload: %w", err)
	}
	var seq int64
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO relay_events (event_type, did, payload) VALUES ($1,$2,$3) RETURNING seq`,
		eventType, did, b,
	).Scan(&seq)
	return seq, err
}

// ListEventsSince returns relay_events with seq > fromSeq, up to limit rows.
func (s *Store) ListEventsSince(ctx context.Context, fromSeq int64, limit int) ([]RelayEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT seq, event_type, did, payload, created_at
		FROM relay_events WHERE seq > $1
		ORDER BY seq ASC LIMIT $2`, fromSeq, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RelayEvent
	for rows.Next() {
		var e RelayEvent
		if err := rows.Scan(&e.Seq, &e.EventType, &e.DID, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeOldEvents deletes relay_events older than the retention window.
func (s *Store) PurgeOldEvents(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM relay_events WHERE created_at < NOW() - $1::interval`,
		fmt.Sprintf("%.0f seconds", retention.Seconds()),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RelayEvent is a single entry from the relay_events log.
type RelayEvent struct {
	Seq       int64
	EventType string
	DID       string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// SearchResult is a single DID matching a search query.
type SearchResult struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	PDSUrl      string `json:"pdsUrl"`
	DisplayName string `json:"displayName,omitempty"`
	Bio         string `json:"bio,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Banner      string `json:"banner,omitempty"`
}

// RecordProfile caches the app.bsky.actor.profile fields for a tracked DID.
// avatar/banner are full public getBlob URLs resolved from the blob refs by
// the caller (the fanout layer). Overwrites unconditionally — the PDS is the
// source of truth. Returns whether any row was actually updated.
func (s *Store) RecordProfile(ctx context.Context, did, displayName, bio, avatar, banner string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE tracked_dids
		   SET display_name = $2, bio = $3, avatar = $4, banner = $5
		 WHERE did = $1`,
		did, displayName, bio, avatar, banner,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SearchDIDs returns up to limit tracked DIDs whose handle matches the query
// (case-insensitive substring match). Excludes DIDs with empty handles.
func (s *Store) SearchDIDs(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + q + "%"
	rows, err := s.DB.QueryContext(ctx, `
		SELECT did, handle, pds_url,
		       COALESCE(display_name, ''), COALESCE(bio, ''),
		       COALESCE(avatar, ''), COALESCE(banner, '')
		FROM tracked_dids
		WHERE handle ILIKE $1 AND handle <> ''
		ORDER BY (handle ILIKE $2) DESC, announced_at DESC
		LIMIT $3`, like, q+"%", limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DID, &r.Handle, &r.PDSUrl, &r.DisplayName, &r.Bio, &r.Avatar, &r.Banner); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolveResult holds the relay's cached view of a handle: identity plus the
// app.bsky.actor.profile fields (display_name, bio, avatar, banner).
type ResolveResult struct {
	DID         string
	PDSUrl      string
	DisplayName string
	Bio         string
	Avatar      string
	Banner      string
}

// ResolveHandle returns the cached identity + profile for a given handle
// (exact match), or nil if not found.
func (s *Store) ResolveHandle(ctx context.Context, handle string) (*ResolveResult, error) {
	var r ResolveResult
	err := s.DB.QueryRowContext(ctx, `
		SELECT did, pds_url,
		       COALESCE(display_name, ''), COALESCE(bio, ''),
		       COALESCE(avatar, ''), COALESCE(banner, '')
		FROM tracked_dids WHERE handle = $1 LIMIT 1`, handle,
	).Scan(&r.DID, &r.PDSUrl, &r.DisplayName, &r.Bio, &r.Avatar, &r.Banner)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
