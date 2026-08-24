package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// --- ATProtoSyncStar ---

func TestATProtoSyncStar_Star(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:starrer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entry_stars WHERE user_id = $1`, userID)
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM entries WHERE feed_id IN (SELECT id FROM feeds WHERE feed_url LIKE $1)", "https://feed.example.com/%")
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM feeds WHERE feed_url LIKE $1", "https://feed.example.com/%")
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:starrer", pdsURL)

	pub := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	articleURL := "https://example.com/star-article"
	entry := &store.Entry{
		URL:         articleURL,
		Title:       "Star Article",
		Description: "A starred article",
		Author:      "Author Name",
		PublishedAt: pub,
		Feed: &store.Feed{
			FeedURL: "https://feed.example.com/rss",
			Title:   "Feed Title",
			SiteURL: "https://feed.example.com",
		},
	}

	// First create the star row (as the API handler does via ToggleEntryStarredByURL).
	if err := s.ToggleEntryStarredByURL(context.Background(), userID,
		articleURL, entry.Title, entry.Description,
		entry.Feed.FeedURL, entry.Feed.Title, entry.Feed.SiteURL,
		entry.Author, &pub, true,
	); err != nil {
		t.Fatalf("create star: %v", err)
	}

	api := &API{store: s}
	api.ATProtoSyncStar(userID, entry, true, "")

	// Verify the PDS received a putRecord for io.sunred.entry.star.
	found := false
	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionStar {
			found = true
			var rec atproto.StarRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if rec.ArticleURL != "https://example.com/star-article" {
				t.Errorf("record articleUrl=%q, want 'https://example.com/star-article'", rec.ArticleURL)
			}
			if rec.Title != "Star Article" {
				t.Errorf("record title=%q, want 'Star Article'", rec.Title)
			}
			if rec.Description != "A starred article" {
				t.Errorf("record description=%q, want 'A starred article'", rec.Description)
			}
			if rec.Author != "Author Name" {
				t.Errorf("record author=%q, want 'Author Name'", rec.Author)
			}
			if rec.FeedURL != "https://feed.example.com/rss" {
				t.Errorf("record feedUrl=%q, want 'https://feed.example.com/rss'", rec.FeedURL)
			}
			if rec.FeedTitle != "Feed Title" {
				t.Errorf("record feedTitle=%q, want 'Feed Title'", rec.FeedTitle)
			}
			if rec.FeedSiteURL != "https://feed.example.com" {
				t.Errorf("record feedSiteUrl=%q, want 'https://feed.example.com'", rec.FeedSiteURL)
			}
			if rec.PublishedAt == "" {
				t.Error("expected non-empty publishedAt")
			}
		}
	}
	if !found {
		t.Error("expected a putRecord call for io.sunred.entry.star")
	}

	// Verify the rkey was stored locally.
	rkey, _ := s.GetStarATProtoRkey(context.Background(), userID, articleURL)
	if rkey == "" {
		t.Error("expected non-empty atproto_rkey after star sync")
	}
}

func TestATProtoSyncStar_Unstar(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:unstarrer")
	articleURL := "https://example.com/unstar-article"
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entry_stars WHERE user_id = $1`, userID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entries WHERE url = $1`, articleURL)
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM feeds WHERE feed_url LIKE $1", "https://feed.example.com/%")
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:unstarrer", pdsURL)

	// Seed a star with a known rkey.
	if err := s.UpsertStarWithRkey(context.Background(), userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, "rkey-unstar-test",
	); err != nil {
		t.Fatalf("seed star: %v", err)
	}

	entry := &store.Entry{URL: articleURL, Title: "Title"}
	api := &API{store: s}
	api.ATProtoSyncStar(userID, entry, false, "rkey-unstar-test")

	// Verify the PDS received a deleteRecord for io.sunred.entry.star.
	found := false
	for _, call := range *calls {
		if call.Op == "deleteRecord" && call.Collection == atproto.CollectionStar {
			found = true
			if call.Rkey != "rkey-unstar-test" {
				t.Errorf("rkey=%q, want 'rkey-unstar-test'", call.Rkey)
			}
		}
	}
	if !found {
		t.Error("expected a deleteRecord call for io.sunred.entry.star")
	}
}

func TestATProtoSyncStar_NilPublishedAt(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:starnilpub")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entry_stars WHERE user_id = $1`, userID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entries WHERE url = $1`, "https://example.com/nil-pub")
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM feeds WHERE feed_url LIKE $1", "https://feed.example.com/%")
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:starnilpub", pdsURL)

	entry := &store.Entry{
		URL:         "https://example.com/nil-pub",
		Title:       "Title",
		Description: "",
		Feed: &store.Feed{
			FeedURL: "https://feed.example.com/rss",
			Title:   "Feed",
			SiteURL: "https://feed.example.com",
		},
	}

	api := &API{store: s}
	api.ATProtoSyncStar(userID, entry, true, "")

	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionStar {
			var rec atproto.StarRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if rec.PublishedAt != "" {
				t.Errorf("expected empty publishedAt for zero-time PublishedAt, got %q", rec.PublishedAt)
			}
		}
	}
}

// --- Relay consumer star event ---

func TestRelayConsumer_StarEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:starconsumer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entry_stars WHERE user_id = $1`, userID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entries WHERE url = $1`, "https://example.com/relay-star")
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM feeds WHERE feed_url = $1", "https://feed.example.com/relay.xml")
	}()

	evt := relayEvent{
		Seq:       1,
		EventType: "star",
		DID:       "did:plc:starconsumer",
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"rkey":        "rkey-star-evt",
		"articleUrl":  "https://example.com/relay-star",
		"title":       "Relay Star",
		"description": "Starred via relay",
		"feedUrl":     "https://feed.example.com/relay.xml",
		"feedTitle":   "Relay Feed",
		"feedSiteUrl": "https://feed.example.com",
		"author":      "Relay Author",
		"publishedAt": "2025-01-01T00:00:00Z",
		"createdAt":   "2025-01-01T00:00:00Z",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rkey, _ := s.GetStarATProtoRkey(context.Background(), userID, "https://example.com/relay-star")
		if rkey == "rkey-star-evt" {
			// Verify the star metadata.
			var title, feedURL string
			if err := s.DB.QueryRow(
				`SELECT title, feed_url FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
				userID, "https://example.com/relay-star",
			).Scan(&title, &feedURL); err != nil {
				t.Fatalf("read star: %v", err)
			}
			if title != "Relay Star" {
				t.Errorf("title=%q, want 'Relay Star'", title)
			}
			if feedURL != "https://feed.example.com/relay.xml" {
				t.Errorf("feed_url=%q, want 'https://feed.example.com/relay.xml'", feedURL)
			}
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for star event to be processed")
}

