package fanout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/relay/internal/store"
)

// --- In-memory stub store ---

// stubStore is a minimal in-memory implementation of the store methods
// used by Fanout, so fanout unit tests don't need a real database.
type stubStore struct {
	follows      map[string]string // rkey → followee_did
	followCounts map[string]int    // followee_did → count
	shares       map[string]bool   // rkey → exists
	feedSubs     map[string]string // rkey → feed_url
	events       []store.RelayEvent
	didCursors   map[string]int64
	errorDIDs    map[string]string
}

func newStubStore() *stubStore {
	return &stubStore{
		follows:      make(map[string]string),
		followCounts: make(map[string]int),
		shares:       make(map[string]bool),
		feedSubs:     make(map[string]string),
		didCursors:   make(map[string]int64),
		errorDIDs:    make(map[string]string),
	}
}

func (s *stubStore) UpdateTrackedDIDCursor(_ context.Context, did string, seq int64) error {
	s.didCursors[did] = seq
	return nil
}

func (s *stubStore) SetTrackedDIDError(_ context.Context, did, msg string) error {
	s.errorDIDs[did] = msg
	return nil
}

func (s *stubStore) RecordFollow(_ context.Context, followerDID, followeeDID, rkey, _ string, _ time.Time) (bool, error) {
	key := followerDID + "/" + rkey
	if _, exists := s.follows[key]; exists {
		return false, nil
	}
	s.follows[key] = followeeDID
	s.followCounts[followeeDID]++
	return true, nil
}

func (s *stubStore) DeleteFollow(_ context.Context, followerDID, rkey string) (string, bool, error) {
	key := followerDID + "/" + rkey
	followeeDID, ok := s.follows[key]
	if !ok {
		return "", false, nil
	}
	delete(s.follows, key)
	s.followCounts[followeeDID]--
	return followeeDID, true, nil
}

func (s *stubStore) RecordShare(_ context.Context, did, rkey, _, _, _, _ string, _ *time.Time) (bool, error) {
	key := did + "/" + rkey
	if s.shares[key] {
		return false, nil
	}
	s.shares[key] = true
	return true, nil
}

func (s *stubStore) DeleteShare(_ context.Context, did, rkey string) (bool, error) {
	key := did + "/" + rkey
	if !s.shares[key] {
		return false, nil
	}
	delete(s.shares, key)
	return true, nil
}

func (s *stubStore) RecordFeedSubscription(_ context.Context, did, rkey, feedURL, _ string, _ *time.Time) (bool, error) {
	key := did + "/" + rkey
	if _, exists := s.feedSubs[key]; exists {
		return false, nil
	}
	s.feedSubs[key] = feedURL
	return true, nil
}

func (s *stubStore) DeleteFeedSubscription(_ context.Context, did, rkey string) (bool, error) {
	key := did + "/" + rkey
	if _, exists := s.feedSubs[key]; !exists {
		return false, nil
	}
	delete(s.feedSubs, key)
	return true, nil
}

func (s *stubStore) AppendEvent(_ context.Context, eventType, did string, payload any) (int64, error) {
	b, _ := json.Marshal(payload)
	seq := int64(len(s.events) + 1)
	s.events = append(s.events, store.RelayEvent{
		Seq:       seq,
		EventType: eventType,
		DID:       did,
		Payload:   b,
	})
	return seq, nil
}

func (s *stubStore) ListActiveTrackedDIDs(_ context.Context) ([]store.TrackedDID, error) {
	return nil, nil
}

