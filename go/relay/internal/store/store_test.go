package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// relayTestDB connects to the relay test database and applies migrations.
// Tests are skipped if the database is unreachable.
func relayTestDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("RELAY_DATABASE_URL")
	if dsn == "" {
		// Fall back to a relay-specific test DB. If not set, skip.
		dsn = "postgres://sunred:sunred@localhost:5432/sunred_relay_test?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("could not open relay database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("relay database not reachable, skipping integration test: %v", err)
	}
	// Run migrations inline for the test database.
	applyRelayMigrations(t, db)
	return New(db)
}

func applyRelayMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	ddl := `
CREATE TABLE IF NOT EXISTS instances (
  id         SERIAL PRIMARY KEY,
  url        TEXT NOT NULL UNIQUE,
  first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tracked_dids (
  id           BIGSERIAL PRIMARY KEY,
  did          TEXT NOT NULL UNIQUE,
  pds_url      TEXT NOT NULL,
  handle       TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  bio          TEXT NOT NULL DEFAULT '',
  avatar       TEXT NOT NULL DEFAULT '',
  banner       TEXT NOT NULL DEFAULT '',
  instance_id  INTEGER REFERENCES instances(id) ON DELETE SET NULL,
  cursor_seq   BIGINT NOT NULL DEFAULT 0,
  status       VARCHAR(32) NOT NULL DEFAULT 'active',
  error_msg    TEXT NOT NULL DEFAULT '',
  announced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_event_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tracked_dids_handle_unique
  ON tracked_dids (handle) WHERE handle <> '';

CREATE TABLE IF NOT EXISTS observed_follows (
  id           BIGSERIAL PRIMARY KEY,
  follower_did TEXT NOT NULL,
  followee_did TEXT NOT NULL,
  rkey         TEXT NOT NULL,
  pds_url      TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (follower_did, followee_did, rkey)
);

CREATE TABLE IF NOT EXISTS observed_shares (
  id           BIGSERIAL PRIMARY KEY,
  did          TEXT NOT NULL,
  rkey         TEXT NOT NULL,
  article_url  TEXT NOT NULL DEFAULT '',
  feed_url     TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  pds_url      TEXT NOT NULL,
  shared_at    TIMESTAMPTZ,
  observed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);

CREATE TABLE IF NOT EXISTS observed_subscriptions (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  feed_url    TEXT NOT NULL,
  pds_url     TEXT NOT NULL,
  created_at  TIMESTAMPTZ,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);

CREATE TABLE IF NOT EXISTS relay_events (
  seq        BIGSERIAL PRIMARY KEY,
  event_type VARCHAR(32) NOT NULL,
  did        TEXT NOT NULL,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("apply relay DDL: %v", err)
	}
	// Ensure columns added by later migrations exist on pre-existing tables
	// (the test DB persists across runs; CREATE IF NOT EXISTS won't add them).
	if _, err := db.Exec(`
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS bio        TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS avatar     TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS banner     TEXT NOT NULL DEFAULT '';
	`); err != nil {
		t.Fatalf("alter tracked_dids: %v", err)
	}
}

// --- Instance ---

func TestUpsertInstance(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()
	url := fmt.Sprintf("https://inst-%d.example.com", time.Now().UnixNano())

	id1, err := s.UpsertInstance(ctx, url)
	if err != nil {
		t.Fatalf("UpsertInstance: %v", err)
	}
	if id1 == 0 {
		t.Error("expected non-zero id")
	}

	// Second upsert should return same id.
	id2, err := s.UpsertInstance(ctx, url)
	if err != nil {
		t.Fatalf("UpsertInstance (second): %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on upsert, got %d then %d", id1, id2)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM instances WHERE url=$1`, url) })
}

// --- Tracked DIDs ---

