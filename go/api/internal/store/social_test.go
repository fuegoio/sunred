package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/urlnorm"
)

// --- Handle / Profile ---

func TestUpsertHandle_Valid(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("handle-valid-%d@example.com", time.Now().UnixNano()))

	p, err := s.UpsertHandle(ctx, userID, "fuego", "My bio")
	if err != nil {
		t.Fatalf("UpsertHandle: %v", err)
	}
	if p.Handle != "fuego" {
		t.Errorf("handle=%q, want 'fuego'", p.Handle)
	}
	if p.Bio != "My bio" {
		t.Errorf("bio=%q, want 'My bio'", p.Bio)
	}
	if p.UserID != userID {
		t.Errorf("user_id=%d, want %d", p.UserID, userID)
	}
}

func TestUpsertHandle_UpdatesBioAndHandle(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("handle-update-%d@example.com", time.Now().UnixNano()))

	_, err := s.UpsertHandle(ctx, userID, "oldalias", "old bio")
	if err != nil {
		t.Fatalf("first UpsertHandle: %v", err)
	}
	p, err := s.UpsertHandle(ctx, userID, "newalias", "new bio")
	if err != nil {
		t.Fatalf("second UpsertHandle: %v", err)
	}
	if p.Handle != "newalias" {
		t.Errorf("handle=%q, want 'newalias'", p.Handle)
	}
	if p.Bio != "new bio" {
		t.Errorf("bio=%q, want 'new bio'", p.Bio)
	}
}

func TestUpsertHandle_Invalid(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("handle-invalid-%d@example.com", time.Now().UnixNano()))

	cases := []string{"ab", "", "has spaces", "has@at"}
	for _, h := range cases {
		_, err := s.UpsertHandle(ctx, userID, h, "")
		if err == nil {
			t.Errorf("expected error for handle %q, got nil", h)
		}
	}
}

func TestUpsertHandle_UniqueConstraint(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	handle := fmt.Sprintf("uniquetest%d", time.Now().UnixNano()%99999)
	u1 := seedUser(t, s, fmt.Sprintf("unique1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("unique2-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
	})

	_, err := s.UpsertHandle(ctx, u1, handle, "")
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	_, err = s.UpsertHandle(ctx, u2, handle, "")
	if err == nil {
		t.Fatal("expected ErrHandleTaken, got nil")
	}
	if err != ErrHandleTaken {
		t.Errorf("expected ErrHandleTaken, got %v", err)
	}
}

func TestGetProfileByHandle(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	handle := fmt.Sprintf("profiletest%d", time.Now().UnixNano()%99999)
	userID := seedUser(t, s, fmt.Sprintf("profile-%d@example.com", time.Now().UnixNano()))

	_, err := s.UpsertHandle(ctx, userID, handle, "bio text")
	if err != nil {
		t.Fatalf("UpsertHandle: %v", err)
	}

	p, err := s.GetProfileByHandle(ctx, handle, 0)
	if err != nil {
		t.Fatalf("GetProfileByHandle: %v", err)
	}
	if p.Handle != handle {
		t.Errorf("handle=%q, want %q", p.Handle, handle)
	}
	if p.Bio != "bio text" {
		t.Errorf("bio=%q, want 'bio text'", p.Bio)
	}
	if p.FollowerCount != 0 {
		t.Errorf("follower_count=%d, want 0", p.FollowerCount)
	}
}

func TestGetProfileByHandle_NotFound(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	_, err := s.GetProfileByHandle(ctx, "doesnotexist99999", 0)
	if err != ErrProfileNotFound {
		t.Errorf("expected ErrProfileNotFound, got %v", err)
	}
}

// --- Follows ---

