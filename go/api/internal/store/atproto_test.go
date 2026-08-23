package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// --- AT Proto credential storage ---

func TestConnectATProto_And_GetCredentials(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	userID := seedUser(t, s, fmt.Sprintf("atproto-creds-%d@example.com", time.Now().UnixNano()))
	// Must have a profile row before we can set AT Proto columns.
	handle := fmt.Sprintf("atcreds%d", time.Now().UnixNano()%99999)
	_, err := s.UpsertHandle(ctx, userID, handle, "")
	if err != nil {
		t.Fatalf("UpsertHandle: %v", err)
	}

	expires := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	if err := s.ConnectATProto(ctx, userID,
		"did:plc:testuser", "https://pds.example.com",
		"access-token-abc", "refresh-token-xyz", &expires,
	); err != nil {
		t.Fatalf("ConnectATProto: %v", err)
	}

	creds, err := s.GetATProtoCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetATProtoCredentials: %v", err)
	}
	if creds == nil {
		t.Fatal("expected non-nil credentials, got nil")
	}
	if creds.DID != "did:plc:testuser" {
		t.Errorf("DID=%q, want 'did:plc:testuser'", creds.DID)
	}
	if creds.PDSUrl != "https://pds.example.com" {
		t.Errorf("PDSUrl=%q", creds.PDSUrl)
	}
	if creds.AccessToken != "access-token-abc" {
		t.Errorf("AccessToken=%q", creds.AccessToken)
	}
	if creds.RefreshToken != "refresh-token-xyz" {
		t.Errorf("RefreshToken=%q", creds.RefreshToken)
	}
	if creds.ExpiresAt == nil {
		t.Error("ExpiresAt should not be nil")
	}
}

func TestGetATProtoCredentials_NoProfile(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	// User exists but has no profile row → should return nil, nil.
	userID := seedUser(t, s, fmt.Sprintf("atproto-norow-%d@example.com", time.Now().UnixNano()))
	creds, err := s.GetATProtoCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetATProtoCredentials (no profile): %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil credentials for user without profile, got %+v", creds)
	}
}

func TestGetATProtoCredentials_NoDID(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("atproto-nodid-%d@example.com", time.Now().UnixNano()))
	handle := fmt.Sprintf("nodid%d", time.Now().UnixNano()%99999)
	_, err := s.UpsertHandle(ctx, userID, handle, "")
	if err != nil {
		t.Fatalf("UpsertHandle: %v", err)
	}

	// No ConnectATProto call → DID is null → should return nil.
	creds, err := s.GetATProtoCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetATProtoCredentials (no DID): %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil for user with profile but no DID, got %+v", creds)
	}
}

func TestDisconnectATProto(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("atproto-disconnect-%d@example.com", time.Now().UnixNano()))
	handle := fmt.Sprintf("disconnect%d", time.Now().UnixNano()%99999)
	_, _ = s.UpsertHandle(ctx, userID, handle, "")

	expires := time.Now().Add(2 * time.Hour)
	_ = s.ConnectATProto(ctx, userID, "did:plc:x", "https://pds.example.com", "acc", "ref", &expires)

	if err := s.DisconnectATProto(ctx, userID); err != nil {
		t.Fatalf("DisconnectATProto: %v", err)
	}
	creds, err := s.GetATProtoCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetATProtoCredentials after disconnect: %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil after disconnect, got %+v", creds)
	}
}

func TestUpdateATProtoTokens(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	userID := seedUser(t, s, fmt.Sprintf("atproto-tokens-%d@example.com", time.Now().UnixNano()))
	handle := fmt.Sprintf("tokentest%d", time.Now().UnixNano()%99999)
	_, _ = s.UpsertHandle(ctx, userID, handle, "")

	expires := time.Now().Add(2 * time.Hour)
	_ = s.ConnectATProto(ctx, userID, "did:plc:tok", "https://pds.example.com", "old-acc", "old-ref", &expires)

	newExpires := time.Now().Add(4 * time.Hour).Truncate(time.Second)
	if err := s.UpdateATProtoTokens(ctx, userID, "new-acc", "new-ref", &newExpires); err != nil {
		t.Fatalf("UpdateATProtoTokens: %v", err)
	}

	creds, _ := s.GetATProtoCredentials(ctx, userID)
	if creds.AccessToken != "new-acc" {
		t.Errorf("AccessToken=%q, want 'new-acc'", creds.AccessToken)
	}
	if creds.RefreshToken != "new-ref" {
		t.Errorf("RefreshToken=%q, want 'new-ref'", creds.RefreshToken)
	}
}