func TestUpsertTrackedDID(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	instanceURL := fmt.Sprintf("https://inst-did-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instanceURL)
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID) })

	did := fmt.Sprintf("did:plc:%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did) })

	id, isNew, err := s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "testhandle", instID)
	if err != nil {
		t.Fatalf("UpsertTrackedDID: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true on first insert")
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	// Second upsert — same DID, different handle. Should not be new.
	_, isNew2, err := s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "newhandle", instID)
	if err != nil {
		t.Fatalf("UpsertTrackedDID (second): %v", err)
	}
	if isNew2 {
		t.Error("expected isNew=false on second upsert")
	}
}

func TestUpsertTrackedDID_DedupHandle(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	instURL := fmt.Sprintf("https://inst-dedup-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instURL)
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID) })

	did1 := fmt.Sprintf("did:plc:dup1-%d", time.Now().UnixNano())
	did2 := fmt.Sprintf("did:plc:dup2-%d", time.Now().UnixNano())
	handle := fmt.Sprintf("duphandle-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did IN ($1,$2)`, did1, did2)
	})

	// First DID claims the handle.
	_, _, err := s.UpsertTrackedDID(ctx, did1, "https://pds1.example.com", handle, instID)
	if err != nil {
		t.Fatalf("UpsertTrackedDID did1: %v", err)
	}

	// Second DID claims the same handle — should steal it from did1.
	_, _, err = s.UpsertTrackedDID(ctx, did2, "https://pds2.example.com", handle, instID)
	if err != nil {
		t.Fatalf("UpsertTrackedDID did2: %v", err)
	}

	// did1's handle should now be empty.
	var h1, h2 string
	_ = s.DB.QueryRow(`SELECT handle FROM tracked_dids WHERE did=$1`, did1).Scan(&h1)
	_ = s.DB.QueryRow(`SELECT handle FROM tracked_dids WHERE did=$1`, did2).Scan(&h2)
	if h1 != "" {
		t.Errorf("did1 handle=%q, want '' (should have been cleared)", h1)
	}
	if h2 != handle {
		t.Errorf("did2 handle=%q, want %q", h2, handle)
	}

	// Search should return the handle exactly once.
	results, err := s.SearchDIDs(ctx, handle, 50)
	if err != nil {
		t.Fatalf("SearchDIDs: %v", err)
	}
	count := 0
	for _, r := range results {
		if r.Handle == handle {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 result for handle %q, got %d", handle, count)
	}
}

func TestListActiveTrackedDIDs(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did1 := fmt.Sprintf("did:plc:act1-%d", time.Now().UnixNano())
	did2 := fmt.Sprintf("did:plc:act2-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did IN ($1,$2)`, did1, did2)
	})

	instanceURL := fmt.Sprintf("https://list-active-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instanceURL)
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID) })

	_, _, _ = s.UpsertTrackedDID(ctx, did1, "https://pds1.example.com", "h1", instID)
	_, _, _ = s.UpsertTrackedDID(ctx, did2, "https://pds2.example.com", "h2", instID)

	dids, err := s.ListActiveTrackedDIDs(ctx)
	if err != nil {
		t.Fatalf("ListActiveTrackedDIDs: %v", err)
	}
	found := 0
	for _, d := range dids {
		if d.DID == did1 || d.DID == did2 {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 of our DIDs, found %d (total %d)", found, len(dids))
	}
}

func TestUpdateTrackedDIDCursor(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:cursor-%d", time.Now().UnixNano())
	instURL := fmt.Sprintf("https://cursor-inst-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instURL)
	_, _, _ = s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "h", instID)
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did)
		_, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID)
	})

	if err := s.UpdateTrackedDIDCursor(ctx, did, 42000); err != nil {
		t.Fatalf("UpdateTrackedDIDCursor: %v", err)
	}

	var seq int64
	_ = s.DB.QueryRow(`SELECT cursor_seq FROM tracked_dids WHERE did=$1`, did).Scan(&seq)
	if seq != 42000 {
		t.Errorf("cursor_seq=%d, want 42000", seq)
	}
}

