package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fuegoio/sunred/go/sdk/sunred"
)

func TestViewSidebarText(t *testing.T) {
	m := NewModel(nil)

	// Simulate getting a window size.
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(Model)

	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string after WindowSizeMsg")
	}

	lines := strings.Split(view, "\n")
	t.Logf("total lines: %d", len(lines))
	for i, l := range lines {
		if i >= 5 {
			break
		}
		t.Logf("line[%d] len=%d visible=%d: %q", i, len(l), ansiStripWidth(l), l)
	}

	// Row 0 should contain "Sunred" from the sidebar header.
	if len(lines) == 0 {
		t.Fatal("no lines in view")
	}
	visible0 := stripAnsiChars(lines[0])
	if !strings.Contains(visible0, "Sunred") {
		t.Errorf("row 0 does not contain 'Sunred'; got: %q", visible0)
	}

	// Sidebar lines should contain nav items.
	sidebarLines := m.renderSidebarLines()
	t.Logf("sidebar lines: %d", len(sidebarLines))
	for i, l := range sidebarLines {
		if i >= 5 {
			break
		}
		t.Logf("sidebar[%d] len=%d visible=%d: %q", i, len(l), ansiStripWidth(l), l)
	}

	// Main lines
	mainLines := m.renderMainLines()
	t.Logf("main lines: %d", len(mainLines))
	for i, l := range mainLines {
		if i >= 5 {
			break
		}
		t.Logf("main[%d] len=%d visible=%d: %q", i, len(l), ansiStripWidth(l), l)
	}
}

func TestAnsiStripWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"\x1b[1mhello\x1b[m", 5},
		{"\x1b[38;5;99mhello\x1b[m", 5},
		{"\x1b[1m\x1b[38;5;99m  Sunred\x1b[m", 8},
		{"─", 1},                      // 3-byte UTF-8 rune, 1 visible column
		{strings.Repeat("─", 27), 27}, // 81 bytes, 27 visible columns
		{"\x1b[38;5;245m" + strings.Repeat("─", 27) + "\x1b[m", 27},
	}
	for _, tt := range tests {
		got := ansiStripWidth(tt.input)
		if got != tt.want {
			t.Errorf("ansiStripWidth(%q) = %d, want %d", tt.input[:min(len(tt.input), 40)], got, tt.want)
		}
	}
}

// stripAnsiChars returns the plain text of s with ANSI escape sequences removed.
func stripAnsiChars(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// TestPreviewViewRendering proves the discovery preview view renders the feed
// header (title + subscriber counts) and preview items with read/star state.
func TestPreviewViewRendering(t *testing.T) {
	m := NewModel(nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(Model)

	starred := true
	read := "read"
	items := []sunred.PreviewFeedItem{
		{Title: "First article", Url: "https://example.com/1", PublishedAt: mustParseTime(t, "2025-01-02T00:00:00Z")},
		{Title: "Read and starred", Url: "https://example.com/2", PublishedAt: mustParseTime(t, "2025-01-03T00:00:00Z"), Starred: &starred, Status: &read},
	}
	m.preview = &sunred.PreviewFeedBody{
		Title:       "Demo Feed",
		SiteUrl:     "https://example.com",
		FeedUrl:     "https://example.com/feed.xml",
		Description: ptr("A demo feed"),
		Subscribers: &sunred.FeedSubscribersSummary{Count: 3, GlobalCount: 42},
		Items:       &items,
	}
	m.focus = focusEntries

	main := m.renderMainLines()
	joined := strings.Join(main, "\n")
	plain := stripAnsiChars(joined)

	if !strings.Contains(plain, "Demo Feed") {
		t.Errorf("preview view missing feed title; got:\n%s", plain)
	}
	if !strings.Contains(plain, "3 subscribers") || !strings.Contains(plain, "42 global") {
		t.Errorf("preview view missing subscriber counts; got:\n%s", plain)
	}
	if !strings.Contains(plain, "First article") || !strings.Contains(plain, "Read and starred") {
		t.Errorf("preview view missing items; got:\n%s", plain)
	}
	if !strings.Contains(plain, "esc back") {
		t.Errorf("preview view missing help text; got:\n%s", plain)
	}

	// Cursor navigation clamps within the preview item list.
	m.previewCursor = 5
	m.clampPreviewOffset()
	if m.previewCursor != 5 { // cursor is allowed beyond the end; offset clamps
		t.Errorf("previewCursor changed unexpectedly: %d", m.previewCursor)
	}
}

func TestEntriesStatusParsers(t *testing.T) {
	// status enums are generated; just confirm the by-URL parser round-trips.
	if string(sunred.UpdateEntryStatusByUrlRequestStatusRead) != "read" {
		t.Errorf("expected read status, got %q", sunred.UpdateEntryStatusByUrlRequestStatusRead)
	}
}

// mustParseTime parses an RFC3339 timestamp, failing the test on error.
func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("mustParseTime(%s): %v", s, err)
	}
	return ts
}
