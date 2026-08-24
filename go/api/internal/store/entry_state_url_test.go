package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// helper: seed a global feed + entry with a specific URL, returning the entry ID.
func seedFeedAndEntryWithURL(t *testing.T, s *Store, userID int, feedTitle, articleURL, entryTitle string) (feedID int, entryID int64) {
	t.Helper()
	err := s.DB.QueryRow(
		`INSERT INTO feeds (feed_url, title) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("https://feed.example.com/%s.xml", feedTitle), feedTitle,
	).Scan(&feedID)
	if err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM feeds WHERE id = $1`, feedID)
	})

	if _, err := s.DB.Exec(
		`INSERT INTO subscriptions (user_id, feed_id) VALUES ($1, $2)`,
		userID, feedID,
	); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	err = s.DB.QueryRow(
		`INSERT INTO entries (feed_id, hash, title, url, content, published_at)
		 VALUES ($1, $2, $3, $4, $3, NOW())
		 RETURNING id`,
		feedID, fmt.Sprintf("hash-%s", entryTitle), entryTitle, articleURL,
	).Scan(&entryID)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM entries WHERE id = $1`, entryID)
	})
	return feedID, entryID
}

func entryStarExists(t *testing.T, s *Store, userID int, articleURL string) bool {
	t.Helper()
	var n int
	err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(&n)
	if err != nil {
		t.Fatalf("check star: %v", err)
	}
	return n > 0
}

func entryReadStatus(t *testing.T, s *Store, userID int, articleURL string) (status string, exists bool) {
	t.Helper()
	err := s.DB.QueryRow(
		`SELECT status FROM entry_read_status WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(&status)
	if err != nil && err.Error() == "sql: no rows in result set" {
		return "", false
	}
	if err != nil {
		t.Fatalf("check read status: %v", err)
	}
	return status, true
}

// --- ToggleEntryStarredByURL ---

func TestToggleEntryStarredByURL_StarWithoutEntry(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "star-url-noentry@example.com")
	articleURL := "https://example.com/preview-article"

	// Star an article that has no materialized entry.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Preview Title", "Preview desc",
		"https://feed.example.com/rss", "Feed Title", "https://feed.example.com",
		"Author", nil, true,
	); err != nil {
		t.Fatalf("star by URL: %v", err)
	}

	if !entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to exist")
	}

	// The star row should carry the article metadata.
	var title, feedURL string
	err := s.DB.QueryRow(
		`SELECT title, feed_url FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(&title, &feedURL)
	if err != nil {
		t.Fatalf("read star row: %v", err)
	}
	if title != "Preview Title" {
		t.Errorf("title=%q, want 'Preview Title'", title)
	}
	if feedURL != "https://feed.example.com/rss" {
		t.Errorf("feed_url=%q, want 'https://feed.example.com/rss'", feedURL)
	}

	// ensureSharedEntry should have materialized the entry (feed_url was provided).
	var entryCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM entries WHERE url = $1`, articleURL,
	).Scan(&entryCount); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 1 {
		t.Errorf("expected 1 materialized entry, got %d", entryCount)
	}
}

func TestToggleEntryStarredByURL_StarWithoutFeedURL(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "star-url-nofeed@example.com")
	articleURL := "https://example.com/no-feed-article"

	// Star with empty feed_url — entry won't be materialized, but star persists.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "Desc",
		"", "", "",
		"Author", nil, true,
	); err != nil {
		t.Fatalf("star by URL: %v", err)
	}

	if !entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to exist even without feed_url")
	}

	// No entry should be materialized.
	var entryCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM entries WHERE url = $1`, articleURL,
	).Scan(&entryCount); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 0 {
		t.Errorf("expected 0 entries, got %d", entryCount)
	}
}

func TestToggleEntryStarredByURL_Unstar(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "unstar-url@example.com")
	articleURL := "https://example.com/unstar-article"

	// Star first.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star: %v", err)
	}

	// Unstar.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, false,
	); err != nil {
		t.Fatalf("unstar: %v", err)
	}

	if entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to be deleted after unstar")
	}
}

func TestToggleEntryStarredByURL_RestarUpdatesMetadata(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "restar-url@example.com")
	articleURL := "https://example.com/restar-article"

	// Star with one title.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Original Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star: %v", err)
	}

	// Re-star with a different title (e.g. feed provided updated metadata).
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Updated Title", "New desc", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("restar: %v", err)
	}

	var title, desc string
	err := s.DB.QueryRow(
		`SELECT title, description FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(&title, &desc)
	if err != nil {
		t.Fatalf("read star: %v", err)
	}
	if title != "Updated Title" {
		t.Errorf("title=%q, want 'Updated Title'", title)
	}
	if desc != "New desc" {
		t.Errorf("description=%q, want 'New desc'", desc)
	}
}