func TestSetTrackedDIDError(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:err-%d", time.Now().UnixNano())
	instURL := fmt.Sprintf("https://err-inst-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instURL)
	_, _, _ = s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "h", instID)
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did)
		_, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID)
	})

	if err := s.SetTrackedDIDError(ctx, did, "connection refused"); err != nil {
		t.Fatalf("SetTrackedDIDError: %v", err)
	}

	var status, errMsg string
	_ = s.DB.QueryRow(`SELECT status, error_msg FROM tracked_dids WHERE did=$1`, did).Scan(&status, &errMsg)
	if status != "error" {
		t.Errorf("status=%q, want 'error'", status)
	}
	if errMsg != "connection refused" {
		t.Errorf("error_msg=%q", errMsg)
	}
}

func TestUpsertTrackedDID_ErrorRecovery(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:rec-%d", time.Now().UnixNano())
	instURL := fmt.Sprintf("https://rec-inst-%d.example.com", time.Now().UnixNano())
	instID, _ := s.UpsertInstance(ctx, instURL)
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did)
		_, _ = s.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID)
	})

	// First insert: needsBackfill should be true.
	_, needsBackfill, err := s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "h", instID)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !needsBackfill {
		t.Error("expected needsBackfill=true on first insert")
	}

	// Mark as error (simulating a failed PDS subscription).
	if err := s.SetTrackedDIDError(ctx, did, "connection refused"); err != nil {
		t.Fatalf("SetTrackedDIDError: %v", err)
	}

	// Re-announce: should reset status to 'active' and return needsBackfill=true.
	_, needsBackfill2, err := s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "h", instID)
	if err != nil {
		t.Fatalf("recovery upsert: %v", err)
	}
	if !needsBackfill2 {
		t.Error("expected needsBackfill=true when recovering from error")
	}

	var status, errMsg string
	_ = s.DB.QueryRow(`SELECT status, error_msg FROM tracked_dids WHERE did=$1`, did).Scan(&status, &errMsg)
	if status != "active" {
		t.Errorf("status=%q, want 'active' after recovery", status)
	}
	if errMsg != "" {
		t.Errorf("error_msg=%q, want '' after recovery", errMsg)
	}

	// Re-announce of active DID: needsBackfill should be false.
	_, needsBackfill3, err := s.UpsertTrackedDID(ctx, did, "https://pds.example.com", "h", instID)
	if err != nil {
		t.Fatalf("active re-announce: %v", err)
	}
	if needsBackfill3 {
		t.Error("expected needsBackfill=false on re-announce of active DID")
	}
}

// --- Observed follows ---

