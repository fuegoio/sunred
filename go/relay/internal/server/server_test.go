package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/relay/internal/fanout"
	"github.com/fuegoio/sunred/go/relay/internal/store"

	_ "github.com/lib/pq"
)

// relayTestDB connects to the relay test database and applies the schema. It
// skips when the database is unreachable. The DDL mirrors the embedded
// migrations (CREATE ... IF NOT EXISTS) so the test database stays current
// across runs without depending on the migrations bookkeeping table.
func relayTestDB(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("RELAY_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sunred:sunred@localhost:5432/sunred_relay_test?sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("could not open relay database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("relay database not reachable, skipping integration test: %v", err)
	}
	applySchema(t, db)
	return store.New(db)
}

// applySchema creates the relay tables if absent. Mirrors the layout in
// internal/migrations/*.sql but uses IF NOT EXISTS so it is idempotent on a
// persistent test database.
func applySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	ddl := `
CREATE TABLE IF NOT EXISTS instances (
  id         SERIAL PRIMARY KEY,
  url        TEXT NOT NULL UNIQUE,
  first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS tracked_dids (
  id            BIGSERIAL PRIMARY KEY,
  did           TEXT NOT NULL UNIQUE,
  pds_url       TEXT NOT NULL,
  handle        TEXT NOT NULL DEFAULT '',
  display_name  TEXT NOT NULL DEFAULT '',
  bio           TEXT NOT NULL DEFAULT '',
  avatar        TEXT NOT NULL DEFAULT '',
  banner        TEXT NOT NULL DEFAULT '',
  instance_id   INTEGER REFERENCES instances(id) ON DELETE SET NULL,
  cursor_seq    BIGINT NOT NULL DEFAULT 0,
  status        VARCHAR(32) NOT NULL DEFAULT 'active',
  error_msg     TEXT NOT NULL DEFAULT '',
  announced_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_event_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tracked_dids_handle_unique
  ON tracked_dids (handle) WHERE handle <> '';
CREATE TABLE IF NOT EXISTS observed_follows (
  id           BIGSERIAL PRIMARY KEY,
  follower_did TEXT NOT NULL,
  followee_did TEXT NOT NULL,
  rkey         TEXT NOT NULL,
  pds_url      TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (follower_did, followee_did, rkey)
);
CREATE TABLE IF NOT EXISTS observed_shares (
  id           BIGSERIAL PRIMARY KEY,
  did          TEXT NOT NULL,
  rkey         TEXT NOT NULL,
  article_url  TEXT NOT NULL DEFAULT '',
  feed_url     TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  pds_url      TEXT NOT NULL,
  shared_at    TIMESTAMPTZ,
  observed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE TABLE IF NOT EXISTS observed_subscriptions (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  feed_url    TEXT NOT NULL,
  pds_url     TEXT NOT NULL,
  created_at  TIMESTAMPTZ,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE TABLE IF NOT EXISTS observed_stars (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  article_url TEXT NOT NULL DEFAULT '',
  pds_url     TEXT NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE TABLE IF NOT EXISTS relay_events (
  seq        BIGSERIAL PRIMARY KEY,
  event_type VARCHAR(32) NOT NULL,
  did        TEXT NOT NULL,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("apply relay schema: %v", err)
	}
}

// newServer builds a relay Server backed by the test store. The fanout uses a
// large reconnect delay and an unreachable PDS so the worker goroutines spawned
// by announceUser stay dormant and do not flap the tracked_did status during
// the (short) test run.
func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st := relayTestDB(t)
	f := fanout.New(st, time.Hour) // dormant retries; tests don't exercise the WS path
	srv := New(st, f)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestServer_Health(t *testing.T) {
	ts, _ := newServer(t)

	resp := do(t, ts, http.MethodGet, "/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	decode(t, resp, &body)
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestServer_GetUserFollowerCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:folcount-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_follows WHERE followee_did=$1`, did) })

	now := time.Now()
	_, _ = st.RecordFollow(ctx, "did:plc:follower", did, "rf1", "https://pds.example.com", now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getUserFollowerCount?did="+did, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		DID   string `json:"did"`
		Count int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.DID != did {
		t.Errorf("did = %q, want %q", out.DID, did)
	}
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
}

