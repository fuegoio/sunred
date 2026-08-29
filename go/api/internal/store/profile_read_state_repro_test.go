package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestProfileReadState_ReflectsViewerState asserts that the shared-articles
// listing used by the profile page surfaces the viewer's read state, and that
// reads performed through either path (entry-id timeline read, URL-based
// preview toggle) flip a share from 'unread' to 'read'. Guards against a
// regression where the profile always shows shares as unread regardless of the
// viewer's actual read state.
func TestProfileReadState_ReflectsViewerState(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	sharer := seedUser(t, s, fmt.Sprintf("sharer-%d@example.com", time.Now().UnixNano()))
	viewer := seedUser(t, s, fmt.Sprintf("viewer-%d@example.com", time.Now().UnixNano()))
	sharerHandle := fmt.Sprintf("sharer%d", time.Now().UnixNano()%99999)
	if _, err := s.UpsertHandle(ctx, sharer, sharerHandle, ""); err != nil {
		t.Fatalf("sharer handle: %v", err)
	}
	if err := s.FollowUser(ctx, viewer, sharerHandle); err != nil {
		t.Fatalf("follow: %v", err)
	}

	articleURL := fmt.Sprintf("https://example.com/article-%d", time.Now().UnixNano())
	feedURL := fmt.Sprintf("https://feed.example.com/%d/rss", time.Now().UnixNano())
	pub := time.Now().Add(-1 * time.Hour)

	sa, err := s.ShareArticle(ctx, sharer, articleURL, "Title", "desc",
		feedURL, "Feed", "https://example.com", "Author", &pub)
	if err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}
	if sa.EntryID == nil || *sa.EntryID == 0 {
		t.Fatalf("share has no entry_id; entry not materialized")
	}

	// Profile listing for viewer right after share: should be 'unread'.
	got, err := s.ListSharedArticlesByUser(ctx, sharer, viewer)
	if err != nil {
		t.Fatalf("ListSharedArticlesByUser (before read): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 share, got %d", len(got))
	}
	if got[0].Status != "unread" {
		t.Errorf("before read: status=%q, want 'unread'", got[0].Status)
	}

	// Viewer reads the article via the entry-id path (timeline read).
	if err := s.UpdateEntryStatus(ctx, []int64{*sa.EntryID}, viewer, "read"); err != nil {
		t.Fatalf("UpdateEntryStatus: %v", err)
	}
	got, err = s.ListSharedArticlesByUser(ctx, sharer, viewer)
	if err != nil {
		t.Fatalf("ListSharedArticlesByUser (after entry-id read): %v", err)
	}
	if got[0].Status != "read" {
		t.Errorf("after entry-id read: status=%q, want 'read'", got[0].Status)
	}

	// The URL-based read path (profile preview toggle) must also be reflected.
	if err := s.UpdateEntryStatusByURL(ctx, viewer, articleURL, "unread"); err != nil {
		t.Fatalf("mark unread by url: %v", err)
	}
	got, err = s.ListSharedArticlesByUser(ctx, sharer, viewer)
	if err != nil {
		t.Fatalf("ListSharedArticlesByUser (after unread): %v", err)
	}
	if got[0].Status != "unread" {
		t.Errorf("after mark unread by url: status=%q, want 'unread'", got[0].Status)
	}
	if err := s.UpdateEntryStatusByURL(ctx, viewer, articleURL, "read"); err != nil {
		t.Fatalf("mark read by url: %v", err)
	}
	got, err = s.ListSharedArticlesByUser(ctx, sharer, viewer)
	if err != nil {
		t.Fatalf("ListSharedArticlesByUser (after url read): %v", err)
	}
	if got[0].Status != "read" {
		t.Errorf("after mark read by url: status=%q, want 'read'", got[0].Status)
	}

	// Anonymous viewer (viewerID = 0) gets no status join — the struct's
	// Status field stays empty, which the frontend maps to its default.
	got, err = s.ListSharedArticlesByUser(ctx, sharer, 0)
	if err != nil {
		t.Fatalf("ListSharedArticlesByUser (anonymous): %v", err)
	}
	if got[0].Status != "" {
		t.Errorf("anonymous viewer: status=%q, want '' (no join)", got[0].Status)
	}
}