func TestRecordFollow_And_Delete(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	follower := fmt.Sprintf("did:plc:follower-%d", time.Now().UnixNano())
	followee := fmt.Sprintf("did:plc:followee-%d", time.Now().UnixNano())
	rkey := "rkeyfollow001"
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM observed_follows WHERE follower_did=$1`, follower)
	})

	isNew, err := s.RecordFollow(ctx, follower, followee, rkey, "https://pds.example.com", time.Now())
	if err != nil {
		t.Fatalf("RecordFollow: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true on first record")
	}

	// Idempotent — same triple should not be new.
	isNew2, err := s.RecordFollow(ctx, follower, followee, rkey, "https://pds.example.com", time.Now())
	if err != nil {
		t.Fatalf("RecordFollow (dup): %v", err)
	}
	if isNew2 {
		t.Error("expected isNew=false on duplicate")
	}

	// Count.
	n, err := s.CountFollowers(ctx, followee)
	if err != nil {
		t.Fatalf("CountFollowers: %v", err)
	}
	if n != 1 {
		t.Errorf("follower count=%d, want 1", n)
	}

	// Delete.
	gotFollowee, deleted, err := s.DeleteFollow(ctx, follower, rkey)
	if err != nil {
		t.Fatalf("DeleteFollow: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true")
	}
	if gotFollowee != followee {
		t.Errorf("followee_did=%q, want %q", gotFollowee, followee)
	}

	// Count after delete.
	n2, _ := s.CountFollowers(ctx, followee)
	if n2 != 0 {
		t.Errorf("follower count after delete=%d, want 0", n2)
	}

	// Delete again — should return deleted=false.
	_, deleted2, err := s.DeleteFollow(ctx, follower, rkey)
	if err != nil {
		t.Fatalf("DeleteFollow (second): %v", err)
	}
	if deleted2 {
		t.Error("expected deleted=false on second delete")
	}
}

// --- Observed shares ---

func TestRecordShare_And_Delete(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:sharer-%d", time.Now().UnixNano())
	rkey := "rkey-share-001"
	sharedAt := time.Now()
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_shares WHERE did=$1`, did) })

	isNew, err := s.RecordShare(ctx, did, rkey, "https://article.com/post", "https://feed.com/rss", "My Article", "https://pds.example.com", &sharedAt)
	if err != nil {
		t.Fatalf("RecordShare: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true")
	}

	// Duplicate.
	isNew2, _ := s.RecordShare(ctx, did, rkey, "https://article.com/post", "", "", "https://pds.example.com", nil)
	if isNew2 {
		t.Error("expected isNew=false on duplicate")
	}

	n, _ := s.CountShares(ctx, did)
	if n != 1 {
		t.Errorf("share count=%d, want 1", n)
	}

	deleted, err := s.DeleteShare(ctx, did, rkey)
	if err != nil {
		t.Fatalf("DeleteShare: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true")
	}

	n2, _ := s.CountShares(ctx, did)
	if n2 != 0 {
		t.Errorf("share count after delete=%d, want 0", n2)
	}
}

// --- Observed feed subscriptions ---

func TestRecordFeedSubscription_And_Delete(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:subber-%d", time.Now().UnixNano())
	rkey := "rkey-sub-001"
	feedURL := fmt.Sprintf("https://feed-%d.example.com/rss", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_subscriptions WHERE did=$1`, did) })

	createdAt := time.Now()
	isNew, err := s.RecordFeedSubscription(ctx, did, rkey, feedURL, "https://pds.example.com", &createdAt)
	if err != nil {
		t.Fatalf("RecordFeedSubscription: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true")
	}

	// Duplicate.
	isNew2, _ := s.RecordFeedSubscription(ctx, did, rkey, feedURL, "https://pds.example.com", nil)
	if isNew2 {
		t.Error("expected isNew=false on duplicate")
	}

	n, _ := s.CountFeedSubscriptions(ctx, feedURL)
	if n != 1 {
		t.Errorf("subscription count=%d, want 1", n)
	}

	deleted, err := s.DeleteFeedSubscription(ctx, did, rkey)
	if err != nil {
		t.Fatalf("DeleteFeedSubscription: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true")
	}

	n2, _ := s.CountFeedSubscriptions(ctx, feedURL)
	if n2 != 0 {
		t.Errorf("subscription count after delete=%d, want 0", n2)
	}
}

// --- per-DID counts ---

func TestCountFollowers(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:fol-count-%d", time.Now().UnixNano())
	follower := fmt.Sprintf("did:plc:fol-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_follows WHERE followee_did=$1`, did) })

	if n, _ := s.CountFollowers(ctx, did); n != 0 {
		t.Errorf("CountFollowers before = %d, want 0", n)
	}
	now := time.Now()
	_, _ = s.RecordFollow(ctx, follower, did, "rf1", "https://pds.example.com", now)
	if n, _ := s.CountFollowers(ctx, did); n != 1 {
		t.Errorf("CountFollowers after = %d, want 1", n)
	}
}

