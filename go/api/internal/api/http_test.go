package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/config"
	"github.com/fuegoio/sunred/go/api/internal/migrations"
	"github.com/fuegoio/sunred/go/api/internal/store"

	_ "github.com/lib/pq"
)

// testEnv is a fully wired API behind the real auth middleware, backed by a
// real Postgres database. Tests authenticate with a bearer API token seeded
// for an owned test user so the whole stack (auth resolution → store → huma
// handler → JSON) is exercised end-to-end.
type testEnv struct {
	handler http.Handler
	store   *store.Store
	userID  int
	token   string // bearer token for userID
	baseURL string // server URL (httptest)
	server  *httptest.Server
}

// newTestEnv builds an authenticated test environment. It skips when the
// database is unreachable. The caller owns no PDS credentials, so the
// fire-and-forget ATProto sync goroutines spawned by handlers are no-ops.
func newTestEnv(t *testing.T) *testEnv {
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
	if err := migrations.Run(db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	st := store.New(db)
	cfg := &config.Config{BaseURL: "http://test.local", WebURL: "http://test.local"}
	authInst, err := auth.New(cfg, db, st)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	humaMux := http.NewServeMux()
	humaCfg := huma.DefaultConfig("Sunred API", "1.0.0")
	humaCfg.Servers = []*huma.Server{{URL: ""}}
	humaCfg.Tags = OpenAPITags()
	humaRouter := humago.New(humaMux, humaCfg)

	apiHandler := New(humaRouter, st, authInst, cfg, nil)
	apiHandler.RegisterRoutes()

	// Health is mounted publicly; everything else behind auth middleware.
	mux := http.NewServeMux()
	mux.Handle("/v1/health", humaMux)
	mux.Handle("/", authInst.Middleware(humaMux))

	// Seed an owned user and a bearer token the test client will use.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	did := fmt.Sprintf("did:plc:http-%s", suffix)
	handle := fmt.Sprintf("http%s", suffix)
	userID, _, err := st.GetOrCreateUserByDID(context.Background(), did, handle)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	rawToken := "pla_test_" + suffix
	hash := auth.HashToken(rawToken)
	if _, err := st.CreateAPIToken(context.Background(), userID, "test", hash, "manual", nil); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &testEnv{
		handler: mux,
		store:   st,
		userID:  userID,
		token:   rawToken,
		baseURL: srv.URL,
		server:  srv,
	}
}

// do performs an authenticated request against the test API. body may be nil.
func (e *testEnv) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.baseURL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	return resp
}

// doRaw performs an authenticated request with a raw body (for non-JSON bodies
// like OPML XML).
func (e *testEnv) doRaw(t *testing.T, method, path, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, e.baseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	return resp
}

