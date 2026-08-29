package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestProfileReadState_HTTP verifies the public-profile endpoint surfaces the
// viewer's read state for shared articles over HTTP (with auth), and reflects
// reads performed via the entry-id (timeline) and URL (preview) paths.
func TestProfileReadState_HTTP(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	viewer := env.userID

	// Sharer: a second user with a handle.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sharerDID := fmt.Sprintf("did:plc:sharer-%s", suffix)
	sharerHandle := fmt.Sprintf("sharer%s", suffix)
	sharerID, _, err := env.store.GetOrCreateUserByDID(ctx, sharerDID, sharerHandle)
	if err != nil {
		t.Fatalf("seed sharer: %v", err)
	}
	t.Cleanup(func() { _, _ = env.store.DB.Exec(`DELETE FROM users WHERE id = $1`, sharerID) })
	if _, err := env.store.UpsertHandle(ctx, sharerID, sharerHandle, ""); err != nil {
		t.Fatalf("sharer handle: %v", err)
	}
	if err := env.store.FollowUser(ctx, viewer, sharerHandle); err != nil {
		t.Fatalf("follow: %v", err)
	}

	articleURL := fmt.Sprintf("https://example.com/http-%d", time.Now().UnixNano())
	feedURL := fmt.Sprintf("https://feed.example.com/%s/rss", suffix)
	pub := time.Now().Add(-1 * time.Hour)
	sa, err := env.store.ShareArticle(ctx, sharerID, articleURL, "Title", "desc",
		feedURL, "Feed", "https://example.com", "Author", &pub)
	if err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}
	if sa.EntryID == nil {
		t.Fatal("no entry_id")
	}

	// Unauthenticated request must be rejected (profile is behind auth).
	if resp := env.doUnauth(t, http.MethodGet, "/api/v1/users/"+sharerHandle, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth profile: expected 401, got %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	getStatus := func() string {
		t.Helper()
		resp := env.do(t, http.MethodGet, "/api/v1/users/"+sharerHandle, nil)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			t.Fatalf("GET profile: %d", resp.StatusCode)
		}
		var out struct {
			SharedArticles []struct {
				ArticleURL string `json:"article_url"`
				Status     string `json:"status"`
				Starred    bool   `json:"starred"`
			} `json:"shared_articles"`
		}
		readJSON(t, resp, &out)
		if len(out.SharedArticles) != 1 {
			t.Fatalf("expected 1 share, got %d", len(out.SharedArticles))
		}
		return out.SharedArticles[0].Status
	}

	if st := getStatus(); st != "unread" {
		t.Fatalf("before read: status=%q, want 'unread'", st)
	}

	// Read via the URL-based endpoint (profile preview toggle path).
	resp := env.do(t, http.MethodPut, "/v1/entries/by-url", map[string]any{
		"article_url": articleURL,
		"status":      "read",
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT by-url read: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	if st := getStatus(); st != "read" {
		t.Fatalf("after url read: status=%q, want 'read'", st)
	}

	// Mark unread again, then read via the entry-id (timeline) path.
	resp = env.do(t, http.MethodPut, "/v1/entries/by-url", map[string]any{
		"article_url": articleURL,
		"status":      "unread",
	})
	_ = resp.Body.Close()
	if st := getStatus(); st != "unread" {
		t.Fatalf("after mark unread: status=%q, want 'unread'", st)
	}

	resp = env.do(t, http.MethodPut, "/v1/entries", map[string]any{
		"entry_ids": []int64{*sa.EntryID},
		"status":    "read",
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT entries read: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	if st := getStatus(); st != "read" {
		t.Fatalf("after entry-id read: status=%q, want 'read'", st)
	}
}
