package fanout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fuegoio/sunred/go/relay/internal/store"
)

// mockPDSServer builds a test PDS that serves listRecords for the given
// collections. The records map is keyed by collection name → list of records.
func mockPDSServer(t *testing.T, collections map[string][]map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.atproto.repo.listRecords", func(w http.ResponseWriter, r *http.Request) {
		col := r.URL.Query().Get("collection")
		records, ok := collections[col]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var recs []map[string]any
		for i, rec := range records {
			recs = append(recs, map[string]any{
				"uri":   "at://did:plc:test/" + col + "/rkey" + jsonInt(i),
				"cid":   "bafy" + jsonInt(i),
				"value": rec,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"records": recs,
			"cursor":  "", // single page
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func jsonInt(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestBackfillDID_FeedSubscriptions(t *testing.T) {
	st := newStubStore()
	pds := mockPDSServer(t, map[string][]map[string]any{
		"io.sunred.feed.subscription": {
			{
				"feedUrl":   "https://example.com/rss",
				"siteUrl":   "https://example.com",
				"title":     "Example Feed",
				"createdAt": "2025-01-01T00:00:00Z",
			},
			{
				"feedUrl":   "https://blog.example.com/atom",
				"siteUrl":   "https://blog.example.com",
				"title":     "Blog",
				"createdAt": "2025-02-01T00:00:00Z",
			},
		},
	})

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	f.backfillDID(context.Background(), "did:plc:test", pds.URL)

	// Two feed subscriptions should be recorded.
	if len(st.feedSubs) != 2 {
		t.Errorf("expected 2 feed subs, got %d: %v", len(st.feedSubs), st.feedSubs)
	}

	// Two feedSubscription events should be emitted.
	feedSubEvents := 0
	for _, evt := range st.events {
		if evt.EventType == "feedSubscription" {
			feedSubEvents++
			var p map[string]any
			_ = json.Unmarshal(evt.Payload, &p)
			if siteURL, _ := p["siteUrl"].(string); siteURL == "" {
				t.Error("feedSubscription event missing siteUrl")
			}
			if title, _ := p["title"].(string); title == "" {
				t.Error("feedSubscription event missing title")
			}
		}
	}
	if feedSubEvents != 2 {
		t.Errorf("expected 2 feedSubscription events, got %d", feedSubEvents)
	}
}

func TestBackfillDID_Follows(t *testing.T) {
	st := newStubStore()
	pds := mockPDSServer(t, map[string][]map[string]any{
		"io.sunred.graph.follow": {
			{"subject": "did:plc:bob", "createdAt": "2025-01-01T00:00:00Z"},
			{"subject": "did:plc:carol", "createdAt": "2025-02-01T00:00:00Z"},
		},
	})

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	f.backfillDID(context.Background(), "did:plc:alice", pds.URL)

	if st.followCounts["did:plc:bob"] != 1 {
		t.Errorf("follow count for bob=%d, want 1", st.followCounts["did:plc:bob"])
	}
	if st.followCounts["did:plc:carol"] != 1 {
		t.Errorf("follow count for carol=%d, want 1", st.followCounts["did:plc:carol"])
	}
	followEvents := 0
	for _, evt := range st.events {
		if evt.EventType == "follow" {
			followEvents++
		}
	}
	if followEvents != 2 {
		t.Errorf("expected 2 follow events, got %d", followEvents)
	}
}

func TestBackfillDID_Shares(t *testing.T) {
	st := newStubStore()
	pds := mockPDSServer(t, map[string][]map[string]any{
		"io.sunred.share.article": {
			{
				"articleUrl":  "https://example.com/article1",
				"title":       "Article One",
				"description": "Description one",
				"feedUrl":     "https://feed.example.com/rss",
				"feedTitle":   "Feed",
				"feedSiteUrl": "https://feed.example.com",
				"author":      "Author",
				"sharedAt":    "2025-01-01T00:00:00Z",
				"publishedAt": "2024-12-01T00:00:00Z",
			},
		},
	})

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	f.backfillDID(context.Background(), "did:plc:sharer", pds.URL)

	// One share should be recorded.
	if len(st.shares) != 1 {
		t.Errorf("expected 1 share, got %d", len(st.shares))
	}

	// One share event should be emitted with full metadata.
	shareEvents := 0
	for _, evt := range st.events {
		if evt.EventType == "share" {
			shareEvents++
			var p map[string]any
			_ = json.Unmarshal(evt.Payload, &p)
			if p["description"] != "Description one" {
				t.Errorf("share event description=%v, want 'Description one'", p["description"])
			}
			if p["author"] != "Author" {
				t.Errorf("share event author=%v, want 'Author'", p["author"])
			}
			if p["feedTitle"] != "Feed" {
				t.Errorf("share event feedTitle=%v, want 'Feed'", p["feedTitle"])
			}
			if p["publishedAt"] != "2024-12-01T00:00:00Z" {
				t.Errorf("share event publishedAt=%v, want '2024-12-01T00:00:00Z'", p["publishedAt"])
			}
		}
	}
	if shareEvents != 1 {
		t.Errorf("expected 1 share event, got %d", shareEvents)
	}
}

func TestBackfillDID_Stars(t *testing.T) {
	st := newStubStore()
	pds := mockPDSServer(t, map[string][]map[string]any{
		"io.sunred.entry.star": {
			{
				"articleUrl":  "https://example.com/starred1",
				"title":       "Starred One",
				"description": "Description one",
				"feedUrl":     "https://feed.example.com/rss",
				"feedTitle":   "Feed",
				"feedSiteUrl": "https://feed.example.com",
				"author":      "Author",
				"publishedAt": "2024-12-01T00:00:00Z",
				"createdAt":   "2025-01-01T00:00:00Z",
			},
			{
				"articleUrl": "https://example.com/starred2",
				"title":      "Starred Two",
				"createdAt":  "2025-02-01T00:00:00Z",
			},
		},
	})

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	f.backfillDID(context.Background(), "did:plc:starrer", pds.URL)

	// Two stars should be recorded.
	if len(st.stars) != 2 {
		t.Errorf("expected 2 stars, got %d: %v", len(st.stars), st.stars)
	}

	// Two star events should be emitted with full metadata.
	starEvents := 0
	for _, evt := range st.events {
		if evt.EventType != "star" {
			continue
		}
		starEvents++
		var p map[string]any
		_ = json.Unmarshal(evt.Payload, &p)
		if articleURL, _ := p["articleUrl"].(string); articleURL == "" {
			t.Error("star event missing articleUrl")
		}
		// The first record carries the full metadata; verify it round-trips.
		if title, _ := p["title"].(string); title == "Starred One" {
			if p["description"] != "Description one" {
				t.Errorf("star event description=%v, want 'Description one'", p["description"])
			}
			if p["author"] != "Author" {
				t.Errorf("star event author=%v, want 'Author'", p["author"])
			}
			if p["feedTitle"] != "Feed" {
				t.Errorf("star event feedTitle=%v, want 'Feed'", p["feedTitle"])
			}
			if p["publishedAt"] != "2024-12-01T00:00:00Z" {
				t.Errorf("star event publishedAt=%v, want '2024-12-01T00:00:00Z'", p["publishedAt"])
			}
		}
	}
	if starEvents != 2 {
		t.Errorf("expected 2 star events, got %d", starEvents)
	}
}

func TestBackfillAndSubscribe_EmitsBackfillComplete(t *testing.T) {
	st := newStubStore()
	pds := mockPDSServer(t, map[string][]map[string]any{})

	f := &Fanout{
		store:          st,
		httpClient:     &http.Client{},
		subscribers:    make(map[string]chan *store.RelayEvent),
		workers:        make(map[string]context.CancelFunc),
		testPDSURL:     pds.URL,
		reconnectDelay: 0,
	}

	// Use a cancellable context so EnsureSubscribed's worker doesn't block.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f.BackfillAndSubscribe(ctx, "did:plc:test", pds.URL)

	// The last event should be backfillComplete.
	found := false
	for _, evt := range st.events {
		if evt.EventType == "backfillComplete" {
			found = true
			var p map[string]any
			_ = json.Unmarshal(evt.Payload, &p)
			if p["did"] != "did:plc:test" {
				t.Errorf("backfillComplete did=%v, want did:plc:test", p["did"])
			}
		}
	}
	if !found {
		t.Error("expected a backfillComplete event")
	}
}

func TestBackfillDID_EmptyRepo(t *testing.T) {
	st := newStubStore()
	// PDS returns 404 for all collections (no records).
	pds := mockPDSServer(t, map[string][]map[string]any{})

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	// Should not panic, should emit no events.
	f.backfillDID(context.Background(), "did:plc:test", pds.URL)
	if len(st.events) != 0 {
		t.Errorf("expected 0 events for empty repo, got %d", len(st.events))
	}
}

func TestBackfillDID_Pagination(t *testing.T) {
	st := newStubStore()

	// Build a PDS that serves 2 pages for feed subscriptions.
	page1 := []map[string]any{
		{"feedUrl": "https://a.com/rss", "createdAt": "2025-01-01T00:00:00Z"},
	}
	page2 := []map[string]any{
		{"feedUrl": "https://b.com/rss", "createdAt": "2025-02-01T00:00:00Z"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.atproto.repo.listRecords", func(w http.ResponseWriter, r *http.Request) {
		col := r.URL.Query().Get("collection")
		if col != "io.sunred.feed.subscription" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		cursor := r.URL.Query().Get("cursor")
		var recs []map[string]any
		var nextCursor string
		switch cursor {
		case "":
			for i, rec := range page1 {
				recs = append(recs, map[string]any{"uri": "at://x/io.sunred.feed.subscription/rkey" + jsonInt(i), "value": rec})
			}
			nextCursor = "page2"
		case "page2":
			for i, rec := range page2 {
				recs = append(recs, map[string]any{"uri": "at://x/io.sunred.feed.subscription/rkey" + jsonInt(i+10), "value": rec})
			}
			nextCursor = ""
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"records": recs, "cursor": nextCursor})
	})
	pds := httptest.NewServer(mux)
	t.Cleanup(pds.Close)

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  pds.URL,
	}

	n, err := f.backfillCollection(context.Background(), "did:plc:test", pds.URL, "io.sunred.feed.subscription")
	if err != nil {
		t.Fatalf("backfillCollection: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 records processed, got %d", n)
	}

	if len(st.feedSubs) != 2 {
		t.Errorf("expected 2 feed subs after pagination, got %d", len(st.feedSubs))
	}
}
