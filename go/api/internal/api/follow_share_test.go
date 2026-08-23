package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// --- Follow AT Proto sync tests ---

func TestATProtoSyncFollow_Follow(t *testing.T) {
	s := testDB(t)
	followerID := mustSeedUser(t, s, "did:plc:follower")
	followeeID := mustSeedUser(t, s, "did:plc:followee")
	defer cleanupFollows(t, s, followerID, followeeID)

	// Seed followee profile with DID + PDS credentials for follower.
	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, followerID, "did:plc:follower", pdsURL)
	seedProfile(t, s, followeeID, "did:plc:followee", "followee")

	api := &API{store: s}
	api.ATProtoSyncFollow(followerID, followeeID, "followee", true)

	// Verify the PDS received a putRecord for io.sunred.graph.follow.
	found := false
	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionFollow {
			found = true
			var rec atproto.FollowRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if rec.Subject != "did:plc:followee" {
				t.Errorf("record subject=%q, want 'did:plc:followee'", rec.Subject)
			}
		}
	}
	if !found {
		t.Error("expected a putRecord call for io.sunred.graph.follow")
	}

	// Verify the rkey was stored locally.
	rkey, _ := s.GetFollowATProtoRkey(context.Background(), followerID, followeeID)
	if rkey == "" {
		t.Error("expected non-empty atproto_rkey after follow sync")
	}
}

func TestATProtoSyncFollow_Unfollow(t *testing.T) {
	s := testDB(t)
	followerID := mustSeedUser(t, s, "did:plc:unfollower")
	followeeID := mustSeedUser(t, s, "did:plc:unfollowee")
	defer cleanupFollows(t, s, followerID, followeeID)

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, followerID, "did:plc:unfollower", pdsURL)
	seedProfile(t, s, followeeID, "did:plc:unfollowee", "unfollowee")

	// Seed a follow with a known rkey.
	_ = s.UpsertFollowWithRkey(context.Background(), followerID, followeeID, "rkey-unfollow-test")

	api := &API{store: s}
	api.ATProtoSyncFollow(followerID, followeeID, "unfollowee", false)

	// Verify the PDS received a deleteRecord for io.sunred.graph.follow.
	found := false
	for _, call := range *calls {
		if call.Op == "deleteRecord" && call.Collection == atproto.CollectionFollow {
			found = true
			if call.Rkey != "rkey-unfollow-test" {
				t.Errorf("deleteRecord rkey=%q, want 'rkey-unfollow-test'", call.Rkey)
			}
		}
	}
	if !found {
		t.Error("expected a deleteRecord call for io.sunred.graph.follow")
	}

	// Verify the rkey was cleared locally.
	rkey, _ := s.GetFollowATProtoRkey(context.Background(), followerID, followeeID)
	if rkey != "" {
		t.Errorf("expected empty atproto_rkey after unfollow, got %q", rkey)
	}
}

// --- Profile AT Proto sync tests ---

func TestATProtoSyncProfile(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:profile")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:profile", pdsURL)

	// Seed a display name + bio so we can assert they round-trip into the record.
	_, _ = s.DB.ExecContext(context.Background(),
		`UPDATE users SET display_name = $2, bio = $3 WHERE id = $1`,
		userID, "Ada Lovelace", "reading feeds")
	createdAt := time.Now().UTC().Truncate(time.Second)

	api := &API{store: s}
	api.ATProtoSyncProfile(userID, "Ada Lovelace", "reading feeds", createdAt)

	found := false
	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionProfile {
			found = true
			if call.Rkey != atproto.ProfileRkey {
				t.Errorf("profile rkey=%q, want %q", call.Rkey, atproto.ProfileRkey)
			}
			var rec atproto.ProfileRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if rec.Type != atproto.CollectionProfile {
				t.Errorf("record $type=%q, want %q", rec.Type, atproto.CollectionProfile)
			}
			if rec.DisplayName != "Ada Lovelace" {
				t.Errorf("displayName=%q, want 'Ada Lovelace'", rec.DisplayName)
			}
			if rec.Description != "reading feeds" {
				t.Errorf("description=%q, want 'reading feeds'", rec.Description)
			}
		}
	}
	if !found {
		t.Error("expected a putRecord call for app.bsky.actor.profile")
	}
}

