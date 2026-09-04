package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMarkEntryUnreadForSubscribers verifies that MarkEntryUnreadForSubscribers
// inserts an 'unread' row for every subscriber of the feed, and that an
// existing row (e.g. set to 'read' by the subscriber) is NOT overwritten.
func TestMarkEntryUnreadForSubscribers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	sub1 := seedUser(t, s, "fanout-sub1@example.com")
	sub2 := seedUser(t, s, "fanout-sub2@example.com")
	nonSub := seedUser(t, s, "fanout-nonsub@example.com")

	feedID := seedFeed(t, s, sub1, nil, "Fanout Feed")
	// Subscribe sub2 to the same feed.
	if _, err := s.CreateSubscription(ctx, sub2, feedID, nil, ""); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	articleURL := fmt.Sprintf("https://example.com/feed-%d/fanout-article", feedID)
	entryID, err := s.CreateEntry(ctx, feedID, "hash-fanout", "Fanout Entry",
		articleURL, "", "Author", "content", "desc", time.Now(), nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entryID == 0 {
		t.Fatal("expected a new entry")
	}

	// sub2 already marked this article read before the fan-out ran (e.g. it
	// was shared to them and they opened it). The fan-out must not resurrect it.
	if err := s.UpdateEntryStatusByURL(ctx, sub2, articleURL, "read"); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	if err := s.MarkEntryUnreadForSubscribers(ctx, feedID, articleURL, entryID); err != nil {
		t.Fatalf("MarkEntryUnreadForSubscribers: %v", err)
	}

	// sub1: no prior row -> unread.
	if status, exists := entryReadStatus(t, s, sub1, articleURL); !exists {
		t.Fatal("expected unread row for subscriber 1")
	} else if status != "unread" {
		t.Errorf("sub1 status=%q, want 'unread'", status)
	}
	// sub2: had a 'read' row -> must stay 'read' (ON CONFLICT DO NOTHING).
	if status, exists := entryReadStatus(t, s, sub2, articleURL); !exists {
		t.Fatal("expected existing read row for subscriber 2")
	} else if status != "read" {
		t.Errorf("sub2 status=%q, want 'read' (existing row must not be overwritten)", status)
	}
	// non-subscriber: no row.
	if _, exists := entryReadStatus(t, s, nonSub, articleURL); exists {
		t.Error("expected no row for a non-subscriber")
	}
}

// TestProcessFeed_NewEntryUnreadForSubscriber simulates the feed-processor path
// (CreateEntry + MarkEntryUnreadForSubscribers) and asserts the new entry shows
// up unread for a subscriber, while absence (no row, no subscription) is read.
// This is the regression test for the bug where new feed articles were never
// marked unread.
func TestProcessFeed_NewEntryUnreadForSubscriber(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "proc-unread@example.com")
	feedID := seedFeed(t, s, userID, nil, "Processor Feed")

	articleURL := fmt.Sprintf("https://example.com/feed-%d/proc-entry", feedID)
	entryID, err := s.CreateEntry(ctx, feedID, "hash-proc", "Proc Entry",
		articleURL, "", "Author", "content", "desc", time.Now(), nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entryID == 0 {
		t.Fatal("expected a new entry")
	}
	if err := s.MarkEntryUnreadForSubscribers(ctx, feedID, articleURL, entryID); err != nil {
		t.Fatalf("MarkEntryUnreadForSubscribers: %v", err)
	}

	fid := feedID
	// Unread feed surfaces the new entry.
	if entries, err := s.ListEntries(ctx, userID, &fid, nil, "unread", nil, "", "", 50, 0); err != nil {
		t.Fatalf("ListEntries unread: %v", err)
	} else if !containsEntryByURL(entries, articleURL) {
		t.Error("expected new entry to appear in the unread feed")
	}
	// Read feed does not.
	if entries, err := s.ListEntries(ctx, userID, &fid, nil, "read", nil, "", "", 50, 0); err != nil {
		t.Fatalf("ListEntries read: %v", err)
	} else if containsEntryByURL(entries, articleURL) {
		t.Error("expected new entry to NOT appear in the read feed")
	}
}

