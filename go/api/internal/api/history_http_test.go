package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/store"
)

// TestListHistory_HTTP exercises the /v1/history endpoint end-to-end: entries
// marked read appear most-recent-first, re-reading an entry moves it to the
// front without duplicating it, and unauthenticated requests are rejected.
func TestListHistory_HTTP(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	feed, err := env.store.GetOrCreateFeed(ctx,
		fmt.Sprintf("https://feed.example.com/%s/rss", suffix),
		fmt.Sprintf("https://feed.example.com/%s", suffix),
		"History Feed", "")
	if err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if _, err := env.store.CreateSubscription(ctx, env.userID, feed.ID, nil, ""); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	pub := time.Now().Add(-2 * time.Hour)
	newEntry := func(slug, title string) int64 {
		t.Helper()
		id, err := env.store.CreateEntry(ctx, feed.ID, "hash-"+slug, title,
			fmt.Sprintf("https://example.com/history-%s-%s", slug, suffix), "", "Author", "content", "desc", pub, nil)
		if err != nil {
			t.Fatalf("seed entry: %v", err)
		}
		return id
	}
	entryA := newEntry("a", "Article A")
	entryB := newEntry("b", "Article B")

	markRead := func(id int64) {
		t.Helper()
		resp := env.do(t, http.MethodPut, "/v1/entries", map[string]any{
			"entry_ids": []int64{id},
			"status":    "read",
		})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("mark read %d: expected 204, got %d", id, resp.StatusCode)
		}
	}
	// Pin read timestamps so ordering assertions are deterministic.
	pinReadAt := func(articleURL string, at time.Time) {
		t.Helper()
		if _, err := env.store.DB.Exec(
			`UPDATE entry_read_status SET changed_at = $3 WHERE user_id = $1 AND article_url = $2`,
			env.userID, articleURL, at,
		); err != nil {
			t.Fatalf("pin read time: %v", err)
		}
	}
	urlA := fmt.Sprintf("https://example.com/history-a-%s", suffix)
	urlB := fmt.Sprintf("https://example.com/history-b-%s", suffix)

	listHistory := func() []store.Entry {
		t.Helper()
		resp := env.do(t, http.MethodGet, "/v1/history?limit=10", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET history: expected 200, got %d", resp.StatusCode)
		}
		var entries []store.Entry
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			t.Fatalf("decode history: %v", err)
		}
		return entries
	}

	// Unauthenticated requests are rejected.
	if resp := env.doUnauth(t, http.MethodGet, "/v1/history", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth history: expected 401, got %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}

	// Empty history before anything is read.
	if entries := listHistory(); len(entries) != 0 {
		t.Fatalf("expected empty history, got %d entries", len(entries))
	}

	// Read A then B: history lists most recent first.
	markRead(entryA)
	markRead(entryB)
	pinReadAt(urlA, time.Now().Add(-2*time.Minute))
	pinReadAt(urlB, time.Now().Add(-1*time.Minute))

	entries := listHistory()
	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(entries))
	}
	if entries[0].URL != urlB || entries[1].URL != urlA {
		t.Errorf("expected [B, A], got [%s, %s]", entries[0].URL, entries[1].URL)
	}
	if entries[0].Feed == nil || entries[0].Feed.Title != "History Feed" {
		t.Errorf("expected nested feed on history entry, got %+v", entries[0].Feed)
	}

	// Re-read A: single row moves to the front.
	markRead(entryA)
	pinReadAt(urlA, time.Now())
	entries = listHistory()
	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries after re-read, got %d", len(entries))
	}
	if entries[0].URL != urlA {
		t.Errorf("expected A first after re-read, got %s", entries[0].URL)
	}
}