func TestServer_GetUserFollowingCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:following-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_follows WHERE follower_did=$1`, did) })

	now := time.Now()
	_, _ = st.RecordFollow(ctx, did, "did:plc:followee", "rf1", "https://pds.example.com", now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getUserFollowingCount?did="+did, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		DID   string `json:"did"`
		Count int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.DID != did {
		t.Errorf("did = %q, want %q", out.DID, did)
	}
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
}

func TestServer_GetUserShareCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:sharecount-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_shares WHERE did=$1`, did) })

	now := time.Now()
	_, _ = st.RecordShare(ctx, did, "rs1", "https://art.example.com/a", "https://feed.example.com/rss", "T", "https://pds.example.com", &now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getUserShareCount?did="+did, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		DID   string `json:"did"`
		Count int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.DID != did {
		t.Errorf("did = %q, want %q", out.DID, did)
	}
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
}

func TestServer_GetUserSubscriptionCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	did := fmt.Sprintf("did:plc:subcount-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_subscriptions WHERE did=$1`, did) })

	now := time.Now()
	_, _ = st.RecordFeedSubscription(ctx, did, "rsub1", "https://feed.example.com/rss", "https://pds.example.com", &now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getUserSubscriptionCount?did="+did, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		DID   string `json:"did"`
		Count int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.DID != did {
		t.Errorf("did = %q, want %q", out.DID, did)
	}
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
}

func TestServer_UserCounts_MissingDID(t *testing.T) {
	ts, _ := newServer(t)

	for _, path := range []string{
		"/xrpc/io.sunred.relay.getUserFollowerCount",
		"/xrpc/io.sunred.relay.getUserFollowingCount",
		"/xrpc/io.sunred.relay.getUserShareCount",
		"/xrpc/io.sunred.relay.getUserSubscriptionCount",
	} {
		resp := do(t, ts, http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestServer_GetFeedSubscriberCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	feedURL := fmt.Sprintf("https://subcount-%d.example.com/rss", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_subscriptions WHERE feed_url=$1`, feedURL) })

	now := time.Now()
	_, _ = st.RecordFeedSubscription(ctx, "did:plc:a", "r1", feedURL, "https://pds.example.com", &now)
	_, _ = st.RecordFeedSubscription(ctx, "did:plc:b", "r2", feedURL, "https://pds.example.com", &now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getFeedSubscriberCount?feedUrl="+feedURL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		FeedURL string `json:"feedUrl"`
		Count   int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.FeedURL != feedURL {
		t.Errorf("feedUrl = %q, want %q", out.FeedURL, feedURL)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

func TestServer_GetArticleShareCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	article := fmt.Sprintf("https://artcount-%d.example.com/post", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_shares WHERE article_url=$1`, article) })

	now := time.Now()
	_, _ = st.RecordShare(ctx, "did:plc:a", "r1", article, "", "T", "https://pds.example.com", &now)
	_, _ = st.RecordShare(ctx, "did:plc:b", "r2", article, "", "T", "https://pds.example.com", &now)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getArticleShareCount?articleUrl="+article, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		ArticleURL string `json:"articleUrl"`
		Count      int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.ArticleURL != article {
		t.Errorf("articleUrl = %q, want %q", out.ArticleURL, article)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

func TestServer_GetArticleStarCount(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	article := fmt.Sprintf("https://starcount-%d.example.com/post", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM observed_stars WHERE article_url=$1`, article) })

	_, _ = st.RecordStar(ctx, "did:plc:a", "r1", article, "https://pds.example.com")
	_, _ = st.RecordStar(ctx, "did:plc:b", "r2", article, "https://pds.example.com")

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.getArticleStarCount?articleUrl="+article, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		ArticleURL string `json:"articleUrl"`
		Count      int64  `json:"count"`
	}
	decode(t, resp, &out)
	if out.ArticleURL != article {
		t.Errorf("articleUrl = %q, want %q", out.ArticleURL, article)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
}

func TestServer_SearchDIDs(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	instURL := fmt.Sprintf("https://search-%d.example.com", time.Now().UnixNano())
	instID, _ := st.UpsertInstance(ctx, instURL)
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID) })

	handle := fmt.Sprintf("findme-%d", time.Now().UnixNano())
	did := fmt.Sprintf("did:plc:%s", handle)
	_, _, _ = st.UpsertTrackedDID(ctx, did, "https://pds.example.com", handle, instID)
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did) })

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.searchDIDs?q="+handle, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Results []store.SearchResult `json:"results"`
	}
	decode(t, resp, &out)
	if len(out.Results) == 0 {
		t.Fatalf("expected at least 1 result for %q", handle)
	}
	found := false
	for _, r := range out.Results {
		if r.DID == did {
			found = true
			if r.Handle != handle {
				t.Errorf("handle = %q, want %q", r.Handle, handle)
			}
		}
	}
	if !found {
		t.Errorf("did %q not in results %+v", did, out.Results)
	}
}

func TestServer_ResolveHandle(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	instURL := fmt.Sprintf("https://resolve-%d.example.com", time.Now().UnixNano())
	instID, _ := st.UpsertInstance(ctx, instURL)
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM instances WHERE id=$1`, instID) })

	handle := fmt.Sprintf("resolveme-%d", time.Now().UnixNano())
	did := fmt.Sprintf("did:plc:%s", handle)
	pdsURL := "https://pds.example.com"
	_, _, _ = st.UpsertTrackedDID(ctx, did, pdsURL, handle, instID)
	t.Cleanup(func() { _, _ = st.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did) })

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.resolveHandle?handle="+handle, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		DID    string `json:"did"`
		PDSUrl string `json:"pdsUrl"`
	}
	decode(t, resp, &out)
	if out.DID != did {
		t.Errorf("did = %q, want %q", out.DID, did)
	}
	if out.PDSUrl != pdsURL {
		t.Errorf("pdsUrl = %q, want %q", out.PDSUrl, pdsURL)
	}
}