// --- Share AT Proto sync tests ---

func TestATProtoSyncShare_Share(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:sharer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM shared_articles WHERE user_id = $1`, userID)
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:sharer", pdsURL)

	// Create a share locally.
	sa, err := s.ShareArticle(context.Background(), userID,
		"https://example.com/article", "Great Article", "A description",
		"https://feed.example.com/rss", "Feed Title", "https://feed.example.com",
		"Author Name", nil,
	)
	if err != nil {
		t.Fatalf("share article: %v", err)
	}

	api := &API{store: s}
	api.ATProtoSyncShare(userID, sa, true)

	// Verify the PDS received a putRecord for io.sunred.share.article.
	found := false
	for _, call := range *calls {
		if call.Op == "putRecord" && call.Collection == atproto.CollectionShare {
			found = true
			var rec atproto.ShareRecord
			if err := json.Unmarshal(call.Record, &rec); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if rec.ArticleURL != "https://example.com/article" {
				t.Errorf("record articleUrl=%q, want 'https://example.com/article'", rec.ArticleURL)
			}
			if rec.Title != "Great Article" {
				t.Errorf("record title=%q, want 'Great Article'", rec.Title)
			}
			if rec.Description != "A description" {
				t.Errorf("record description=%q, want 'A description'", rec.Description)
			}
			if rec.Author != "Author Name" {
				t.Errorf("record author=%q, want 'Author Name'", rec.Author)
			}
		}
	}
	if !found {
		t.Error("expected a putRecord call for io.sunred.share.article")
	}

	// Verify the rkey was stored locally.
	rkey, _ := s.GetShareATProtoRkey(context.Background(), sa.ID)
	if rkey == "" {
		t.Error("expected non-empty atproto_rkey after share sync")
	}
}

func TestATProtoSyncShare_Unshare(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:unsharer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM shared_articles WHERE user_id = $1`, userID)
	}()

	pdsURL, calls := mockPDSPutRecord(t)
	seedCredentials(t, s, userID, "did:plc:unsharer", pdsURL)

	// Seed a share with a known rkey.
	sa, err := s.ShareArticle(context.Background(), userID,
		"https://example.com/unshare-article", "Title", "",
		"", "", "", "", nil,
	)
	if err != nil {
		t.Fatalf("share article: %v", err)
	}
	_ = s.SetShareATProtoRkey(context.Background(), sa.ID, "rkey-unshare-test")

	api := &API{store: s}
	api.ATProtoSyncShare(userID, sa, false)

	// Verify the PDS received a deleteRecord for io.sunred.share.article.
	found := false
	for _, call := range *calls {
		if call.Op == "deleteRecord" && call.Collection == atproto.CollectionShare {
			found = true
			if call.Rkey != "rkey-unshare-test" {
				t.Errorf("deleteRecord rkey=%q, want 'rkey-unshare-test'", call.Rkey)
			}
		}
	}
	if !found {
		t.Error("expected a deleteRecord call for io.sunred.share.article")
	}

	// Verify the rkey was cleared locally.
	rkey, _ := s.GetShareATProtoRkey(context.Background(), sa.ID)
	if rkey != "" {
		t.Errorf("expected empty atproto_rkey after unshare, got %q", rkey)
	}
}

// --- Relay consumer share/unshare tests ---