func TestToggleEntryStarredByURL_SameURLInMultipleFeeds(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "multi-feed-star@example.com")
	articleURL := "https://example.com/dup-url-article"

	// Create two feeds with the same article URL.
	_, entryID1 := seedFeedAndEntryWithURL(t, s, userID, "Feed A", articleURL, "Entry in Feed A")
	_, entryID2 := seedFeedAndEntryWithURL(t, s, userID, "Feed B", articleURL, "Entry in Feed B")

	if entryID1 == entryID2 {
		t.Fatal("expected different entry IDs for different feeds")
	}

	// Star by URL — should not error even though multiple entries have this URL.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star by URL with duplicate entries: %v", err)
	}

	if !entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to exist")
	}

	// Unstar should also work.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, false,
	); err != nil {
		t.Fatalf("unstar by URL: %v", err)
	}

	if entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to be deleted")
	}
}

// --- UpdateEntryStatusByURL ---

func TestUpdateEntryStatusByURL_MarkReadWithoutEntry(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "read-url-noentry@example.com")
	articleURL := "https://example.com/preview-read-article"

	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "read"); err != nil {
		t.Fatalf("mark read by URL: %v", err)
	}

	// Marking read deletes the row (absence = read), so no row should exist.
	status, exists := entryReadStatus(t, s, userID, articleURL)
	if exists {
		t.Errorf("expected no read-status row after marking read (absent = read), got status=%q", status)
	}

	// Verify the default state via GetEntryStatesByURLs reports 'read'.
	states, err := s.GetEntryStatesByURLs(ctx, userID, []string{articleURL})
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	if st := states[articleURL]; st.Status != "read" {
		t.Errorf("status=%q, want 'read'", st.Status)
	}
}

func TestUpdateEntryStatusByURL_MarkReadWithEntry(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "read-url-entry@example.com")
	articleURL := "https://example.com/read-with-entry"

	_, entryID := seedFeedAndEntryWithURL(t, s, userID, "Read Feed", articleURL, "Read Entry")

	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "read"); err != nil {
		t.Fatalf("mark read by URL: %v", err)
	}

	// Marking read deletes the row (absence = read).
	_, exists := entryReadStatus(t, s, userID, articleURL)
	if exists {
		t.Error("expected no read-status row after marking read (absent = read)")
	}

	// ListEntries should report the entry as read.
	fid := 0
	_ = fid
	entries, err := s.ListEntries(ctx, userID, nil, nil, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	for _, e := range entries {
		if e.ID == entryID {
			if e.Status != "read" {
				t.Errorf("status=%q, want 'read'", e.Status)
			}
		}
	}
}

func TestUpdateEntryStatusByURL_MarkUnread(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "unread-url@example.com")
	articleURL := "https://example.com/mark-unread"

	// Mark read first, then unread.
	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "read"); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "unread"); err != nil {
		t.Fatalf("mark unread: %v", err)
	}

	status, exists := entryReadStatus(t, s, userID, articleURL)
	if !exists {
		t.Fatal("expected read status row to exist (status='unread')")
	}
	if status != "unread" {
		t.Errorf("status=%q, want 'unread'", status)
	}
}

func TestUpdateEntryStatusByURL_SameURLInMultipleFeeds(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "multi-feed-read@example.com")
	articleURL := "https://example.com/dup-read-url"

	// Create two feeds with the same article URL.
	seedFeedAndEntryWithURL(t, s, userID, "Read Feed A", articleURL, "Entry A")
	seedFeedAndEntryWithURL(t, s, userID, "Read Feed B", articleURL, "Entry B")

	// Mark read by URL — must not error with "more than one row returned".
	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "read"); err != nil {
		t.Fatalf("mark read by URL with duplicate entries: %v", err)
	}

	// Marking read deletes the row (absence = read).
	_, exists := entryReadStatus(t, s, userID, articleURL)
	if exists {
		t.Error("expected no read-status row after marking read (absent = read)")
	}
}