func TestCountFollowing(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:following-%d", time.Now().UnixNano())
	followee := fmt.Sprintf("did:plc:followee-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_follows WHERE follower_did=$1`, did) })

	if n, _ := s.CountFollowing(ctx, did); n != 0 {
		t.Errorf("CountFollowing before = %d, want 0", n)
	}
	now := time.Now()
	_, _ = s.RecordFollow(ctx, did, followee, "rf1", "https://pds.example.com", now)
	_, _ = s.RecordFollow(ctx, did, followee+"2", "rf2", "https://pds.example.com", now)
	if n, _ := s.CountFollowing(ctx, did); n != 2 {
		t.Errorf("CountFollowing after = %d, want 2", n)
	}
}

func TestCountShares(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:share-count-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_shares WHERE did=$1`, did) })

	if n, _ := s.CountShares(ctx, did); n != 0 {
		t.Errorf("CountShares before = %d, want 0", n)
	}
	now := time.Now()
	_, _ = s.RecordShare(ctx, did, "rs1", "https://a.com", "https://feed.example.com/rss", "T", "https://pds.example.com", &now)
	_, _ = s.RecordShare(ctx, did, "rs2", "https://b.com", "https://feed.example.com/rss", "T", "https://pds.example.com", &now)
	if n, _ := s.CountShares(ctx, did); n != 2 {
		t.Errorf("CountShares after = %d, want 2", n)
	}
}

func TestCountFeedSubscriptionsByDID(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:sub-count-%d", time.Now().UnixNano())
	feedURL := fmt.Sprintf("https://counts-feed-%d.example.com/rss", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM observed_subscriptions WHERE did=$1`, did) })

	if n, _ := s.CountFeedSubscriptionsByDID(ctx, did); n != 0 {
		t.Errorf("CountFeedSubscriptionsByDID before = %d, want 0", n)
	}
	now := time.Now()
	_, _ = s.RecordFeedSubscription(ctx, did, "rsub1", feedURL, "https://pds.example.com", &now)
	_, _ = s.RecordFeedSubscription(ctx, did, "rsub2", feedURL+"2", "https://pds.example.com", &now)
	if n, _ := s.CountFeedSubscriptionsByDID(ctx, did); n != 2 {
		t.Errorf("CountFeedSubscriptionsByDID after = %d, want 2", n)
	}
}

// --- Event log ---

func TestAppendEvent_And_ListSince(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:events-%d", time.Now().UnixNano())

	seq1, err := s.AppendEvent(ctx, "follow", did, map[string]string{"test": "data1"})
	if err != nil {
		t.Fatalf("AppendEvent 1: %v", err)
	}
	seq2, err := s.AppendEvent(ctx, "share", did, map[string]string{"test": "data2"})
	if err != nil {
		t.Fatalf("AppendEvent 2: %v", err)
	}
	if seq2 <= seq1 {
		t.Errorf("seq2=%d should be > seq1=%d", seq2, seq1)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM relay_events WHERE did=$1`, did) })

	// List since before both.
	events, err := s.ListEventsSince(ctx, seq1-1, 100)
	if err != nil {
		t.Fatalf("ListEventsSince: %v", err)
	}
	found := 0
	for _, e := range events {
		if e.DID == did {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 events for did, found %d", found)
	}

	// List since seq1 — should only get seq2.
	events2, _ := s.ListEventsSince(ctx, seq1, 100)
	found2 := 0
	for _, e := range events2 {
		if e.DID == did {
			found2++
		}
	}
	if found2 != 1 {
		t.Errorf("expected 1 event since seq1, found %d", found2)
	}

	// Event types preserved.
	types := map[string]bool{}
	for _, e := range events {
		if e.DID == did {
			types[e.EventType] = true
		}
	}
	if !types["follow"] || !types["share"] {
		t.Errorf("event types not preserved: %v", types)
	}
}

func TestPurgeOldEvents(t *testing.T) {
	s := relayTestDB(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:purge-%d", time.Now().UnixNano())
	// Insert an event then immediately purge with 0s retention.
	_, _ = s.AppendEvent(ctx, "follow", did, nil)
	// Purge everything older than 0 — should remove our event.
	n, err := s.PurgeOldEvents(ctx, 0)
	if err != nil {
		t.Fatalf("PurgeOldEvents: %v", err)
	}
	if n < 1 {
		t.Errorf("expected at least 1 purged event, got %d", n)
	}
}