func TestFollowUser_And_Unfollow(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	handle1 := fmt.Sprintf("follower%d", time.Now().UnixNano()%99999)
	handle2 := fmt.Sprintf("followee%d", time.Now().UnixNano()%99999)
	u1 := seedUser(t, s, fmt.Sprintf("follow-u1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("follow-u2-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id=$1 OR followee_id=$1 OR follower_id=$2 OR followee_id=$2`, u1, u2)
	})

	_, err := s.UpsertHandle(ctx, u1, handle1, "")
	if err != nil {
		t.Fatalf("handle1: %v", err)
	}
	_, err = s.UpsertHandle(ctx, u2, handle2, "")
	if err != nil {
		t.Fatalf("handle2: %v", err)
	}

	// Follow.
	if err := s.FollowUser(ctx, u1, handle2); err != nil {
		t.Fatalf("FollowUser: %v", err)
	}

	// IsFollowing reflected in profile.
	p, err := s.GetProfileByHandle(ctx, handle2, u1)
	if err != nil {
		t.Fatalf("GetProfileByHandle after follow: %v", err)
	}
	if !p.IsFollowing {
		t.Error("expected IsFollowing=true")
	}
	if p.FollowerCount != 1 {
		t.Errorf("follower_count=%d, want 1", p.FollowerCount)
	}

	// Duplicate follow should fail.
	if err := s.FollowUser(ctx, u1, handle2); err != ErrAlreadyFollowing {
		t.Errorf("expected ErrAlreadyFollowing, got %v", err)
	}

	// Unfollow.
	if err := s.UnfollowUser(ctx, u1, handle2); err != nil {
		t.Fatalf("UnfollowUser: %v", err)
	}

	// Count should be back to 0.
	p, err = s.GetProfileByHandle(ctx, handle2, u1)
	if err != nil {
		t.Fatalf("GetProfileByHandle after unfollow: %v", err)
	}
	if p.FollowerCount != 0 {
		t.Errorf("follower_count=%d, want 0 after unfollow", p.FollowerCount)
	}
}

func TestFollowUser_CannotFollowSelf(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	handle := fmt.Sprintf("selffollow%d", time.Now().UnixNano()%99999)
	u := seedUser(t, s, fmt.Sprintf("self-%d@example.com", time.Now().UnixNano()))

	_, err := s.UpsertHandle(ctx, u, handle, "")
	if err != nil {
		t.Fatalf("UpsertHandle: %v", err)
	}
	if err := s.FollowUser(ctx, u, handle); err != ErrCannotFollowSelf {
		t.Errorf("expected ErrCannotFollowSelf, got %v", err)
	}
}

func TestFollowUser_ProfileNotFound(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("noprofile-%d@example.com", time.Now().UnixNano()))
	if err := s.FollowUser(ctx, u, "nonexistenthandle9999"); err != ErrProfileNotFound {
		t.Errorf("expected ErrProfileNotFound, got %v", err)
	}
}

func TestListFollowing(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	base := time.Now().UnixNano() % 99999
	u1 := seedUser(t, s, fmt.Sprintf("list-f1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("list-f2-%d@example.com", time.Now().UnixNano()))
	u3 := seedUser(t, s, fmt.Sprintf("list-f3-%d@example.com", time.Now().UnixNano()))
	h2 := fmt.Sprintf("listf2h%d", base)
	h3 := fmt.Sprintf("listf3h%d", base)
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id=$1`, u1)
	})
	_, _ = s.UpsertHandle(ctx, u1, fmt.Sprintf("listf1h%d", base), "")
	_, _ = s.UpsertHandle(ctx, u2, h2, "")
	_, _ = s.UpsertHandle(ctx, u3, h3, "")
	_ = s.FollowUser(ctx, u1, h2)
	_ = s.FollowUser(ctx, u1, h3)

	following, err := s.ListFollowing(ctx, u1)
	if err != nil {
		t.Fatalf("ListFollowing: %v", err)
	}
	if len(following) != 2 {
		t.Errorf("expected 2 following, got %d", len(following))
	}
	for _, p := range following {
		if !p.IsFollowing {
			t.Errorf("profile %q: IsFollowing should be true", p.Handle)
		}
	}
}

// --- Shared Articles ---

