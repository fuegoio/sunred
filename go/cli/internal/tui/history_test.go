package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// mustParseTime is a test helper for building entry fixtures.
func mustParseTimeHistory(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

// historyEntry builds a read entry fixture with distinct published and read
// times, so tests can tell which one a view renders.
func historyEntry(t *testing.T, id int64, title, published, read string) sunred.Entry {
	t.Helper()
	return sunred.Entry{
		Id:          id,
		Title:       title,
		Url:         "https://example.com/" + title,
		Status:      "read",
		Starred:     false,
		PublishedAt: mustParseTimeHistory(t, published),
		ChangedAt:   mustParseTimeHistory(t, read),
	}
}

// --- Sidebar ---

func TestRebuildSidebar_HistoryBelowStarred(t *testing.T) {
	m := NewModel(nil)
	m.rebuildSidebar()

	wantKinds := []sidebarItemKind{sidebarUnread, sidebarAll, sidebarStarred, sidebarHistory}
	wantLabels := []string{"Unread", "All", "Starred", "History"}
	if len(m.items) < 4 {
		t.Fatalf("expected at least 4 sidebar items, got %d", len(m.items))
	}
	for i, want := range wantKinds {
		if m.items[i].kind != want {
			t.Errorf("items[%d].kind = %v, want %v", i, m.items[i].kind, want)
		}
		if m.items[i].label != wantLabels[i] {
			t.Errorf("items[%d].label = %q, want %q", i, m.items[i].label, wantLabels[i])
		}
	}

	// The rendered sidebar shows the History row below Starred.
	var plain []string
	for _, l := range m.renderSidebarLines() {
		plain = append(plain, stripAnsiChars(l))
	}
	starredIdx, historyIdx := -1, -1
	for i, l := range plain {
		if strings.Contains(l, "Starred") {
			starredIdx = i
		}
		if strings.Contains(l, "History") {
			historyIdx = i
		}
	}
	if starredIdx == -1 || historyIdx == -1 {
		t.Fatalf("sidebar missing Starred (%d) or History (%d) row", starredIdx, historyIdx)
	}
	if historyIdx != starredIdx+1 {
		t.Errorf("History row at %d, want directly below Starred at %d", historyIdx, starredIdx+1)
	}
}

func TestRenderSidebar_HistoryHiddenDuringSearch(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 40
	m.rebuildSidebar()
	m.searchQuery = "query"

	var joined strings.Builder
	for _, l := range m.renderSidebarLines() {
		joined.WriteString(stripAnsiChars(l) + "\n")
	}
	if strings.Contains(joined.String(), "History") {
		t.Error("History nav item should be hidden while a search is active")
	}
}

// --- Main panel rendering ---

func TestRenderEntryList_HistoryTitleAndEntries(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 40
	m.focus = focusEntries
	m.rebuildSidebar()
	m.sidebarCursor = 3 // History
	m.entries = []sunred.Entry{
		historyEntry(t, 7, "Read most recently", "2026-08-01T10:00:00Z", "2026-08-28T09:07:56Z"),
		historyEntry(t, 9, "Read earlier", "2026-08-20T10:00:00Z", "2026-08-25T20:28:40Z"),
	}

	out := stripAnsiChars(m.renderEntryList(100))
	if !strings.Contains(out, "History") {
		t.Errorf("main panel title should be 'History', got:\n%s", out)
	}
	for _, want := range []string{"Read most recently", "Read earlier"} {
		if !strings.Contains(out, want) {
			t.Errorf("history entry %q missing from panel; got:\n%s", want, out)
		}
	}
	// The date column shows the read date (ChangedAt), not the published
	// date, matching the list's read-time ordering.
	if !strings.Contains(out, "2026-08-28") || !strings.Contains(out, "2026-08-25") {
		t.Errorf("date column should show read dates (2026-08-28, 2026-08-25); got:\n%s", out)
	}
	if strings.Contains(out, "2026-08-01") || strings.Contains(out, "2026-08-20") {
		t.Errorf("date column should not show published dates in history view; got:\n%s", out)
	}
}

func TestRenderEntryList_HistoryEmptyMessage(t *testing.T) {
	m := NewModel(nil)
	m.width, m.height = 120, 40
	m.focus = focusEntries
	m.rebuildSidebar()
	m.sidebarCursor = 3 // History

	out := stripAnsiChars(m.renderEntryList(100))
	if !strings.Contains(out, "History") {
		t.Errorf("main panel title should be 'History', got:\n%s", out)
	}
	if !strings.Contains(out, "Nothing in your history yet") {
		t.Errorf("history empty state missing; got:\n%s", out)
	}
}

// --- Loading ---

// historyAPITestServer serves GET /v1/history. Requests to any other path
// fail the test, so a regression that loads history via /v1/entries is caught.
func historyAPITestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/history" {
			t.Errorf("unexpected request path %q, want /v1/history", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Errorf("limit = %q, want 200 (page size used by the TUI)", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {"id": 7, "title": "Read most recently", "status": "read",
		   "published_at": "2026-08-01T10:00:00Z", "changed_at": "2026-08-28T09:07:56Z"},
		  {"id": 9, "title": "Read earlier", "status": "read",
		   "published_at": "2026-08-20T10:00:00Z", "changed_at": "2026-08-25T20:28:40Z"}
		]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLoadSelection_HistoryUsesHistoryEndpoint(t *testing.T) {
	srv := historyAPITestServer(t)
	c, err := sunred.NewClientWithResponses(srv.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	m := NewModel(c)
	m.rebuildSidebar()
	if len(m.items) < 4 || m.items[3].kind != sidebarHistory {
		t.Fatalf("sidebar item 3 should be History, got %+v", m.items)
	}

	// loadSelection for the History item issues the history load; tea.Batch
	// with a single command returns that command directly.
	cmd := m.loadSelection(m.items[3])
	if cmd == nil {
		t.Fatal("loadSelection(History) returned no command")
	}
	msg := cmd()
	loadMsg, ok := msg.(loadEntriesMsg)
	if !ok {
		t.Fatalf("expected loadEntriesMsg, got %T", msg)
	}
	if loadMsg.err != nil {
		t.Fatalf("history load error: %v", loadMsg.err)
	}
	if len(loadMsg.entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(loadMsg.entries))
	}
	if loadMsg.entries[0].Title != "Read most recently" {
		t.Errorf("entries[0].Title = %q, want most recently read first", loadMsg.entries[0].Title)
	}
}

// The history load feeds the shared loadEntriesMsg, so Update stores it in
// the entries panel like any other view.
func TestUpdate_HistoryLoadMsgPopulatesEntries(t *testing.T) {
	m := NewModel(nil)
	entries := []sunred.Entry{historyEntry(t, 7, "Read article", "2026-08-01T10:00:00Z", "2026-08-28T09:07:56Z")}
	m2, _ := m.Update(loadEntriesMsg{entries: entries})
	model := m2.(Model)
	if len(model.entries) != 1 || model.entries[0].Title != "Read article" {
		t.Fatalf("Update(loadEntriesMsg) should populate entries, got %+v", model.entries)
	}
}
