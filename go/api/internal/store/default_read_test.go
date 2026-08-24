package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMarkEntryUnreadForSubscribers verifies that when a new entry is created
// by the feed processor, an 'unread' row is inserted for every subscriber.
func TestMarkEntryUnreadForSubscribers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "unread-subs@example.com")
	feedID := seedFeed(t, s, userID, nil, "Unread Subs Feed")

	// Create an entry directly (simulating what the processor does).
	var entryID int64
	err := s.DB.QueryRow(
		`INSERT INTO entries (feed_id, hash, title, url, content, published_at)
		 VALUES ($1, $2, $3, $4, $3, NOW()) RETURNING id`,
		feedID, "hash-unread-subs", "Unread Subs Entry", "https://example.com/unread-subs",
	).Scan(&entryID)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM entries WHERE id = $1`, entryID) })

	// Mark unread for subscribers (what the processor calls).
	if err := s.MarkEntryUnreadForSubscribers(ctx, entryID, feedID); err != nil {
		t.Fatalf("MarkEntryUnreadForSubscribers: %v", err)
	}

	// The subscriber should have an 'unread' row.
	status, exists := entryReadStatus(t, s, userID, "https://example.com/unread-subs")
	if !exists {
		t.Fatal("expected unread row for subscriber")
	}
	if status != "unread" {
		t.Errorf("status=%q, want 'unread'", status)
	}
}

// TestMarkEntryUnreadForSubscribers_NoSubscribers verifies no error when the
// feed has no subscribers.
func TestMarkEntryUnreadForSubscribers_NoSubscribers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	// Create a feed with no subscribers.
	var feedID int
	err := s.DB.QueryRow(
		`INSERT INTO feeds (feed_url, title) VALUES ($1, $2) RETURNING id`,
		"https://example.com/no-subs.xml", "No Subs Feed",
	).Scan(&feedID)
	if err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM feeds WHERE id = $1`, feedID) })

	var entryID int64
	err = s.DB.QueryRow(
		`INSERT INTO entries (feed_id, hash, title, url, content, published_at)
		 VALUES ($1, $2, $3, $4, $3, NOW()) RETURNING id`,
		feedID, "hash-no-subs", "No Subs Entry", "https://example.com/no-subs-entry",
	).Scan(&entryID)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM entries WHERE id = $1`, entryID) })

	if err := s.MarkEntryUnreadForSubscribers(ctx, entryID, feedID); err != nil {
		t.Fatalf("MarkEntryUnreadForSubscribers with no subscribers: %v", err)
	}
}

// TestMarkShareUnreadForFollowers verifies that when a share is created, an
// 'unread' row is inserted for every follower.
func TestMarkShareUnreadForFollowers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	base := time.Now().UnixNano() % 99999
	sharerID := seedUser(t, s, fmt.Sprintf("sharer-%d@example.com", time.Now().UnixNano()))
	followerID := seedUser(t, s, fmt.Sprintf("follower-%d@example.com", time.Now().UnixNano()))
	hSharer := fmt.Sprintf("sharerh%d", base)
	_, _ = s.UpsertHandle(ctx, sharerID, hSharer, "")
	_, _ = s.UpsertHandle(ctx, followerID, fmt.Sprintf("followerh%d", base), "")
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id = $1`, followerID)
	})

	// Follower follows sharer.
	if err := s.FollowUser(ctx, followerID, hSharer); err != nil {
		t.Fatalf("follow: %v", err)
	}

	articleURL := "https://example.com/shared-unread"
	// Mark unread for followers (what ShareArticle calls on a new share).
	if err := s.MarkShareUnreadForFollowers(ctx, sharerID, articleURL, 0); err != nil {
		t.Fatalf("MarkShareUnreadForFollowers: %v", err)
	}

	// The follower should have an 'unread' row.
	status, exists := entryReadStatus(t, s, followerID, articleURL)
	if !exists {
		t.Fatal("expected unread row for follower")
	}
	if status != "unread" {
		t.Errorf("status=%q, want 'unread'", status)
	}
}

// TestShareArticle_MarksUnreadForFollowers verifies that ShareArticle creates
// unread rows for followers on a new share, but not on a reshare.
func TestShareArticle_MarksUnreadForFollowers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	base := time.Now().UnixNano() % 99999
	sharerID := seedUser(t, s, fmt.Sprintf("share-unread-%d@example.com", time.Now().UnixNano()))
	followerID := seedUser(t, s, fmt.Sprintf("follow-unread-%d@example.com", time.Now().UnixNano()))
	hSharer := fmt.Sprintf("shareunreadh%d", base)
	_, _ = s.UpsertHandle(ctx, sharerID, hSharer, "")
	_, _ = s.UpsertHandle(ctx, followerID, fmt.Sprintf("followunreadh%d", base), "")
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id = $1`, followerID)
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id = $1`, sharerID)
	})

	// Follower follows sharer.
	if err := s.FollowUser(ctx, followerID, hSharer); err != nil {
		t.Fatalf("follow: %v", err)
	}

	articleURL := "https://example.com/share-creates-unread"
	// First share — should mark unread for follower.
	if _, err := s.ShareArticle(ctx, sharerID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil,
	); err != nil {
		t.Fatalf("ShareArticle (first): %v", err)
	}

	status, exists := entryReadStatus(t, s, followerID, articleURL)
	if !exists {
		t.Fatal("expected unread row for follower after share")
	}
	if status != "unread" {
		t.Errorf("status=%q, want 'unread'", status)
	}

	// Mark read (delete the row).
	if err := s.UpdateEntryStatusByURL(ctx, followerID, articleURL, "read"); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	_, exists = entryReadStatus(t, s, followerID, articleURL)
	if exists {
		t.Fatal("expected no row after marking read")
	}

	// Reshare — should NOT re-mark unread for follower.
	if _, err := s.ShareArticle(ctx, sharerID,
		articleURL, "Updated Title", "", "https://feed.example.com/rss", "Feed", "", "", nil,
	); err != nil {
		t.Fatalf("ShareArticle (reshare): %v", err)
	}

	_, exists = entryReadStatus(t, s, followerID, articleURL)
	if exists {
		t.Error("expected no unread row after reshare (existing share should not re-notify)")
	}
}

// TestShareArticle_NoFollowersNoError verifies that sharing with no followers
// does not error.
func TestShareArticle_NoFollowersNoError(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	sharerID := seedUser(t, s, fmt.Sprintf("no-followers-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id = $1`, sharerID)
	})

	if _, err := s.ShareArticle(ctx, sharerID,
		"https://example.com/no-followers-share", "Title", "",
		"https://feed.example.com/rss", "Feed", "", "", nil,
	); err != nil {
		t.Fatalf("ShareArticle with no followers: %v", err)
	}
}
