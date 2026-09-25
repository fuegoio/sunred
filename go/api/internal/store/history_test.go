package store

import (
	"context"
	"testing"
	"time"
)

// setReadTime pins a read-status row's changed_at to a fixed time so ordering
// assertions are deterministic regardless of how fast the reads happen.
func setReadTime(t *testing.T, s *Store, userID int, articleURL string, at time.Time) {
	t.Helper()
	if _, err := s.DB.Exec(
		`UPDATE entry_read_status SET changed_at = $3 WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL, at,
	); err != nil {
		t.Fatalf("set read time: %v", err)
	}
}

func TestListHistory_OrderAndDedup(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "history-order@example.com")

	_, entryA := seedFeedAndEntryWithURL(t, s, userID, "History Feed A", "https://example.com/history-a", "Article A")
	_, entryB := seedFeedAndEntryWithURL(t, s, userID, "History Feed B", "https://example.com/history-b", "Article B")

	// Read A first, then B.
	if err := s.UpdateEntryStatus(ctx, []int64{entryA}, userID, "read"); err != nil {
		t.Fatalf("mark A read: %v", err)
	}
	if err := s.UpdateEntryStatus(ctx, []int64{entryB}, userID, "read"); err != nil {
		t.Fatalf("mark B read: %v", err)
	}
	t1 := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC)
	setReadTime(t, s, userID, "https://example.com/history-a", t1)
	setReadTime(t, s, userID, "https://example.com/history-b", t2)

	history, err := s.ListHistory(ctx, userID, 50, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].URL != "https://example.com/history-b" || history[1].URL != "https://example.com/history-a" {
		t.Errorf("expected [B, A] (most recent read first), got [%s, %s]", history[0].URL, history[1].URL)
	}
	if !history[0].ChangedAt.Equal(t2) {
		t.Errorf("history[0].ChangedAt=%v, want %v (read time, not published time)", history[0].ChangedAt, t2)
	}

	// Re-read A: it moves to the top and still appears exactly once.
	if err := s.UpdateEntryStatus(ctx, []int64{entryA}, userID, "read"); err != nil {
		t.Fatalf("re-read A: %v", err)
	}
	t3 := time.Date(2025, 6, 3, 10, 0, 0, 0, time.UTC)
	setReadTime(t, s, userID, "https://example.com/history-a", t3)

	history, err = s.ListHistory(ctx, userID, 50, 0)
	if err != nil {
		t.Fatalf("ListHistory after re-read: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries after re-read (one per article), got %d", len(history))
	}
	if history[0].URL != "https://example.com/history-a" {
		t.Errorf("expected A first after re-read, got %s", history[0].URL)
	}
}

func TestListHistory_ExcludesUnreadAndUnreadEntries(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "history-exclude@example.com")

	_, entryA := seedFeedAndEntryWithURL(t, s, userID, "History Feed C", "https://example.com/history-c", "Article C")
	_, _ = seedFeedAndEntryWithURL(t, s, userID, "History Feed D", "https://example.com/history-d", "Article D")

	// A is read then marked unread again; B is never touched.
	if err := s.UpdateEntryStatus(ctx, []int64{entryA}, userID, "read"); err != nil {
		t.Fatalf("mark A read: %v", err)
	}
	if err := s.UpdateEntryStatus(ctx, []int64{entryA}, userID, "unread"); err != nil {
		t.Fatalf("mark A unread: %v", err)
	}

	history, err := s.ListHistory(ctx, userID, 50, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	for _, e := range history {
		if e.URL == "https://example.com/history-c" {
			t.Error("article marked unread should not appear in history")
		}
	}
	if len(history) != 0 {
		t.Errorf("expected empty history (A unread, B never read), got %d entries", len(history))
	}
}

func TestListHistory_StarredFlagAndFeed(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "history-star@example.com")

	_, entryA := seedFeedAndEntryWithURL(t, s, userID, "History Feed E", "https://example.com/history-e", "Article E")
	if err := s.UpdateEntryStatus(ctx, []int64{entryA}, userID, "read"); err != nil {
		t.Fatalf("mark A read: %v", err)
	}
	if err := s.ToggleEntryStarred(ctx, entryA, userID, true); err != nil {
		t.Fatalf("star A: %v", err)
	}

	history, err := s.ListHistory(ctx, userID, 50, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	e := history[0]
	if !e.Starred {
		t.Error("expected starred=true on history entry")
	}
	if e.Feed == nil || e.Feed.Title != "History Feed E" {
		t.Errorf("expected nested feed with title 'History Feed E', got %+v", e.Feed)
	}
	if e.Title != "Article E" {
		t.Errorf("title=%q, want 'Article E'", e.Title)
	}
}