func TestShareArticle_And_Timeline(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	base := time.Now().UnixNano() % 99999
	u1 := seedUser(t, s, fmt.Sprintf("share-u1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("share-u2-%d@example.com", time.Now().UnixNano()))
	h1 := fmt.Sprintf("shareu1h%d", base)
	h2 := fmt.Sprintf("shareu2h%d", base)
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id IN ($1,$2)`, u1, u2)
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id=$1`, u1)
	})

	_, _ = s.UpsertHandle(ctx, u1, h1, "")
	_, _ = s.UpsertHandle(ctx, u2, h2, "")

	pub := time.Now().Add(-24 * time.Hour)
	sa, err := s.ShareArticle(ctx, u2,
		"https://example.com/great-post",
		"Great Post", "A great read",
		"https://feed.example.com/rss", "Example Feed", "https://example.com",
		"Author", &pub,
	)
	if err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}
	if sa.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if sa.ArticleURL != "https://example.com/great-post" {
		t.Errorf("article_url=%q", sa.ArticleURL)
	}

	// u1 follows u2 — should see the share in timeline.
	_ = s.FollowUser(ctx, u1, h2)

	timeline, err := s.ListSocialTimeline(ctx, u1, 50, 0)
	if err != nil {
		t.Fatalf("ListSocialTimeline: %v", err)
	}
	if len(timeline) != 1 {
		t.Errorf("expected 1 timeline item, got %d", len(timeline))
	}
	if timeline[0].ArticleURL != "https://example.com/great-post" {
		t.Errorf("article_url=%q", timeline[0].ArticleURL)
	}
	if timeline[0].SharerHandle != h2 {
		t.Errorf("sharer_handle=%q, want %q", timeline[0].SharerHandle, h2)
	}
}

func TestShareArticle_Idempotent(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("share-idem-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id=$1`, u)
	})
	url := "https://example.com/idempotent"
	sa1, err := s.ShareArticle(ctx, u, url, "Title", "", "", "", "", "", nil)
	if err != nil {
		t.Fatalf("first share: %v", err)
	}
	// Resharing same URL updates shared_at but keeps same row.
	sa2, err := s.ShareArticle(ctx, u, url, "Updated Title", "", "", "", "", "", nil)
	if err != nil {
		t.Fatalf("second share: %v", err)
	}
	if sa1.ID != sa2.ID {
		t.Errorf("expected same ID on reshare, got %d vs %d", sa1.ID, sa2.ID)
	}
}

func TestShareArticle_NoDuplicateInSubscribedFeed(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	u := seedUser(t, s, fmt.Sprintf("share-dup-%s@example.com", suffix))
	feedURL := fmt.Sprintf("https://feed.example.com/%s/rss", suffix)
	articleURL := fmt.Sprintf("https://example.com/%s/dup-test", suffix)

	feed, err := s.GetOrCreateFeed(ctx, feedURL, "https://example.com", "Example Feed", "")
	if err != nil {
		t.Fatalf("GetOrCreateFeed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id=$1`, u)
		_, _ = s.DB.Exec(`DELETE FROM subscriptions WHERE user_id=$1`, u)
		_, _ = s.DB.Exec(`DELETE FROM feeds WHERE id=$1`, feed.ID)
	})

	if _, err := s.CreateSubscription(ctx, u, feed.ID, nil, ""); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	// Simulate a feed refresh ingesting the article — both the processor and
	// ensureSharedEntry now hash with sha256(url), so they collide on the
	// (feed_id, hash) unique constraint.
	pub := time.Now().Add(-1 * time.Hour)
	refreshHash := hashItemTest(articleURL)
	entryID, err := s.CreateEntry(ctx, feed.ID, refreshHash, "Great Post", articleURL, "", "Author", "", "desc", pub, nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entryID == 0 {
		t.Fatal("expected non-zero entry ID from CreateEntry")
	}

	// Mark the article unread — the default read state is now 'read', so we
	// must explicitly mark it to reproduce the "unread" scenario.
	if err := s.UpdateEntryStatusByURL(ctx, u, articleURL, "unread"); err != nil {
		t.Fatalf("UpdateEntryStatusByURL: %v", err)
	}

	// Share the same article using the same feed_url.
	if _, err := s.ShareArticle(ctx, u, articleURL, "Great Post", "desc", feedURL, "Example Feed", "https://example.com", "Author", &pub); err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}

	// The unread feed should contain exactly one entry for this article.
	entries, err := s.ListEntries(ctx, u, nil, nil, "unread", nil, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	var count int
	for _, e := range entries {
		if e.URL == articleURL {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry for %s in unread feed, got %d", articleURL, count)
	}

	// And only one entries row should exist for this URL in the feed.
	var rowCount int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE feed_id=$1 AND url=$2`, feed.ID, articleURL).Scan(&rowCount); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected 1 entries row, got %d", rowCount)
	}
}

func TestUnshareArticle(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("unshare-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id=$1`, u)
	})
	sa, err := s.ShareArticle(ctx, u, "https://example.com/to-unshare", "Title", "", "", "", "", "", nil)
	if err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}
	if err := s.UnshareArticle(ctx, sa.ID, u); err != nil {
		t.Fatalf("UnshareArticle: %v", err)
	}
	// Second unshare should return ErrShareNotFound.
	if err := s.UnshareArticle(ctx, sa.ID, u); err != ErrShareNotFound {
		t.Errorf("expected ErrShareNotFound, got %v", err)
	}
}

