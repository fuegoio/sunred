package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
)

// mockPDSPutRecord creates a mock PDS that captures putRecord / deleteRecord
// calls and returns success. Returns the server URL and a slice of captured
// requests.
func mockPDSPutRecord(t *testing.T) (url string, calls *[]pdsCall) {
	t.Helper()
	captured := &[]pdsCall{}

	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.atproto.repo.putRecord", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Repo       string          `json:"repo"`
			Collection string          `json:"collection"`
			Rkey       string          `json:"rkey"`
			Record     json.RawMessage `json:"record"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*captured = append(*captured, pdsCall{
			Op:         "putRecord",
			Collection: body.Collection,
			Rkey:       body.Rkey,
			Record:     body.Record,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uri": "at://" + body.Repo + "/" + body.Collection + "/" + body.Rkey,
			"cid": "bafy-test",
		})
	})
	mux.HandleFunc("/xrpc/com.atproto.repo.deleteRecord", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Repo       string `json:"repo"`
			Collection string `json:"collection"`
			Rkey       string `json:"rkey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*captured = append(*captured, pdsCall{
			Op:         "deleteRecord",
			Collection: body.Collection,
			Rkey:       body.Rkey,
		})
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, captured
}

type pdsCall struct {
	Op         string
	Collection string
	Rkey       string
	Record     json.RawMessage
}

// TestATProtoSyncFeedSubscription_Subscribe verifies that when a user subscribes
// to a feed, the subscription is written to their PDS as an
// io.sunred.feed.subscription record.
func TestATProtoSyncFeedSubscription_Subscribe(t *testing.T) {
	s := testDB(t)
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM users WHERE did = $1`, "did:plc:feedtest")
	}()

	userID, _, _ := s.GetOrCreateUserByDID(context.Background(), "did:plc:feedtest", "feedtest")

	// Seed ATProto credentials for the user.
	pdsURL, calls := mockPDSPutRecord(t)
	_, _ = s.DB.ExecContext(context.Background(), `
		UPDATE users SET did = $2, pds_url = $3, atproto_access_token = 'test-token', atproto_refresh_token = 'test-refresh'
		WHERE id = $1`,
		userID, "did:plc:feedtest", pdsURL)
	defer func() {
	}()

	// Create a global feed locally and subscribe the user to it.
	feed, err := s.GetOrCreateFeed(context.Background(),
		"https://feed.example.com/rss", "https://example.com", "Test Feed", "")
	if err != nil {
		t.Fatalf("create feed: %v", err)
	}
	if _, err := s.CreateSubscription(context.Background(), userID, feed.ID, nil, ""); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM subscriptions WHERE feed_id = $1`, feed.ID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM feeds WHERE id = $1`, feed.ID)
	}()

	// Simulate the subscribe sync.
	api := &API{store: s}
	api.ATProtoSyncFeedSubscription(userID, feed.ID, feed.FeedURL, feed.SiteURL, feed.Title, true, feed.CreatedAt, "")

	// Verify the PDS received a putRecord for io.sunred.feed.subscription.
	found := false
	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionSubscription {
			found = true
			var rec atproto.SubscriptionRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if rec.FeedURL != "https://feed.example.com/rss" {
				t.Errorf("record feedUrl=%q, want 'https://feed.example.com/rss'", rec.FeedURL)
			}
			if rec.SiteURL != "https://example.com" {
				t.Errorf("record siteUrl=%q, want 'https://example.com'", rec.SiteURL)
			}
			if rec.Title != "Test Feed" {
				t.Errorf("record title=%q, want 'Test Feed'", rec.Title)
			}
		}
	}
	if !found {
		t.Error("expected a putRecord call for io.sunred.feed.subscription")
	}

	// Verify the rkey was stored locally on the subscription.
	rkey, _ := s.GetFeedATProtoRkey(context.Background(), userID, feed.ID)
	if rkey == "" {
		t.Error("expected non-empty atproto_rkey after subscribe sync")
	}
}

// TestATProtoSyncFeedSubscription_Unsubscribe verifies that unsubscribing
// deletes the io.sunred.feed.subscription record from the PDS using the
// stored rkey.
func TestATProtoSyncFeedSubscription_Unsubscribe(t *testing.T) {
	s := testDB(t)
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM users WHERE did = $1`, "did:plc:unsubtest")
	}()

	userID, _, _ := s.GetOrCreateUserByDID(context.Background(), "did:plc:unsubtest", "unsubtest")

	pdsURL, calls := mockPDSPutRecord(t)
	_, _ = s.DB.ExecContext(context.Background(), `
		UPDATE users SET did = $2, pds_url = $3, atproto_access_token = 'test-token', atproto_refresh_token = 'test-refresh'
		WHERE id = $1`,
		userID, "did:plc:unsubtest", pdsURL)
	defer func() {
	}()

	// Seed a feed subscription with a known rkey.
	_ = s.UpsertFeedSubscriptionWithRkey(context.Background(), userID,
		"https://feed.example.com/rss", "https://example.com", "Test", "rkey-unsub-1")
	feed, _ := s.GetFeedByURL(context.Background(), "https://feed.example.com/rss")
	if feed == nil {
		t.Fatal("expected feed to exist")
	}
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM subscriptions WHERE feed_id = $1`, feed.ID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM feeds WHERE id = $1`, feed.ID)
	}()

	// Simulate the unsubscribe sync (isSubscribe=false).
	api := &API{store: s}
	api.ATProtoSyncFeedSubscription(userID, feed.ID, feed.FeedURL, feed.SiteURL, feed.Title, false, time.Now(), "")

	// Verify the PDS received a deleteRecord for io.sunred.feed.subscription.
	found := false
	for _, call := range *calls {
		if call.Op == "deleteRecord" && call.Collection == atproto.CollectionSubscription {
			found = true
			if call.Rkey != "rkey-unsub-1" {
				t.Errorf("deleteRecord rkey=%q, want 'rkey-unsub-1'", call.Rkey)
			}
		}
	}
	if !found {
		t.Error("expected a deleteRecord call for io.sunred.feed.subscription")
	}
}