func TestServer_ResolveHandle_NotFound(t *testing.T) {
	ts, _ := newServer(t)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.resolveHandle?handle=nosuchhandle", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestServer_AnnounceUser(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	did := fmt.Sprintf("did:plc:announce-%d", suffix)
	handle := fmt.Sprintf("announce%d", suffix)
	instanceURL := fmt.Sprintf("https://inst-%d.example.com", suffix)
	t.Cleanup(func() {
		_, _ = st.DB.Exec(`DELETE FROM tracked_dids WHERE did=$1`, did)
		_, _ = st.DB.Exec(`DELETE FROM instances WHERE url=$1`, instanceURL)
	})

	resp := do(t, ts, http.MethodPost, "/xrpc/io.sunred.relay.announceUser", map[string]any{
		"did":         did,
		"pdsUrl":      "http://127.0.0.1:1", // unreachable PDS → dormant worker
		"instanceUrl": instanceURL,
		"handle":      handle,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Tracked bool `json:"tracked"`
		New     bool `json:"new"`
	}
	decode(t, resp, &out)
	if !out.Tracked {
		t.Error("expected tracked=true")
	}
	if !out.New {
		t.Error("expected new=true on first announce")
	}

	// The instance + tracked_did rows must exist.
	var instID int
	if err := st.DB.QueryRowContext(ctx, `SELECT id FROM instances WHERE url=$1`, instanceURL).Scan(&instID); err != nil {
		t.Fatalf("instance row not created: %v", err)
	}
	var gotDID string
	if err := st.DB.QueryRowContext(ctx, `SELECT did FROM tracked_dids WHERE did=$1`, did).Scan(&gotDID); err != nil {
		t.Fatalf("tracked_did row not created: %v", err)
	}
	if gotDID != did {
		t.Errorf("tracked did = %q", gotDID)
	}
}

func TestServer_AnnounceUser_MissingFields(t *testing.T) {
	ts, _ := newServer(t)

	resp := do(t, ts, http.MethodPost, "/xrpc/io.sunred.relay.announceUser", map[string]any{
		"did": "did:plc:incomplete",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestServer_AnnounceUser_BadJSON(t *testing.T) {
	ts, _ := newServer(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/xrpc/io.sunred.relay.announceUser", bytes.NewReader([]byte("{not json")))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestServer_AnnounceUser_MethodNotAllowed(t *testing.T) {
	ts, _ := newServer(t)

	resp := do(t, ts, http.MethodGet, "/xrpc/io.sunred.relay.announceUser", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