// TestSubscribeBacklogReadNewEntriesUnread models the subscribe flow: the backlog
// is cleared to read on subscribe, and entries fetched afterwards are marked
// unread for the subscriber by the processor fan-out.
func TestSubscribeBacklogReadNewEntriesUnread(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "sub-backlog@example.com")
	feedID := seedFeed(t, s, userID, nil, "Subscribe Backlog Feed")

	// Backlog article present before the subscribe clear.
	backlogURL := fmt.Sprintf("https://example.com/feed-%d/backlog", feedID)
	if _, err := s.CreateEntry(ctx, feedID, "hash-backlog", "Backlog",
		backlogURL, "", "Author", "content", "desc", time.Now(), nil); err != nil {
		t.Fatalf("CreateEntry backlog: %v", err)
	}

	// Subscribe clears the backlog, the way SubscribeFeed does.
	if err := s.MarkFeedEntriesRead(ctx, feedID, userID); err != nil {
		t.Fatalf("MarkFeedEntriesRead: %v", err)
	}

	// A new article arrives after subscribe — the processor creates it and
	// fans out an 'unread' row.
	newURL := fmt.Sprintf("https://example.com/feed-%d/new-after-subscribe", feedID)
	newID, err := s.CreateEntry(ctx, feedID, "hash-new-after", "New After Subscribe",
		newURL, "", "Author", "content", "desc", time.Now(), nil)
	if err != nil {
		t.Fatalf("CreateEntry new: %v", err)
	}
	if newID == 0 {
		t.Fatal("expected a new entry")
	}
	if err := s.MarkEntryUnreadForSubscribers(ctx, feedID, newURL, newID); err != nil {
		t.Fatalf("MarkEntryUnreadForSubscribers: %v", err)
	}

	fid := feedID

	// Backlog article should be read.
	if entries, err := s.ListEntries(ctx, userID, &fid, nil, "read", nil, "", "", 50, 0); err != nil {
		t.Fatalf("ListEntries read: %v", err)
	} else if !containsEntryByURL(entries, backlogURL) {
		t.Error("expected backlog article to be read after subscribe clear")
	} else if containsEntryByURL(entries, newURL) {
		t.Error("expected new article to NOT be read")
	}

	// New article should be unread.
	if entries, err := s.ListEntries(ctx, userID, &fid, nil, "unread", nil, "", "", 50, 0); err != nil {
		t.Fatalf("ListEntries unread: %v", err)
	} else if !containsEntryByURL(entries, newURL) {
		t.Error("expected new article to be unread after subscribe")
	} else if containsEntryByURL(entries, backlogURL) {
		t.Error("expected backlog article to NOT be unread")
	}
}

func containsEntryByURL(entries []Entry, url string) bool {
	for _, e := range entries {
		if e.URL == url {
			return true
		}
	}
	return false
}

// TestMarkAllEntriesRead_DuplicateURLAcrossFeeds is the regression test for the
// "pq: ON CONFLICT DO UPDATE command cannot affect row a second time" error.
// When the same article URL exists in multiple subscribed feeds, the upsert
// source yields one row per entry; the ON CONFLICT (user_id, article_url) then
// tries to update the same conflict key more than once in a single statement,
// which Postgres rejects. DISTINCT ON (e.url) collapses the source to one row
// per URL so the statement succeeds.
func TestMarkAllEntriesRead_DuplicateURLAcrossFeeds(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "dup-url-markall@example.com")
	articleURL := "https://example.com/shared-across-feeds"

	// Two subscribed feeds carrying the same article URL.
	seedFeedAndEntryWithURL(t, s, userID, "Dup Feed A", articleURL, "Entry in A")
	seedFeedAndEntryWithURL(t, s, userID, "Dup Feed B", articleURL, "Entry in B")

	if err := s.MarkAllEntriesRead(ctx, userID); err != nil {
		t.Fatalf("MarkAllEntriesRead with duplicate URL across feeds: %v", err)
	}

	status, exists := entryReadStatus(t, s, userID, articleURL)
	if !exists {
		t.Fatal("expected a read status row for the shared URL")
	}
	if status != "read" {
		t.Errorf("status=%q, want 'read'", status)
	}
}

// TestMarkFeedEntriesRead_DuplicateURLAcrossFeeds guards the feed-scoped path
// against the same conflict. The target feed itself has one entry, but a second
// subscribed feed shares the same URL, so the visible-entry filter's OR branch
// can surface both rows for the same conflict key.
func TestMarkFeedEntriesRead_DuplicateURLAcrossFeeds(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "dup-url-markfeed@example.com")
	articleURL := "https://example.com/shared-across-feeds-2"

	feedID, _ := seedFeedAndEntryWithURL(t, s, userID, "Target Feed", articleURL, "Target Entry")
	seedFeedAndEntryWithURL(t, s, userID, "Other Feed", articleURL, "Other Entry")

	if err := s.MarkFeedEntriesRead(ctx, feedID, userID); err != nil {
		t.Fatalf("MarkFeedEntriesRead with duplicate URL across feeds: %v", err)
	}

	status, exists := entryReadStatus(t, s, userID, articleURL)
	if !exists {
		t.Fatal("expected a read status row for the shared URL")
	}
	if status != "read" {
		t.Errorf("status=%q, want 'read'", status)
	}
}

// TestUpdateEntryStatus_DuplicateURLAcrossFeeds guards the entry-ID bulk path:
// passing entry IDs that map to the same article URL must not raise the
// "affect row a second time" error.
func TestUpdateEntryStatus_DuplicateURLAcrossFeeds(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, "dup-url-update@example.com")
	articleURL := "https://example.com/shared-across-feeds-3"

	_, entryID1 := seedFeedAndEntryWithURL(t, s, userID, "Upd Feed A", articleURL, "Entry A")
	_, entryID2 := seedFeedAndEntryWithURL(t, s, userID, "Upd Feed B", articleURL, "Entry B")

	if err := s.UpdateEntryStatus(ctx, []int64{entryID1, entryID2}, userID, "read"); err != nil {
		t.Fatalf("UpdateEntryStatus with duplicate URL across feeds: %v", err)
	}

	status, exists := entryReadStatus(t, s, userID, articleURL)
	if !exists {
		t.Fatal("expected a read status row for the shared URL")
	}
	if status != "read" {
		t.Errorf("status=%q, want 'read'", status)
	}
}