// TestFeedSubscriptionRoundTrip verifies the full local → PDS → relay → local
// round-trip: a subscription is written to the PDS, the relay backfills it
// and emits an event, and the API consumer processes it into the local cache.
// This is tested at the store level (no network).
func TestFeedSubscriptionRoundTrip_StoreLevel(t *testing.T) {
	s := testDB(t)
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM users WHERE did = $1`, "did:plc:roundtrip")
	}()

	userID, _, _ := s.GetOrCreateUserByDID(context.Background(), "did:plc:roundtrip", "roundtrip")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM subscriptions WHERE user_id = $1`, userID)
	}()

	// Step 1: Subscribe (write to PDS + store rkey locally).
	rkey := "rkey-rt-001"
	err := s.UpsertFeedSubscriptionWithRkey(context.Background(), userID,
		"https://rt.example.com/rss", "https://rt.example.com", "Round Trip", rkey)
	if err != nil {
		t.Fatalf("upsert feed sub: %v", err)
	}

	// Step 2: Simulate relay backfill reading it back.
	// (In production, the relay calls listRecords and emits a feedSubscription
	// event. The API consumer then calls UpsertFeedSubscriptionWithRkey.)
	// Verify the feed exists locally.
	feed, err := s.GetFeedByURL(context.Background(), "https://rt.example.com/rss")
	if err != nil {
		t.Fatalf("get feed by url: %v", err)
	}
	if feed == nil {
		t.Fatal("expected feed to exist after upsert")
	}
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM feeds WHERE id = $1`, feed.ID)
	}()
	// The user must be subscribed to it.
	sub, err := s.GetSubscriptionFeed(context.Background(), feed.ID, userID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if sub == nil {
		t.Fatal("expected subscription to exist after upsert")
	}
	if sub.Title != "Round Trip" {
		t.Errorf("feed title=%q, want 'Round Trip'", sub.Title)
	}

	// Step 3: Simulate unsubscribe (relay emits feedUnsubscription).
	// The API consumer calls DeleteFeedByRkey.
	storedRkey, _ := s.GetFeedATProtoRkey(context.Background(), userID, feed.ID)
	if storedRkey != rkey {
		t.Fatalf("stored rkey=%q, want %q", storedRkey, rkey)
	}
	err = s.DeleteFeedByRkey(context.Background(), userID, rkey)
	if err != nil {
		t.Fatalf("delete feed by rkey: %v", err)
	}

	// Step 4: Verify the subscription is gone (the global feed persists).
	sub, _ = s.GetSubscriptionFeed(context.Background(), feed.ID, userID)
	if sub != nil {
		t.Error("expected subscription to be deleted after unsubscription")
	}
}
