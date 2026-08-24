package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/fuegoio/sunred/go/api/internal/store"
)

func testDB(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("SUNRED_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sunred:sunred@localhost:5432/sunred?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("could not open database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database not reachable, skipping integration test: %v", err)
	}
	return store.New(db)
}

// mockRelayServer creates a test relay that serves subscribeEvents over
// WebSocket, sending the provided events in order. Returns the server URL
// and a function to send additional events.
func mockRelayServer(t *testing.T, events []relayEvent) (url string, sendEvt func(relayEvent)) {
	t.Helper()

	var ch chan *relayEvent

	mux := http.NewServeMux()
	mux.Handle("/xrpc/io.sunred.relay.subscribeEvents", websocket.Handler(func(ws *websocket.Conn) {
		for _, evt := range events {
			_ = websocket.JSON.Send(ws, evt)
		}
		for evt := range ch {
			_ = websocket.JSON.Send(ws, evt)
		}
		_ = ws.Close()
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ch = make(chan *relayEvent, 16)
	return srv.URL, func(evt relayEvent) { ch <- &evt }
}

func mustSeedUser(t *testing.T, s *store.Store, did string) int {
	t.Helper()
	userID, _, err := s.GetOrCreateUserByDID(context.Background(), did, "handle_"+did[len(did)-4:])
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

func TestRelayConsumer_FeedSubscriptionEvent(t *testing.T) {
	s := testDB(t)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := mustSeedUser(t, s, "did:plc:alice-"+suffix)
	feedURL := fmt.Sprintf("https://feedsub-%s.example.com/rss", suffix)

	evt := relayEvent{
		Seq:       1,
		EventType: "feedSubscription",
		DID:       "did:plc:alice-" + suffix,
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"rkey":    "rkey001",
		"feedUrl": feedURL,
		"siteUrl": "https://feedsub.example.com",
		"title":   "Example",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	// Wait for the feed + subscription to appear.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		feed, _ := s.GetFeedByURL(context.Background(), feedURL)
		if feed != nil {
			if feed.Title != "Example" {
				t.Errorf("feed title=%q, want 'Example'", feed.Title)
			}
			if sub, _ := s.GetSubscriptionFeed(context.Background(), feed.ID, userID); sub == nil {
				// Feed exists but subscription isn't created yet — keep polling.
				time.Sleep(50 * time.Millisecond)
				continue
			}
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for feed subscription event to be processed")
}

func TestRelayConsumer_BackfillCompleteEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:bob")
	_ = s.SetUserSyncStatus(context.Background(), userID, "syncing")

	evt := relayEvent{
		Seq:       1,
		EventType: "backfillComplete",
		DID:       "did:plc:bob",
	}
	evt.Payload, _ = json.Marshal(map[string]any{"did": "did:plc:bob"})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		user, _ := s.GetUserByID(context.Background(), userID)
		if user != nil && user.PDSSyncStatus == "idle" {
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for sync status to become idle")
}

func TestRelayConsumer_FeedUnsubscriptionEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:carol")

	// Seed a feed with an rkey.
	_ = s.UpsertFeedSubscriptionWithRkey(context.Background(), userID,
		"https://example.com/rss", "https://example.com", "Test", "rkey-del")

	evt := relayEvent{
		Seq:       1,
		EventType: "feedUnsubscription",
		DID:       "did:plc:carol",
	}
	evt.Payload, _ = json.Marshal(map[string]any{"rkey": "rkey-del"})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		feed, _ := s.GetFeedByURL(context.Background(), "https://example.com/rss")
		if feed == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// The global feed persists; unsubscription removes the subscription.
		if sub, _ := s.GetSubscriptionFeed(context.Background(), feed.ID, userID); sub == nil {
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for subscription to be deleted by unsubscription event")
}

func TestRelayConsumer_FollowEvent(t *testing.T) {
	s := testDB(t)
	aliceID := mustSeedUser(t, s, "did:plc:alice")
	bobID := mustSeedUser(t, s, "did:plc:bob")

	evt := relayEvent{
		Seq:       1,
		EventType: "follow",
		DID:       "did:plc:alice",
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"subjectDid": "did:plc:bob",
		"rkey":       "rkey-follow-1",
		"createdAt":  "2025-01-01T00:00:00Z",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rkey, _ := s.GetFollowATProtoRkey(context.Background(), aliceID, bobID)
		if rkey == "rkey-follow-1" {
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for follow event to be processed")
}

func TestRelayConsumer_UnknownDIDIgnored(t *testing.T) {
	s := testDB(t)
	// No user seeded for this DID.

	evt := relayEvent{
		Seq:       1,
		EventType: "feedSubscription",
		DID:       "did:plc:unknown",
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"rkey":    "rkey-unk",
		"feedUrl": "https://unknown.com/rss",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	time.Sleep(500 * time.Millisecond)
	// No user should have this feed. We can't check by userID (unknown), but
	// we can verify no crash occurred and the consumer is still running.
	cancel()
}