func TestListUsersWithATProto(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	u1 := seedUser(t, s, fmt.Sprintf("list-atp1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("list-atp2-%d@example.com", time.Now().UnixNano()))
	u3 := seedUser(t, s, fmt.Sprintf("list-atp3-%d@example.com", time.Now().UnixNano()))
	h1 := fmt.Sprintf("latp1%d", time.Now().UnixNano()%99999)
	h2 := fmt.Sprintf("latp2%d", time.Now().UnixNano()%99999)
	h3 := fmt.Sprintf("latp3%d", time.Now().UnixNano()%99999)
	_, _ = s.UpsertHandle(ctx, u1, h1, "")
	_, _ = s.UpsertHandle(ctx, u2, h2, "")
	_, _ = s.UpsertHandle(ctx, u3, h3, "") // u3 gets handle but no AT Proto
	t.Cleanup(func() {
	})

	exp := time.Now().Add(2 * time.Hour)
	_ = s.ConnectATProto(ctx, u1, "did:plc:a1", "https://pds1.example.com", "acc1", "ref1", &exp)
	_ = s.ConnectATProto(ctx, u2, "did:plc:a2", "https://pds2.example.com", "acc2", "ref2", &exp)
	// u3 has no DID.

	users, err := s.ListUsersWithATProto(ctx)
	if err != nil {
		t.Fatalf("ListUsersWithATProto: %v", err)
	}
	// At minimum we should see u1 and u2 — there may be others from concurrent tests.
	found := 0
	for _, u := range users {
		if u.DID == "did:plc:a1" || u.DID == "did:plc:a2" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected to find 2 of our test users, found %d (total: %d)", found, len(users))
	}
}

// --- rkey tracking ---

func TestFollowATProtoRkey(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	base := time.Now().UnixNano() % 99999
	u1 := seedUser(t, s, fmt.Sprintf("rkey-f1-%d@example.com", time.Now().UnixNano()))
	u2 := seedUser(t, s, fmt.Sprintf("rkey-f2-%d@example.com", time.Now().UnixNano()))
	h1 := fmt.Sprintf("rkeyfh1%d", base)
	h2 := fmt.Sprintf("rkeyfh2%d", base)
	_, _ = s.UpsertHandle(ctx, u1, h1, "")
	_, _ = s.UpsertHandle(ctx, u2, h2, "")
	t.Cleanup(func() {
		_, _ = s.DB.Exec(`DELETE FROM user_follows WHERE follower_id=$1 OR followee_id=$1 OR follower_id=$2 OR followee_id=$2`, u1, u2)
	})
	_ = s.FollowUser(ctx, u1, h2)

	// No rkey yet.
	rkey, err := s.GetFollowATProtoRkey(ctx, u1, u2)
	if err != nil {
		t.Fatalf("GetFollowATProtoRkey (empty): %v", err)
	}
	if rkey != "" {
		t.Errorf("expected empty rkey, got %q", rkey)
	}

	// Set rkey.
	if err := s.SetFollowATProtoRkey(ctx, u1, u2, "rkey-follow-abc"); err != nil {
		t.Fatalf("SetFollowATProtoRkey: %v", err)
	}
	rkey, err = s.GetFollowATProtoRkey(ctx, u1, u2)
	if err != nil {
		t.Fatalf("GetFollowATProtoRkey (set): %v", err)
	}
	if rkey != "rkey-follow-abc" {
		t.Errorf("rkey=%q, want 'rkey-follow-abc'", rkey)
	}
}

func TestShareATProtoRkey(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	u := seedUser(t, s, fmt.Sprintf("rkey-share-%d@example.com", time.Now().UnixNano()))
	t.Cleanup(func() { _, _ = s.DB.Exec(`DELETE FROM shared_articles WHERE user_id=$1`, u) })

	sa, err := s.ShareArticle(ctx, u, "https://example.com/rkey-test", "T", "", "", "", "", "", nil)
	if err != nil {
		t.Fatalf("ShareArticle: %v", err)
	}

	// Initially no rkey.
	rkey, err := s.GetShareATProtoRkey(ctx, sa.ID)
	if err != nil {
		t.Fatalf("GetShareATProtoRkey (empty): %v", err)
	}
	if rkey != "" {
		t.Errorf("expected empty rkey, got %q", rkey)
	}

	// Set rkey.
	if err := s.SetShareATProtoRkey(ctx, sa.ID, "share-rkey-xyz"); err != nil {
		t.Fatalf("SetShareATProtoRkey: %v", err)
	}
	rkey, err = s.GetShareATProtoRkey(ctx, sa.ID)
	if err != nil {
		t.Fatalf("GetShareATProtoRkey (set): %v", err)
	}
	if rkey != "share-rkey-xyz" {
		t.Errorf("rkey=%q, want 'share-rkey-xyz'", rkey)
	}
}

// --- Relay cursor ---

func TestRelayCursor(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	// Initially seq=0.
	relayURL, seq, err := s.GetRelayCursor(ctx)
	if err != nil {
		t.Fatalf("GetRelayCursor: %v", err)
	}
	_ = relayURL
	_ = seq

	// Update.
	if err := s.UpdateRelayCursor(ctx, "https://relay.example.com", 12345); err != nil {
		t.Fatalf("UpdateRelayCursor: %v", err)
	}

	relayURL2, seq2, err := s.GetRelayCursor(ctx)
	if err != nil {
		t.Fatalf("GetRelayCursor after update: %v", err)
	}
	if seq2 != 12345 {
		t.Errorf("cursor_seq=%d, want 12345", seq2)
	}
	if relayURL2 != "https://relay.example.com" {
		t.Errorf("relay_url=%q", relayURL2)
	}

	// Another update.
	if err := s.UpdateRelayCursor(ctx, "https://relay.example.com", 99999); err != nil {
		t.Fatalf("UpdateRelayCursor second: %v", err)
	}
	_, seq3, _ := s.GetRelayCursor(ctx)
	if seq3 != 99999 {
		t.Errorf("cursor_seq=%d, want 99999", seq3)
	}

	// Restore original.
	_ = s.UpdateRelayCursor(ctx, relayURL, seq)
}