func TestSocialTimeline_EmptyWhenNoFollows(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("timeline-empty-%d@example.com", time.Now().UnixNano()))
	items, err := s.ListSocialTimeline(ctx, u, 50, 0)
	if err != nil {
		t.Fatalf("ListSocialTimeline: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty timeline, got %d items", len(items))
	}
}

// --- Feed Subscribers ---

func TestCountFeedSubscribers(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	feedURL := fmt.Sprintf("https://shared-feed-%d.example.com/rss", time.Now().UnixNano())
	u1 := seedUser(t, s, fmt.Sprintf("subs-u1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("subs-u2-%d@example.com", time.Now().UnixNano()))

	// One global feed; two users subscribe to it.
	feed, err := s.GetOrCreateFeed(ctx, feedURL, "", "Shared Feed", "")
	if err != nil {
		t.Fatalf("GetOrCreateFeed: %v", err)
	}
	if _, err := s.CreateSubscription(ctx, u1, feed.ID, nil, ""); err != nil {
		t.Fatalf("subscribe u1: %v", err)
	}
	if _, err := s.CreateSubscription(ctx, u2, feed.ID, nil, ""); err != nil {
		t.Fatalf("subscribe u2: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM subscriptions WHERE feed_id = $1`, feed.ID)
		_, _ = s.DB.Exec(`DELETE FROM feeds WHERE id = $1`, feed.ID)
	})

	n, err := s.CountFeedSubscribers(ctx, feed.ID)
	if err != nil {
		t.Fatalf("CountFeedSubscribers: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 subscribers, got %d", n)
	}
}

func TestListFeedSubscribers_OnlyWithHandles(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	feedURL := fmt.Sprintf("https://subscriber-list-%d.example.com/rss", time.Now().UnixNano())
	handle := fmt.Sprintf("sublisth%d", time.Now().UnixNano()%99999)
	u1 := seedUser(t, s, fmt.Sprintf("sublist-u1-%d@example.com", time.Now().UnixNano()))
	// u2 has no handle — should be excluded from the public subscriber list.
	var u2 int
	err := s.DB.QueryRow(
		`INSERT INTO users (did, handle) VALUES ($1, '') RETURNING id`,
		fmt.Sprintf("did:plc:sublist-u2-%d", time.Now().UnixNano()),
	).Scan(&u2)
	if err != nil {
		t.Fatalf("seed u2: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM users WHERE id = $1`, u2)
	})

	feed, err := s.GetOrCreateFeed(ctx, feedURL, "", "Feed", "")
	if err != nil {
		t.Fatalf("GetOrCreateFeed: %v", err)
	}
	if _, err := s.CreateSubscription(ctx, u1, feed.ID, nil, ""); err != nil {
		t.Fatalf("subscribe u1: %v", err)
	}
	if _, err := s.CreateSubscription(ctx, u2, feed.ID, nil, ""); err != nil {
		t.Fatalf("subscribe u2: %v", err)
	}
	// Only u1 has a handle.
	_, _ = s.UpsertHandle(ctx, u1, handle, "")
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM subscriptions WHERE feed_id = $1`, feed.ID)
		_, _ = s.DB.Exec(`DELETE FROM feeds WHERE id = $1`, feed.ID)
	})

	subs, err := s.ListFeedSubscribers(ctx, feed.ID)
	if err != nil {
		t.Fatalf("ListFeedSubscribers: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 public subscriber (with handle), got %d", len(subs))
	}
	if subs[0].Handle != handle {
		t.Errorf("subscriber handle=%q, want %q", subs[0].Handle, handle)
	}
}

// --- GetSharedArticleByURL ---

func TestGetSharedArticleByURL(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("getshared-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id=$1`, u)
	})
	url := "https://example.com/check-article"
	_, _ = s.ShareArticle(ctx, u, url, "T", "", "", "", "", "", nil)

	sa, err := s.GetSharedArticleByURL(ctx, u, url)
	if err != nil {
		t.Fatalf("GetSharedArticleByURL: %v", err)
	}
	if sa == nil {
		t.Fatal("expected non-nil SharedArticle, got nil")
	}
	if sa.ArticleURL != url {
		t.Errorf("article_url=%q", sa.ArticleURL)
	}

	// Not shared by this user.
	sa2, err := s.GetSharedArticleByURL(ctx, u, "https://not-shared.example.com")
	if err != nil {
		t.Fatalf("GetSharedArticleByURL (not found): %v", err)
	}
	if sa2 != nil {
		t.Error("expected nil for unshared article")
	}
}