// fanoutWithStub creates a Fanout that uses the stub store and a mock PDS.
// The mock PDS serves getRecord responses from the provided map:
//
//	key "collection/rkey" → JSON value object
func fanoutWithStub(t *testing.T, st *stubStore, recordMap map[string]map[string]any) *Fanout {
	t.Helper()

	// Build mock PDS server.
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.atproto.repo.getRecord", func(w http.ResponseWriter, r *http.Request) {
		collection := r.URL.Query().Get("collection")
		rkey := r.URL.Query().Get("rkey")
		key := collection + "/" + rkey
		rec, ok := recordMap[key]
		if !ok {
			http.Error(w, "record not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"uri": "at://did/col/rkey", "cid": "bafy", "value": rec}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f := &Fanout{
		store:       st,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
		testPDSURL:  srv.URL,
	}
	return f
}

// --- processOp routing ---

// We test processOp by checking which handle* method was invoked via the stub store.

func TestProcessOp_Follow_Create(t *testing.T) {
	st := newStubStore()

	records := map[string]map[string]any{
		"io.sunred.graph.follow/rkeyf001": {
			"subject":   "did:plc:bob",
			"createdAt": "2025-01-01T00:00:00Z",
		},
	}

	f := fanoutWithStub(t, st, records)
	f.processOp(context.Background(), "did:plc:alice", f.testPDSURL, repoOp{
		Action: "create",
		Path:   "io.sunred.graph.follow/rkeyf001",
	})

	if st.followCounts["did:plc:bob"] != 1 {
		t.Errorf("follow count for bob=%d, want 1", st.followCounts["did:plc:bob"])
	}
	if len(st.events) != 1 {
		t.Errorf("expected 1 event, got %d", len(st.events))
	}
	if st.events[0].EventType != "follow" {
		t.Errorf("event type=%q, want 'follow'", st.events[0].EventType)
	}
}

func TestProcessOp_Follow_Delete(t *testing.T) {
	st := newStubStore()
	// Pre-populate a follow.
	ctx := context.Background()
	_, _ = st.RecordFollow(ctx, "did:plc:alice", "did:plc:bob", "rkeyf002", "pds", time.Now())

	f := fanoutWithStub(t, st, nil)
	f.processOp(ctx, "did:plc:alice", f.testPDSURL, repoOp{
		Action: "delete",
		Path:   "io.sunred.graph.follow/rkeyf002",
	})

	if st.followCounts["did:plc:bob"] != 0 {
		t.Errorf("follow count for bob after unfollow=%d, want 0", st.followCounts["did:plc:bob"])
	}
	if len(st.events) != 1 || st.events[0].EventType != "unfollow" {
		t.Errorf("expected 1 unfollow event, got %+v", st.events)
	}
}

func TestProcessOp_Share_Create(t *testing.T) {
	st := newStubStore()

	records := map[string]map[string]any{
		"io.sunred.share.article/rkeys001": {
			"articleUrl": "https://example.com/article",
			"feedUrl":    "https://feed.example.com/rss",
			"title":      "Great Article",
			"sharedAt":   "2025-06-01T12:00:00Z",
		},
	}

	f := fanoutWithStub(t, st, records)
	f.processOp(context.Background(), "did:plc:alice", f.testPDSURL, repoOp{
		Action: "create",
		Path:   "io.sunred.share.article/rkeys001",
	})

	if !st.shares["did:plc:alice/rkeys001"] {
		t.Error("expected share to be recorded")
	}
	if len(st.events) != 1 || st.events[0].EventType != "share" {
		t.Errorf("expected 1 share event, got %+v", st.events)
	}
}

func TestProcessOp_Share_Delete(t *testing.T) {
	st := newStubStore()
	ctx := context.Background()
	_, _ = st.RecordShare(ctx, "did:plc:alice", "rkeys002", "", "", "", "", nil)

	f := fanoutWithStub(t, st, nil)
	f.processOp(ctx, "did:plc:alice", f.testPDSURL, repoOp{
		Action: "delete",
		Path:   "io.sunred.share.article/rkeys002",
	})

	if st.shares["did:plc:alice/rkeys002"] {
		t.Error("expected share to be removed")
	}
	if len(st.events) != 1 || st.events[0].EventType != "unshare" {
		t.Errorf("expected 1 unshare event, got %+v", st.events)
	}
}

func TestProcessOp_FeedSub_Create(t *testing.T) {
	st := newStubStore()
	records := map[string]map[string]any{
		"io.sunred.feed.subscription/rkeysub001": {
			"feedUrl":   "https://example.com/rss",
			"createdAt": "2025-01-01T00:00:00Z",
		},
	}

	f := fanoutWithStub(t, st, records)
	f.processOp(context.Background(), "did:plc:alice", f.testPDSURL, repoOp{
		Action: "create",
		Path:   "io.sunred.feed.subscription/rkeysub001",
	})

	if st.feedSubs["did:plc:alice/rkeysub001"] != "https://example.com/rss" {
		t.Errorf("feed sub not recorded: %v", st.feedSubs)
	}
	if len(st.events) != 1 || st.events[0].EventType != "feedSubscription" {
		t.Errorf("expected 1 feedSubscription event, got %+v", st.events)
	}
}

func TestProcessOp_FeedSub_Delete(t *testing.T) {
	st := newStubStore()
	ctx := context.Background()
	_, _ = st.RecordFeedSubscription(ctx, "did:plc:alice", "rkeysub002", "https://feed.example.com/rss", "", nil)

	f := fanoutWithStub(t, st, nil)
	f.processOp(ctx, "did:plc:alice", f.testPDSURL, repoOp{
		Action: "delete",
		Path:   "io.sunred.feed.subscription/rkeysub002",
	})

	if _, exists := st.feedSubs["did:plc:alice/rkeysub002"]; exists {
		t.Error("expected feed sub to be removed")
	}
	if len(st.events) != 1 || st.events[0].EventType != "feedUnsubscription" {
		t.Errorf("expected 1 feedUnsubscription event, got %+v", st.events)
	}
}

func TestProcessOp_UnknownCollection_Ignored(t *testing.T) {
	st := newStubStore()
	f := fanoutWithStub(t, st, nil)
	f.processOp(context.Background(), "did:plc:alice", f.testPDSURL, repoOp{
		Action: "create",
		Path:   "app.bsky.feed.post/rkey999",
	})
	// Unknown collection must produce no events.
	if len(st.events) != 0 {
		t.Errorf("expected 0 events for unknown collection, got %d", len(st.events))
	}
}

func TestProcessOp_MalformedPath_Ignored(t *testing.T) {
	st := newStubStore()
	f := fanoutWithStub(t, st, nil)
	// Path with no slash should be silently ignored.
	f.processOp(context.Background(), "did:plc:alice", f.testPDSURL, repoOp{
		Action: "create",
		Path:   "noslash",
	})
	if len(st.events) != 0 {
		t.Errorf("expected 0 events for malformed path, got %d", len(st.events))
	}
}

func TestHandleFollow_MissingSubject_Ignored(t *testing.T) {
	st := newStubStore()
	// PDS returns record with empty subject.
	records := map[string]map[string]any{
		"io.sunred.graph.follow/badfollow": {
			"subject": "", // empty — should be dropped
		},
	}
	f := fanoutWithStub(t, st, records)
	f.handleFollow(context.Background(), "did:plc:alice", f.testPDSURL, "badfollow", "create")
	if len(st.events) != 0 {
		t.Errorf("expected 0 events for empty subject, got %d", len(st.events))
	}
	if len(st.follows) != 0 {
		t.Errorf("expected 0 follows recorded")
	}
}

func TestHandleFollow_PDSError_Ignored(t *testing.T) {
	st := newStubStore()
	// No record in mock PDS — getRecord returns 404.
	f := fanoutWithStub(t, st, map[string]map[string]any{})
	f.handleFollow(context.Background(), "did:plc:alice", f.testPDSURL, "notfound", "create")
	// Should not panic and should not record anything.
	if len(st.events) != 0 {
		t.Errorf("expected 0 events on PDS error, got %d", len(st.events))
	}
}

func TestSubscribe_And_Unsubscribe(t *testing.T) {
	f := &Fanout{
		subscribers: make(map[string]chan *store.RelayEvent),
		workers:     make(map[string]context.CancelFunc),
	}

	ch := f.Subscribe("https://instance.example.com")
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Emit an event — should be received on ch.
	f.store = newStubStore()
	go func() {
		f.emit(context.Background(), "follow", "did:plc:test", map[string]string{"test": "x"})
	}()

	select {
	case evt := <-ch:
		if evt.EventType != "follow" {
			t.Errorf("event type=%q, want 'follow'", evt.EventType)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}

	f.Unsubscribe("https://instance.example.com")
	if len(f.subscribers) != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", len(f.subscribers))
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   any
		want time.Time
	}{
		{"2025-03-15T10:30:00Z", time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)},
		{"", time.Time{}}, // empty → should not panic, return ~now
		{42, time.Time{}}, // non-string → should not panic
		{"not-a-date", time.Time{}},
	}

	for _, tc := range cases {
		got := parseTime(tc.in)
		if !tc.want.IsZero() && !got.Equal(tc.want) {
			t.Errorf("parseTime(%v) = %v, want %v", tc.in, got, tc.want)
		}
		// For zero-want cases, just verify it doesn't panic and returns a time.
		if got.IsZero() {
			t.Errorf("parseTime(%v) returned zero time", tc.in)
		}
	}
}