func TestUpdateEntryStatusByURL_StatusPersistsAfterEntryDeleted(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "read-persist@example.com")
	articleURL := "https://example.com/persist-read"

	_, entryID := seedFeedAndEntryWithURL(t, s, userID, "Persist Feed", articleURL, "Persist Entry")

	// Mark unread (creates a row), then delete the entry.
	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "unread"); err != nil {
		t.Fatalf("mark unread: %v", err)
	}

	// Delete the entry (simulates feed cleanup).
	if _, err := s.DB.Exec(`DELETE FROM entries WHERE id = $1`, entryID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}

	// Read status should still exist (entry_id SET NULL on delete).
	status, exists := entryReadStatus(t, s, userID, articleURL)
	if !exists {
		t.Fatal("expected unread status to persist after entry deletion")
	}
	if status != "unread" {
		t.Errorf("status=%q, want 'unread'", status)
	}
}

// --- GetStarATProtoRkey / SetStarATProtoRkey (by article URL) ---

func TestStarATProtoRkey_ByURL(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "rkey-url@example.com")
	articleURL := "https://example.com/rkey-article"

	// Star first so the row exists.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star: %v", err)
	}

	// Get should return empty (no rkey set yet).
	rkey, err := s.GetStarATProtoRkey(ctx, userID, articleURL)
	if err != nil {
		t.Fatalf("get rkey: %v", err)
	}
	if rkey != "" {
		t.Errorf("expected empty rkey, got %q", rkey)
	}

	// Set the rkey.
	if err := s.SetStarATProtoRkey(ctx, userID, articleURL, "rkey-123"); err != nil {
		t.Fatalf("set rkey: %v", err)
	}

	// Get should return the rkey.
	rkey, err = s.GetStarATProtoRkey(ctx, userID, articleURL)
	if err != nil {
		t.Fatalf("get rkey: %v", err)
	}
	if rkey != "rkey-123" {
		t.Errorf("rkey=%q, want 'rkey-123'", rkey)
	}

	// Clear the rkey.
	if err := s.SetStarATProtoRkey(ctx, userID, articleURL, ""); err != nil {
		t.Fatalf("clear rkey: %v", err)
	}

	rkey, _ = s.GetStarATProtoRkey(ctx, userID, articleURL)
	if rkey != "" {
		t.Errorf("expected empty rkey after clear, got %q", rkey)
	}
}

// --- UpsertStarWithRkey / DeleteStarByRkey (relay backfeed) ---

func TestUpsertStarWithRkey_MaterializesEntry(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "relay-star@example.com")
	articleURL := "https://example.com/relay-star-article"
	pub := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	if err := s.UpsertStarWithRkey(ctx, userID,
		articleURL, "Relay Star Title", "Relay desc",
		"https://feed.example.com/rss", "Feed Title", "https://feed.example.com",
		"Author", &pub, "rkey-relay-star",
	); err != nil {
		t.Fatalf("upsert star with rkey: %v", err)
	}

	// Star should exist with the rkey.
	rkey, _ := s.GetStarATProtoRkey(ctx, userID, articleURL)
	if rkey != "rkey-relay-star" {
		t.Errorf("rkey=%q, want 'rkey-relay-star'", rkey)
	}

	// Entry should be materialized (feed_url was provided).
	var entryCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM entries WHERE url = $1`, articleURL,
	).Scan(&entryCount); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 1 {
		t.Errorf("expected 1 materialized entry, got %d", entryCount)
	}

	// Star row should carry the metadata.
	var title string
	if err := s.DB.QueryRow(
		`SELECT title FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(&title); err != nil {
		t.Fatalf("read star title: %v", err)
	}
	if title != "Relay Star Title" {
		t.Errorf("title=%q, want 'Relay Star Title'", title)
	}
}