// --- ListPublicFeedsByUser ---

func TestListPublicFeedsByUser(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("publicfeeds-%d@example.com", time.Now().UnixNano()))
	f1 := seedFeed(t, s, u, nil, fmt.Sprintf("pf1-%d", time.Now().UnixNano()))
	f2 := seedFeed(t, s, u, nil, fmt.Sprintf("pf2-%d", time.Now().UnixNano()))
	_ = f1
	_ = f2

	feeds, err := s.ListPublicFeedsByUser(ctx, u)
	if err != nil {
		t.Fatalf("ListPublicFeedsByUser: %v", err)
	}
	if len(feeds) < 2 {
		t.Errorf("expected at least 2 feeds, got %d", len(feeds))
	}
	// Every returned feed must be one the user subscribes to.
	seen := map[int]bool{}
	for _, f := range feeds {
		seen[f.ID] = true
	}
	if !seen[f1] || !seen[f2] {
		t.Errorf("expected both seeded feeds to be listed; got %v", seen)
	}
}

// --- Handle validation edge cases ---

func TestUpsertHandle_AllowedChars(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("handlechar-%d@example.com", time.Now().UnixNano()))

	valid := []string{"abc", "ABC123", "my-handle", "my_handle", "aaa"}
	for _, h := range valid {
		// Use unique suffix to avoid handle collision.
		handle := fmt.Sprintf("%s%d", h, time.Now().UnixNano()%9999)
		_, err := s.UpsertHandle(ctx, u, handle, "")
		if err != nil {
			t.Errorf("expected valid handle %q to succeed, got %v", handle, err)
		}
	}

	invalid := []string{"a b", "a@b", "a.b", strings.Repeat("x", 65)}
	u2 := seedUser(t, s, fmt.Sprintf("handlechar2-%d@example.com", time.Now().UnixNano()))
	for _, h := range invalid {
		_, err := s.UpsertHandle(ctx, u2, h, "")
		if err == nil {
			t.Errorf("expected invalid handle %q to fail, got nil", h)
		}
	}
}

// hashItemTest mirrors the reader processor's hashItem (sha256 of the
// normalized article URL) so tests can simulate feed-refresh entries that
// collide with ensureSharedEntry's hash on the same (feed_id, hash) key.
func hashItemTest(link string) string {
	h := sha256.Sum256([]byte(urlnorm.URL(link)))
	return hex.EncodeToString(h[:])
}
