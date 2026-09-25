package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// historyTestServer spins up an API stub for GET /v1/history. Any request to
// another path fails the test, so a regression that points the history view
// at /v1/entries is caught.
func historyTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/history" {
			t.Errorf("unexpected request path %q, want /v1/history", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func historyTestClient(t *testing.T, srv *httptest.Server) *sunred.ClientWithResponses {
	t.Helper()
	c, err := sunred.NewClientWithResponses(srv.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c
}

// Two entries, one with a nested feed and one without, plus a third whose
// feed has an empty title — exercising both feed-column fallbacks.
const historyFixture = `[
  {"id": 7, "title": "Read with feed", "url": "https://example.com/a",
   "changed_at": "2026-08-28T09:07:56Z",
   "feed": {"id": 3, "title": "Feed A"}},
  {"id": 9, "title": "Read without feed", "url": "https://example.com/b",
   "changed_at": "2026-08-25T20:28:40Z"}
]`

func TestRunHistory(t *testing.T) {
	srv := historyTestServer(t, historyFixture)
	var out bytes.Buffer
	if err := runHistory(&out, historyTestClient(t, srv), 50); err != nil {
		t.Fatalf("runHistory: %v", err)
	}

	outStr := out.String()
	for _, want := range []string{
		"ID", "READ AT", "FEED", "TITLE",
		"7", "2026-08-28 09:07", "Feed A", "Read with feed",
		"9", "2026-08-25 20:28", "Read without feed",
	} {
		if !strings.Contains(outStr, want) {
			t.Errorf("output missing %q; got:\n%s", want, outStr)
		}
	}
}

func TestRunHistory_EmptyHistory(t *testing.T) {
	srv := historyTestServer(t, `[]`)
	var out bytes.Buffer
	if err := runHistory(&out, historyTestClient(t, srv), 50); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	lines := strings.TrimSpace(out.String())
	if lines != "ID  READ AT  FEED  TITLE" {
		t.Errorf("expected header-only output for empty history, got %q", lines)
	}
}

func TestRunHistory_APIThroughError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"title": "Internal Server Error", "detail": "boom"}`))
	}))
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	err := runHistory(&out, historyTestClient(t, srv), 50)
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should surface status and detail, got: %v", err)
	}
}

func TestRunHistory_LimitClamped(t *testing.T) {
	var gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	if err := runHistory(&out, historyTestClient(t, srv), 500); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if gotLimit != "200" {
		t.Errorf("limit = %q, want clamped to 200", gotLimit)
	}
}

func TestRunHistory_ZeroLimitUsesDefault(t *testing.T) {
	var gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	if err := runHistory(&out, historyTestClient(t, srv), 0); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if gotLimit != "50" {
		t.Errorf("limit = %q, want default 50", gotLimit)
	}
}

func TestHistoryCmdFlagDefault(t *testing.T) {
	flag := historyCmd.Flags().Lookup("limit")
	if flag == nil {
		t.Fatal("history command has no --limit flag")
	}
	if flag.DefValue != "50" {
		t.Errorf("--limit default = %q, want 50", flag.DefValue)
	}
}