func TestUpsertStarWithRkey_NoFeedURL(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "relay-star-nofeed@example.com")
	articleURL := "https://example.com/relay-star-no-feed"

	if err := s.UpsertStarWithRkey(ctx, userID,
		articleURL, "Title", "", "", "", "", "", nil, "rkey-no-feed",
	); err != nil {
		t.Fatalf("upsert star: %v", err)
	}

	if !entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to exist")
	}

	// No entry materialized.
	var entryCount int
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM entries WHERE url = $1`, articleURL,
	).Scan(&entryCount); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 0 {
		t.Errorf("expected 0 entries, got %d", entryCount)
	}
}

func TestDeleteStarByRkey(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "relay-unstar@example.com")
	articleURL := "https://example.com/relay-unstar-article"

	// Seed a star with an rkey.
	if err := s.UpsertStarWithRkey(ctx, userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, "rkey-del-star",
	); err != nil {
		t.Fatalf("upsert star: %v", err)
	}

	// Delete by rkey.
	if err := s.DeleteStarByRkey(ctx, userID, "rkey-del-star"); err != nil {
		t.Fatalf("delete star by rkey: %v", err)
	}

	if entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to be deleted")
	}
}

// --- ListEntries reads star/read state from URL-keyed tables ---

func TestListEntries_StarFromURLKeyedTable(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "list-star@example.com")
	articleURL := "https://example.com/list-star-article"

	feedID, entryID := seedFeedAndEntryWithURL(t, s, userID, "List Star Feed", articleURL, "List Star Entry")

	// Star by URL (not by entry ID).
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "List Star Entry", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star by URL: %v", err)
	}

	// ListEntries should show the entry as starred.
	fid := feedID
	entries, err := s.ListEntries(ctx, userID, &fid, nil, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e.ID == entryID {
			found = true
			if !e.Starred {
				t.Error("expected entry to be starred from URL-keyed star")
			}
		}
	}
	if !found {
		t.Fatal("entry not found in ListEntries result")
	}
}

func TestListEntries_ReadStatusFromURLKeyedTable(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "list-read@example.com")
	articleURL := "https://example.com/list-read-article"

	feedID, entryID := seedFeedAndEntryWithURL(t, s, userID, "List Read Feed", articleURL, "List Read Entry")

	// Mark read by URL.
	if err := s.UpdateEntryStatusByURL(ctx, userID, articleURL, "read"); err != nil {
		t.Fatalf("mark read by URL: %v", err)
	}

	// ListEntries should show the entry as read.
	fid := feedID
	entries, err := s.ListEntries(ctx, userID, &fid, nil, "", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e.ID == entryID {
			found = true
			if e.Status != "read" {
				t.Errorf("status=%q, want 'read'", e.Status)
			}
		}
	}
	if !found {
		t.Fatal("entry not found in ListEntries result")
	}
}

// --- Existing entryId-based star/read still work ---

func TestToggleEntryStarred_ExistingEntryID(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "star-id@example.com")
	articleURL := "https://example.com/star-by-id"

	_, entryID := seedFeedAndEntryWithURL(t, s, userID, "ID Star Feed", articleURL, "ID Star Entry")

	// Star by entry ID (the existing path).
	if err := s.ToggleEntryStarred(ctx, entryID, userID, true); err != nil {
		t.Fatalf("star by ID: %v", err)
	}

	if !entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to exist")
	}

	// Unstar by entry ID.
	if err := s.ToggleEntryStarred(ctx, entryID, userID, false); err != nil {
		t.Fatalf("unstar by ID: %v", err)
	}

	if entryStarExists(t, s, userID, articleURL) {
		t.Error("expected star row to be deleted")
	}
}

func TestUpdateEntryStatus_ExistingEntryID(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "read-id@example.com")
	articleURL := "https://example.com/read-by-id"

	_, entryID := seedFeedAndEntryWithURL(t, s, userID, "ID Read Feed", articleURL, "ID Read Entry")

	// Mark read by entry IDs (the existing path).
	if err := s.UpdateEntryStatus(ctx, []int64{entryID}, userID, "read"); err != nil {
		t.Fatalf("mark read by ID: %v", err)
	}

	// Marking read deletes the row (absence = read).
	_, exists := entryReadStatus(t, s, userID, articleURL)
	if exists {
		t.Error("expected no read-status row after marking read (absent = read)")
	}
}

// --- GetEntryStatesByURLs (preview state lookup) ---

func TestGetEntryStatesByURLs_AllAbsent(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "states-absent@example.com")

	urls := []string{
		"https://example.com/no-state-1",
		"https://example.com/no-state-2",
	}
	states, err := s.GetEntryStatesByURLs(ctx, userID, urls)
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 entries in map, got %d", len(states))
	}
	for _, url := range urls {
		st, ok := states[url]
		if !ok {
			t.Errorf("expected URL %q to be in map", url)
			continue
		}
		if st.Status != "read" {
			t.Errorf("status=%q, want 'read' (absent = read)", st.Status)
		}
		if st.Starred {
			t.Error("expected starred=false (absent = unstarred)")
		}
	}
}

func TestGetEntryStatesByURLs_WithReadAndStarred(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "states-mixed@example.com")

	readURL := "https://example.com/read-article"
	starredURL := "https://example.com/starred-article"
	bothURL := "https://example.com/both-article"
	noneURL := "https://example.com/none-article"

	// Mark one read by URL.
	if err := s.UpdateEntryStatusByURL(ctx, userID, readURL, "read"); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	// Star one by URL.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		starredURL, "Starred", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star: %v", err)
	}
	// Both unread and starred.
	if err := s.UpdateEntryStatusByURL(ctx, userID, bothURL, "unread"); err != nil {
		t.Fatalf("mark both unread: %v", err)
	}
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		bothURL, "Both", "", "https://feed.example.com/rss", "Feed", "", "", nil, true,
	); err != nil {
		t.Fatalf("star both: %v", err)
	}

	urls := []string{readURL, starredURL, bothURL, noneURL}
	states, err := s.GetEntryStatesByURLs(ctx, userID, urls)
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	if len(states) != 4 {
		t.Fatalf("expected 4 entries in map, got %d", len(states))
	}

	if st := states[readURL]; st.Status != "read" || st.Starred {
		t.Errorf("readURL: status=%q starred=%v, want 'read' false", st.Status, st.Starred)
	}
	if st := states[starredURL]; st.Status != "read" || !st.Starred {
		t.Errorf("starredURL: status=%q starred=%v, want 'read' true", st.Status, st.Starred)
	}
	if st := states[bothURL]; st.Status != "unread" || !st.Starred {
		t.Errorf("bothURL: status=%q starred=%v, want 'unread' true", st.Status, st.Starred)
	}
	if st := states[noneURL]; st.Status != "read" || st.Starred {
		t.Errorf("noneURL: status=%q starred=%v, want 'read' false", st.Status, st.Starred)
	}
}

func TestGetEntryStatesByURLs_EmptyInput(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "states-empty@example.com")

	states, err := s.GetEntryStatesByURLs(ctx, userID, nil)
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected empty map, got %d entries", len(states))
	}
}

func TestGetEntryStatesByURLs_URLKeyIsInputURL(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "states-key@example.com")
	articleURL := "https://example.com/key-test"

	// Star an article by URL (no materialized entry).
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		articleURL, "Title", "", "", "", "", "", nil, true,
	); err != nil {
		t.Fatalf("star: %v", err)
	}

	// Lookup must return the state keyed by the input URL, not by the
	// (possibly NULL) joined column. This is the regression test for the
	// bug where the query selected rs.article_url instead of u.article_url.
	states, err := s.GetEntryStatesByURLs(ctx, userID, []string{articleURL})
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}

	st, ok := states[articleURL]
	if !ok {
		t.Fatalf("expected URL %q to be in map (bug: query selected rs.article_url instead of u.article_url)", articleURL)
	}
	if !st.Starred {
		t.Error("expected starred=true")
	}
}

// TestStarReadStateUniformAcrossURLVariants is the motivating property for
// URL normalization: two textually-different but canonically-equivalent
// article URLs (tracking params, fragment, trailing slash, query order)
// must share the same star/read state, so an article starred or marked
// read via one source variant shows as starred/read via any other.
func TestStarReadStateUniformAcrossURLVariants(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "uniform-variants@example.com")

	starVariant := "https://example.com/article?utm_source=feed#top"
	readVariant := "https://example.com/article/#fbclid=abc"
	lookupVariant := "https://example.com/article"

	// Star via one variant and mark read via another.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		starVariant, "Title", "", "", "", "", "", nil, true,
	); err != nil {
		t.Fatalf("star by variant: %v", err)
	}
	if err := s.UpdateEntryStatusByURL(ctx, userID, readVariant, "read"); err != nil {
		t.Fatalf("mark read by variant: %v", err)
	}

	// Look up state via a third, differently-spelled variant.
	states, err := s.GetEntryStatesByURLs(ctx, userID, []string{lookupVariant})
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	st, ok := states[lookupVariant]
	if !ok {
		t.Fatalf("expected %q in map", lookupVariant)
	}
	if !st.Starred {
		t.Error("expected starred=true across URL variants (normalization broken)")
	}
	if st.Status != "read" {
		t.Errorf("status=%q, want 'read' across URL variants (normalization broken)", st.Status)
	}

	// Unstar via the lookup variant must clear the star (keyed by the
	// normalized URL). entryStarExists queries raw SQL, so check the
	// normalized key that is actually stored.
	if err := s.ToggleEntryStarredByURL(ctx, userID,
		lookupVariant, "Title", "", "", "", "", "", nil, false,
	); err != nil {
		t.Fatalf("unstar by variant: %v", err)
	}
	if entryStarExists(t, s, userID, "https://example.com/article") {
		t.Error("expected unstar by one variant to clear the star set via another variant")
	}
}