func TestRelayConsumer_UnstarEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:unstarconsumer")
	articleURL := "https://example.com/relay-unstar"
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entry_stars WHERE user_id = $1`, userID)
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM entries WHERE url = $1`, articleURL)
		_, _ = s.DB.ExecContext(context.Background(), "DELETE FROM feeds WHERE feed_url LIKE $1", "https://feed.example.com/%")
	}()

	// Seed a star with a known rkey.
	if err := s.UpsertStarWithRkey(context.Background(), userID,
		articleURL, "Title", "", "https://feed.example.com/rss", "Feed", "", "", nil, "rkey-unstar-evt",
	); err != nil {
		t.Fatalf("seed star: %v", err)
	}

	evt := relayEvent{
		Seq:       1,
		EventType: "unstar",
		DID:       "did:plc:unstarconsumer",
	}
	evt.Payload, _ = json.Marshal(map[string]any{"rkey": "rkey-unstar-evt"})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := s.DB.QueryRow(
			`SELECT COUNT(*) FROM entry_stars WHERE user_id = $1 AND atproto_rkey = $2`,
			userID, "rkey-unstar-evt",
		).Scan(&n); err != nil {
			t.Fatalf("count stars: %v", err)
		}
		if n == 0 {
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for unstar event to delete the star")
}
