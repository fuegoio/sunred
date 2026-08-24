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

// testDB returns a Store backed by a real PostgreSQL instance, or skips
// the test if the database is not reachable. The database must already
// have migrations applied.
func testDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SUNRED_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sunred:sunred@localhost:5432/sunred?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("could not open database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database not reachable, skipping integration test: %v", err)
	}
	return New(db)
}

// seedUser creates a test user and returns its ID. Cleans up via t.Cleanup.
func seedUser(t *testing.T, s *Store, email string) int {
	t.Helper()
	var id int
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	did := fmt.Sprintf("did:plc:test%s", suffix)
	handle := fmt.Sprintf("test%s", suffix)
	err := s.DB.QueryRow(
		`INSERT INTO users (did, handle) VALUES ($1, $2) RETURNING id`,
		did, handle,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// seedFeed creates a global feed and subscribes userID to it (optionally in a
// folder). Returns the feed ID. Cleans up via t.Cleanup.
func seedFeed(t *testing.T, s *Store, userID int, folderID *int, title string) int {
	t.Helper()
	var id int
	err := s.DB.QueryRow(
		`INSERT INTO feeds (feed_url, title) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("https://example.com/%s.xml", title), title,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if _, err := s.DB.Exec(
		`INSERT INTO subscriptions (user_id, feed_id, folder_id) VALUES ($1, $2, $3)`,
		userID, id, folderID,
	); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM subscriptions WHERE feed_id = $1`, id)
		_, _ = s.DB.Exec(`DELETE FROM feeds WHERE id = $1`, id)
	})
	return id
}

// seedEntry creates a global entry against feedID and writes per-user state
// (status/starred) for userID. Absent state means read; a read-status row is
// only inserted when status is not "read"; a star row is only inserted when
// starred is true. Returns the entry ID.
func seedEntry(t *testing.T, s *Store, userID, feedID int, title, status string, starred bool) int64 {
	t.Helper()
	var id int64
	entryURL := fmt.Sprintf("https://example.com/%s", title)
	err := s.DB.QueryRow(
		`INSERT INTO entries (feed_id, hash, title, url, content, published_at)
		 VALUES ($1, $2, $3, $4, $3, NOW())
		 RETURNING id`,
		feedID, fmt.Sprintf("hash-%d-%s", feedID, title), title, entryURL,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	if status != "read" {
		if _, err := s.DB.Exec(
			`INSERT INTO entry_read_status (user_id, article_url, entry_id, status) VALUES ($1, $2, $3, $4)`,
			userID, entryURL, id, status,
		); err != nil {
			t.Fatalf("seed read status: %v", err)
		}
	}
	if starred {
		if _, err := s.DB.Exec(
			`INSERT INTO entry_stars (user_id, article_url, entry_id) VALUES ($1, $2, $3)`,
			userID, entryURL, id,
		); err != nil {
			t.Fatalf("seed star: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM entries WHERE id = $1`, id)
	})
	return id
}

// TestListEntriesByFeedID verifies that filtering entries by feed_id
// does not produce a SQL syntax error. This is a regression test for a
// bug where the feed_id condition was appended with a dangling AND
// before the WHERE clause.
func TestListEntriesByFeedID(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, "test-feed-filter@example.com")
	feedID := seedFeed(t, s, userID, nil, "Test Feed")
	seedEntry(t, s, userID, feedID, "Entry A", "unread", false)
	seedEntry(t, s, userID, feedID, "Entry B", "read", true)

	fid := feedID
	entries, err := s.ListEntries(ctx, userID, &fid, nil, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries with feed_id failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for feed_id=%d, got %d", feedID, len(entries))
	}
}

// TestListEntriesByFolderID verifies that filtering by folder_id
// works without errors.
func TestListEntriesByFolderID(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, "test-cat-filter@example.com")

	// Create folder
	var folderID int
	err := s.DB.QueryRow(
		`INSERT INTO folders (user_id, title) VALUES ($1, 'Tech') RETURNING id`,
		userID,
	).Scan(&folderID)
	if err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM folders WHERE id = $1`, folderID) })

	feedID := seedFeed(t, s, userID, &folderID, "Folder Feed")
	seedEntry(t, s, userID, feedID, "Folder Entry", "unread", false)

	fid := folderID
	entries, err := s.ListEntries(ctx, userID, nil, &fid, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries with folder_id failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for folder_id=%d, got %d", folderID, len(entries))
	}
}

// TestListEntriesCombinedFilters verifies that multiple filters can be
// combined without SQL errors.
func TestListEntriesCombinedFilters(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, "test-combined@example.com")
	feedID := seedFeed(t, s, userID, nil, "Combined Feed")
	seedEntry(t, s, userID, feedID, "Unread Starred", "unread", true)
	seedEntry(t, s, userID, feedID, "Read Unstarred", "read", false)
	seedEntry(t, s, userID, feedID, "Unread Unstarred", "unread", false)

	fid := feedID
	starred := true
	entries, err := s.ListEntries(ctx, userID, &fid, nil, "unread", &starred, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries with combined filters failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (unread+starred), got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].Title != "Unread Starred" {
		t.Errorf("expected 'Unread Starred', got %q", entries[0].Title)
	}
}

// TestListEntriesNoFilters verifies the base case with no optional filters.
func TestListEntriesNoFilters(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, "test-nofilter@example.com")
	feedID := seedFeed(t, s, userID, nil, "No Filter Feed")
	seedEntry(t, s, userID, feedID, "Entry 1", "unread", false)
	seedEntry(t, s, userID, feedID, "Entry 2", "read", false)

	entries, err := s.ListEntries(ctx, userID, nil, nil, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries with no filters failed: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries, got %d", len(entries))
	}
}

// TestListEntriesSearch verifies that full-text search works.
func TestListEntriesSearch(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, "test-search@example.com")
	feedID := seedFeed(t, s, userID, nil, "Search Feed")
	seedEntry(t, s, userID, feedID, "Go programming language", "unread", false)
	seedEntry(t, s, userID, feedID, "Rust memory safety", "unread", false)

	entries, err := s.ListEntries(ctx, userID, nil, nil, "", nil, "Go", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries with search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry matching 'Go', got %d", len(entries))
	}
}