// unauthenticated request helper (no bearer token).
func (e *testEnv) doUnauth(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.baseURL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// readJSON decodes resp.Body into v and closes it.
func readJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestHTTP_Health(t *testing.T) {
	e := newTestEnv(t)

	resp := e.doUnauth(t, http.MethodGet, "/v1/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	readJSON(t, resp, &body)
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
}

func TestHTTP_UnauthorizedWithoutToken(t *testing.T) {
	e := newTestEnv(t)

	resp := e.doUnauth(t, http.MethodGet, "/v1/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	// The auth middleware must return a huma ErrorModel body whose `status`
	// field lets the web client distinguish a 401 (redirect to /login) from a
	// 5xx/network outage (render an error page).
	var body struct {
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	readJSON(t, resp, &body)
	if body.Status != http.StatusUnauthorized {
		t.Errorf("body status = %d, want %d", body.Status, http.StatusUnauthorized)
	}
	if body.Title == "" {
		t.Errorf("body title is empty")
	}
}

func TestHTTP_GetMe(t *testing.T) {
	e := newTestEnv(t)

	resp := e.do(t, http.MethodGet, "/v1/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var u store.User
	readJSON(t, resp, &u)
	if u.ID != e.userID {
		t.Errorf("user id = %d, want %d", u.ID, e.userID)
	}
}

func TestHTTP_UpdateMe(t *testing.T) {
	e := newTestEnv(t)

	resp := e.do(t, http.MethodPatch, "/v1/me", map[string]any{
		"display_name": "Ada Lovelace",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var u store.User
	readJSON(t, resp, &u)
	if u.DisplayName != "Ada Lovelace" {
		t.Errorf("display_name = %q, want %q", u.DisplayName, "Ada Lovelace")
	}

	// Persisted.
	got, err := e.store.GetUserByID(context.Background(), e.userID)
	if err != nil || got == nil {
		t.Fatalf("GetUserByID: %v %v", got, err)
	}
	if got.DisplayName != "Ada Lovelace" {
		t.Errorf("persisted display_name = %q", got.DisplayName)
	}
}

func TestHTTP_FoldersCRUD(t *testing.T) {
	e := newTestEnv(t)

	// Create.
	resp := e.do(t, http.MethodPost, "/v1/folders", map[string]any{
		"title": "Engineering",
	})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var folder store.Folder
	readJSON(t, resp, &folder)
	if folder.Title != "Engineering" {
		t.Fatalf("title = %q", folder.Title)
	}
	if folder.ID == 0 {
		t.Fatal("id not set")
	}

	// List.
	resp = e.do(t, http.MethodGet, "/v1/folders", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var folders []store.Folder
	readJSON(t, resp, &folders)
	if len(folders) != 1 || folders[0].ID != folder.ID {
		t.Errorf("folders = %+v", folders)
	}

	// Update (rename + nest under itself is disallowed in practice, so just rename).
	resp = e.do(t, http.MethodPatch, fmt.Sprintf("/v1/folders/%d", folder.ID), map[string]any{
		"title": "Eng",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	var updated store.Folder
	readJSON(t, resp, &updated)
	if updated.Title != "Eng" {
		t.Errorf("title = %q, want Eng", updated.Title)
	}

	// Get via list confirms persistence.
	resp = e.do(t, http.MethodGet, "/v1/folders", nil)
	readJSON(t, resp, &folders)
	if len(folders) != 1 || folders[0].Title != "Eng" {
		t.Errorf("folders after update = %+v", folders)
	}

	// Delete.
	resp = e.do(t, http.MethodDelete, fmt.Sprintf("/v1/folders/%d", folder.ID), nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = e.do(t, http.MethodGet, "/v1/folders", nil)
	readJSON(t, resp, &folders)
	if len(folders) != 0 {
		t.Errorf("folders after delete = %+v", folders)
	}
}

func TestHTTP_TokensCRUD(t *testing.T) {
	e := newTestEnv(t)

	// Create a new token (distinct from the auth token).
	resp := e.do(t, http.MethodPost, "/v1/tokens", map[string]any{"label": "ci"})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var created struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
		Token string `json:"token"`
	}
	readJSON(t, resp, &created)
	if created.ID == 0 || created.Label != "ci" || created.Token == "" {
		t.Fatalf("created token = %+v", created)
	}

	// List (includes the seeded auth token + the new one).
	resp = e.do(t, http.MethodGet, "/v1/tokens", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var tokens []store.APIToken
	readJSON(t, resp, &tokens)
	if len(tokens) < 2 {
		t.Errorf("expected >=2 tokens, got %d", len(tokens))
	}
	found := false
	for _, tk := range tokens {
		if tk.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("new token id %d not in list", created.ID)
	}

	// Delete.
	resp = e.do(t, http.MethodDelete, fmt.Sprintf("/v1/tokens/%d", created.ID), nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = e.do(t, http.MethodGet, "/v1/tokens", nil)
	readJSON(t, resp, &tokens)
	for _, tk := range tokens {
		if tk.ID == created.ID {
			t.Errorf("token %d still present after delete", created.ID)
		}
	}
}

func TestHTTP_FeedsListEmpty(t *testing.T) {
	e := newTestEnv(t)

	resp := e.do(t, http.MethodGet, "/v1/feeds", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var feeds []store.Feed
	readJSON(t, resp, &feeds)
	// New user has no subscriptions; the handler coerces nil to [].
	if feeds == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(feeds) != 0 {
		t.Errorf("expected 0 feeds, got %d", len(feeds))
	}
}

func TestHTTP_EntriesListEmpty(t *testing.T) {
	e := newTestEnv(t)

	resp := e.do(t, http.MethodGet, "/v1/entries", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var entries []store.Entry
	readJSON(t, resp, &entries)
	if entries == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestHTTP_EntryByURLStarAndRead(t *testing.T) {
	e := newTestEnv(t)
	article := fmt.Sprintf("https://article-%d.example.com/post", time.Now().UnixNano())

	// Star by URL (no materialized entry required).
	resp := e.do(t, http.MethodPut, "/v1/entries/by-url/starred", map[string]any{
		"article_url": article,
		"title":       "Starred via URL",
		"starred":     true,
	})
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("star status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Verify the star landed in the store.
	rkey, _ := e.store.GetStarATProtoRkey(context.Background(), e.userID, article)
	_ = rkey
	states, err := e.store.GetEntryStatesByURLs(context.Background(), e.userID, []string{article})
	if err != nil {
		t.Fatalf("GetEntryStatesByURLs: %v", err)
	}
	if !states[article].Starred {
		t.Errorf("expected article %q starred", article)
	}

	// Mark read by URL.
	resp = e.do(t, http.MethodPut, "/v1/entries/by-url", map[string]any{
		"article_url": article,
		"status":      "read",
	})
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	states, _ = e.store.GetEntryStatesByURLs(context.Background(), e.userID, []string{article})
	if states[article].Status != "read" {
		t.Errorf("status = %q, want read", states[article].Status)
	}

	// Unstar by URL.
	resp = e.do(t, http.MethodPut, "/v1/entries/by-url/starred", map[string]any{
		"article_url": article,
		"title":       "Starred via URL",
		"starred":     false,
	})
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("unstar status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	states, _ = e.store.GetEntryStatesByURLs(context.Background(), e.userID, []string{article})
	if states[article].Starred {
		t.Errorf("expected article %q unstarred", article)
	}
}

func TestHTTP_OPMLExportImportRoundTrip(t *testing.T) {
	e := newTestEnv(t)

	// Seed two feeds directly in the store so export has content.
	feed1, err := e.store.GetOrCreateFeed(context.Background(),
		"https://opml-a.example.com/rss", "https://opml-a.example.com", "OPML A", "")
	if err != nil {
		t.Fatalf("seed feed A: %v", err)
	}
	if _, err := e.store.CreateSubscription(context.Background(), e.userID, feed1.ID, nil, ""); err != nil {
		t.Fatalf("sub A: %v", err)
	}
	feed2, err := e.store.GetOrCreateFeed(context.Background(),
		"https://opml-b.example.com/rss", "https://opml-b.example.com", "OPML B", "")
	if err != nil {
		t.Fatalf("seed feed B: %v", err)
	}
	if _, err := e.store.CreateSubscription(context.Background(), e.userID, feed2.ID, nil, ""); err != nil {
		t.Fatalf("sub B: %v", err)
	}

	// Export.
	resp := e.do(t, http.MethodGet, "/v1/opml/export", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	xmlBody := string(body)
	if !strings.Contains(xmlBody, "https://opml-a.example.com/rss") {
		t.Errorf("export missing feed A url:\n%s", xmlBody)
	}
	if !strings.Contains(xmlBody, "OPML B") {
		t.Errorf("export missing feed B title:\n%s", xmlBody)
	}

	// Delete the subscriptions, then re-import the exported OPML to verify the
	// import path subscribes the user back to the same feeds.
	if err := e.store.DeleteSubscription(context.Background(), e.userID, feed1.ID); err != nil {
		t.Fatalf("delete sub A: %v", err)
	}
	if err := e.store.DeleteSubscription(context.Background(), e.userID, feed2.ID); err != nil {
		t.Fatalf("delete sub B: %v", err)
	}

	resp = e.doRaw(t, http.MethodPost, "/v1/opml/import", "text/xml", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d", resp.StatusCode)
	}
	var result struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
		Failed   int `json:"failed"`
	}
	readJSON(t, resp, &result)
	if result.Imported+result.Skipped != 2 {
		t.Errorf("import result = %+v, want 2 total", result)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}

	// Both feeds should be subscribed again.
	feeds, err := e.store.ListFeeds(context.Background(), e.userID)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 2 {
		t.Errorf("expected 2 feeds after import, got %d", len(feeds))
	}
}

func TestHTTP_RefreshFeedUnavailable(t *testing.T) {
	e := newTestEnv(t)

	// Refresh requires the processor, which is nil in the test env (no fetcher).
	resp := e.do(t, http.MethodPost, "/v1/feeds/1/refresh", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestHTTP_GetEntryNotFound(t *testing.T) {
	e := newTestEnv(t)

	resp := e.do(t, http.MethodGet, "/v1/entries/9999999", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