func TestRelayConsumer_ShareEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:shareconsumer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM shared_articles WHERE user_id = $1`, userID)
	}()

	evt := relayEvent{
		Seq:       1,
		EventType: "share",
		DID:       "did:plc:shareconsumer",
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"rkey":        "rkey-share-1",
		"articleUrl":  "https://example.com/shared",
		"title":       "Shared Article",
		"description": "Shared description",
		"feedUrl":     "https://feed.example.com/rss",
		"feedTitle":   "Feed",
		"feedSiteUrl": "https://feed.example.com",
		"author":      "Author",
		"sharedAt":    "2025-01-01T00:00:00Z",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sa, _ := s.GetSharedArticleByURL(context.Background(), userID, "https://example.com/shared")
		if sa != nil {
			if sa.Title != "Shared Article" {
				t.Errorf("share title=%q, want 'Shared Article'", sa.Title)
			}
			if sa.Description != "Shared description" {
				t.Errorf("share description=%q, want 'Shared description'", sa.Description)
			}
			if sa.Author != "Author" {
				t.Errorf("share author=%q, want 'Author'", sa.Author)
			}
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for share event to be processed")
}

func TestRelayConsumer_UnshareEvent(t *testing.T) {
	s := testDB(t)
	userID := mustSeedUser(t, s, "did:plc:unshareconsumer")
	defer func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM shared_articles WHERE user_id = $1`, userID)
	}()

	// Seed a share with a known rkey.
	_ = s.UpsertShareWithRkey(context.Background(), userID,
		"https://example.com/unshare-event", "Title", "", "", "", "", "", nil, "rkey-unshare-evt")

	evt := relayEvent{
		Seq:       1,
		EventType: "unshare",
		DID:       "did:plc:unshareconsumer",
	}
	evt.Payload, _ = json.Marshal(map[string]any{"rkey": "rkey-unshare-evt"})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sa, _ := s.GetSharedArticleByURL(context.Background(), userID, "https://example.com/unshare-event")
		if sa == nil {
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for share to be deleted by unshare event")
}

func TestRelayConsumer_UnfollowEvent(t *testing.T) {
	s := testDB(t)
	followerID := mustSeedUser(t, s, "did:plc:unfolcons")
	followeeID := mustSeedUser(t, s, "did:plc:unfolconsee")
	defer cleanupFollows(t, s, followerID, followeeID)

	// Seed a follow with a known rkey.
	_ = s.UpsertFollowWithRkey(context.Background(), followerID, followeeID, "rkey-unfollow-evt")

	evt := relayEvent{
		Seq:       1,
		EventType: "unfollow",
		DID:       "did:plc:unfolcons",
	}
	evt.Payload, _ = json.Marshal(map[string]any{
		"subjectDid": "did:plc:unfolconsee",
		"rkey":       "rkey-unfollow-evt",
	})

	relayURL, _ := mockRelayServer(t, []relayEvent{evt})
	consumer := NewRelayConsumer(s, relayURL, "http://test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go consumer.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rkey, _ := s.GetFollowATProtoRkey(context.Background(), followerID, followeeID)
		if rkey == "" {
			// Follow was deleted by rkey
			cancel()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for unfollow event to delete the follow")
}

// --- Helpers ---

func seedCredentials(t *testing.T, s *store.Store, userID int, did, pdsURL string) {
	t.Helper()
	_, err := s.DB.ExecContext(context.Background(), `
		UPDATE users SET did = $2, pds_url = $3, atproto_access_token = 'test-token', atproto_refresh_token = 'test-refresh'
		WHERE id = $1`,
		userID, did, pdsURL)
	if err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	t.Cleanup(func() {
	})
}

func seedProfile(t *testing.T, s *store.Store, userID int, did, handle string) {
	t.Helper()
	_, err := s.DB.ExecContext(context.Background(), `
		UPDATE users SET did = $2, handle = $3
		WHERE id = $1`,
		userID, did, handle)
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
}

func cleanupFollows(t *testing.T, s *store.Store, followerID, followeeID int) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = s.DB.ExecContext(context.Background(), `DELETE FROM user_follows WHERE follower_id = $1 AND followee_id = $2`, followerID, followeeID)
	})
}
