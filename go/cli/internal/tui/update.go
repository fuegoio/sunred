package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// Update handles messages and keyboard input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case loadFeedsMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.feeds = msg.feeds
		m.err = ""
		m.rebuildSidebar()
		// load entries for whatever sidebar item is currently selected
		if len(m.items) > 0 && m.sidebarCursor < len(m.items) {
			m.loading = true
			return m, m.loadSelection(m.items[m.sidebarCursor])
		}
		return m, nil

	case loadFoldersMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.folders = msg.folders
		m.err = ""
		m.rebuildSidebar()
		return m, nil

	case loadEntriesMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.entries = msg.entries
		m.entriesCursor = 0
		m.entriesOffset = 0
		m.preview = nil
		m.err = ""
		return m, nil

	case markReadMsg:
		if msg.err == nil {
			for i, e := range m.entries {
				if e.Id == msg.entryID {
					m.entries[i].Status = string(msg.status)
				}
			}
		}
		return m, nil

	case toggleStarMsg:
		if msg.err == nil {
			for i, e := range m.entries {
				if e.Id == msg.entryID {
					m.entries[i].Starred = msg.starred
				}
			}
		}
		return m, nil

	case feedSubsMsg:
		if m.subsLoading == msg.feedID {
			m.subsLoading = 0
		}
		if msg.err != nil {
			// Non-fatal: just leave the count unset.
			return m, nil
		}
		m.subsCache[msg.feedID] = *msg.subs
		if m.subsFeedID == msg.feedID {
			c := *msg.subs
			m.subsLast = &c
		}
		return m, nil

	case feedRefreshedMsg:
		if msg.err != nil {
			m.err = "refresh failed: " + msg.err.Error()
			return m, nil
		}
		// Reload entries for the current selection to pick up new items.
		if len(m.items) > 0 && m.sidebarCursor < len(m.items) {
			m.loading = true
			m.entries = nil
			return m, m.loadSelection(m.items[m.sidebarCursor])
		}
		return m, nil

	case feedMarkedReadMsg:
		if msg.err != nil {
			m.err = "mark-read failed: " + msg.err.Error()
			return m, nil
		}
		// Reflect the change locally: mark visible entries of this feed as read.
		for i, e := range m.entries {
			if e.FeedId == msg.feedID {
				m.entries[i].Status = "read"
			}
		}
		return m, nil

	case previewFeedMsg:
		m.loading = false
		m.enteringURL = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.preview = nil
			return m, nil
		}
		m.preview = msg.preview
		m.previewCursor = 0
		m.previewOffset = 0
		m.err = ""
		m.focus = focusEntries
		return m, nil

	case byUrlStatusMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		if m.preview != nil && m.preview.Items != nil {
			for i := range *m.preview.Items {
				if (*m.preview.Items)[i].Url == msg.articleURL {
					s := string(msg.status)
					(*m.preview.Items)[i].Status = &s
				}
			}
		}
		return m, nil

	case byUrlStarMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		if m.preview != nil && m.preview.Items != nil {
			for i := range *m.preview.Items {
				if (*m.preview.Items)[i].Url == msg.articleURL {
					b := msg.starred
					(*m.preview.Items)[i].Starred = &b
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey dispatches key events to the focused panel.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// URL input mode (preview discovery) intercepts everything except ctrl+c.
	if m.enteringURL {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleURLKey(msg)
	}

	// Search mode intercepts everything except ctrl+c.
	if m.searching {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.searchQuery = ""
		m.focus = focusEntries
		return m, nil
	case "p":
		// Enter preview/discovery URL input.
		m.enteringURL = true
		m.urlInput = ""
		m.focus = focusEntries
		return m, nil
	case "r":
		m.loading = true
		return m, tea.Batch(loadFeeds(m.client), loadFolders(m.client))
	}

	// Preview view keys are handled by the entries panel handler.
	if m.preview != nil && m.focus == focusEntries {
		return m.handlePreviewKey(msg)
	}

	if m.focus == focusSidebar {
		return m.handleSidebarKey(msg)
	}
	return m.handleEntriesKey(msg)
}

func (m Model) handleURLKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.enteringURL = false
		m.urlInput = ""
		return m, nil
	case "enter":
		url := m.urlInput
		m.enteringURL = false
		m.urlInput = ""
		if url == "" {
			return m, nil
		}
		m.loading = true
		m.preview = nil
		return m, previewFeed(m.client, url)
	case "backspace", "ctrl+h":
		runes := []rune(m.urlInput)
		if len(runes) > 0 {
			m.urlInput = string(runes[:len(runes)-1])
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.urlInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel: restore the previous sidebar selection.
		m.searching = false
		m.searchQuery = ""
		if len(m.items) > 0 {
			m.loading = true
			m.entries = nil
			m.preview = nil
			return m, m.loadSelection(m.items[m.sidebarCursor])
		}
		return m, nil
	case "enter":
		// Confirm: commit the search and focus the results list.
		m.searching = false
		m.focus = focusEntries
		m.entriesCursor = 0
		m.entriesOffset = 0
		m.preview = nil
		if m.searchQuery == "" {
			// Empty query: restore current sidebar selection.
			m.loading = true
			m.entries = nil
			if len(m.items) > 0 {
				return m, m.loadSelection(m.items[m.sidebarCursor])
			}
			return m, nil
		}
		// Results are already loaded live; nothing more to fetch.
		return m, nil
	case "backspace", "ctrl+h":
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
		}
		// Live search on delete.
		if m.searchQuery != "" {
			m.loading = true
			m.entries = nil
			m.entriesCursor = 0
			m.entriesOffset = 0
			return m, searchEntries(m.client, m.searchQuery)
		}
		return m, nil
	default:
		// Append printable characters.
		if msg.Type == tea.KeyRunes {
			m.searchQuery += string(msg.Runes)
			m.loading = true
			m.entries = nil
			m.entriesCursor = 0
			m.entriesOffset = 0
			return m, searchEntries(m.client, m.searchQuery)
		}
	}
	return m, nil
}

func (m Model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	prev := m.sidebarCursor
	switch msg.String() {
	case "up", "k":
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
		}
	case "down", "j":
		if m.sidebarCursor < len(m.items)-1 {
			m.sidebarCursor++
		}
	case "g":
		m.sidebarCursor = 0
	case "G":
		m.sidebarCursor = max(0, len(m.items)-1)
	case "enter", "l", "right":
		if len(m.items) == 0 {
			return m, nil
		}
		m.focus = focusEntries
		m.loading = true
		m.entries = nil
		m.searchQuery = ""
		m.preview = nil
		return m, m.loadSelection(m.items[m.sidebarCursor])
	}
	// If the cursor moved, refresh the entry panel for the new selection.
	if m.sidebarCursor != prev && len(m.items) > 0 {
		m.loading = true
		m.entries = nil
		m.searchQuery = ""
		m.preview = nil
		return m, m.loadSelection(m.items[m.sidebarCursor])
	}
	return m, nil
}

func (m Model) handleEntriesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.entriesCursor > 0 {
			m.entriesCursor--
		}
	case "down", "j":
		if m.entriesCursor < len(m.entries)-1 {
			m.entriesCursor++
		}
	case "g":
		m.entriesCursor = 0
	case "G":
		m.entriesCursor = max(0, len(m.entries)-1)
	case "ctrl+d":
		pageSize := m.entriesPageSize()
		m.entriesCursor = min(m.entriesCursor+pageSize/2, len(m.entries)-1)
	case "ctrl+u":
		pageSize := m.entriesPageSize()
		m.entriesCursor = max(m.entriesCursor-pageSize/2, 0)
	case "enter", "o":
		// Open in browser and mark as read.
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.entriesCursor]
		var cmds []tea.Cmd
		if entry.Status != "read" {
			cmds = append(cmds, setEntryStatus(m.client, entry.Id, sunred.UpdateEntriesRequestStatusRead))
		}
		cmds = append(cmds, openURL(entry.Url))
		m.clampEntriesOffset()
		return m, tea.Batch(cmds...)
	case "u":
		// Toggle read / unread.
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.entriesCursor]
		newStatus := sunred.UpdateEntriesRequestStatusRead
		if entry.Status == "read" {
			newStatus = sunred.UpdateEntriesRequestStatusUnread
		}
		return m, setEntryStatus(m.client, entry.Id, newStatus)
	case "s":
		// Toggle star.
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.entriesCursor]
		return m, toggleStar(m.client, entry.Id, !entry.Starred)
	case "R":
		// Refresh the selected feed (matches the web FeedHeader refresh action).
		if len(m.items) == 0 || m.sidebarCursor >= len(m.items) {
			return m, nil
		}
		item := m.items[m.sidebarCursor]
		if item.kind != sidebarFeed {
			return m, nil
		}
		return m, refreshFeed(m.client, item.feedID)
	case "M":
		// Mark all entries in the selected feed as read.
		if len(m.items) == 0 || m.sidebarCursor >= len(m.items) {
			return m, nil
		}
		item := m.items[m.sidebarCursor]
		if item.kind != sidebarFeed {
			return m, nil
		}
		return m, markFeedRead(m.client, item.feedID)
	case "esc", "h", "left":
		m.focus = focusSidebar
	case "q":
		return m, tea.Quit
	}
	m.clampEntriesOffset()
	return m, nil
}

// handlePreviewKey handles keys while the discovery preview view is shown.
func (m Model) handlePreviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.previewCursor > 0 {
			m.previewCursor--
		}
	case "down", "j":
		if m.preview != nil && m.preview.Items != nil && m.previewCursor < len(*m.preview.Items)-1 {
			m.previewCursor++
		}
	case "g":
		m.previewCursor = 0
	case "G":
		if m.preview != nil && m.preview.Items != nil {
			m.previewCursor = max(0, len(*m.preview.Items)-1)
		}
	case "ctrl+d":
		ps := m.previewPageSize()
		m.previewCursor = min(m.previewCursor+ps/2, m.previewItemCount()-1)
	case "ctrl+u":
		ps := m.previewPageSize()
		m.previewCursor = max(m.previewCursor-ps/2, 0)
	case "enter", "o":
		// Open in browser and mark as read (by URL — preview items have no id).
		item, ok := m.selectedPreviewItem()
		if !ok {
			return m, nil
		}
		status := sunred.UpdateEntryStatusByUrlRequestStatusRead
		read := item.Status != nil && *item.Status == "read"
		cmds := []tea.Cmd{openURL(item.Url)}
		if !read {
			cmds = append(cmds, setEntryStatusByUrl(m.client, item.Url, status))
		}
		m.clampPreviewOffset()
		return m, tea.Batch(cmds...)
	case "u":
		// Toggle read / unread by URL.
		item, ok := m.selectedPreviewItem()
		if !ok {
			return m, nil
		}
		read := item.Status != nil && *item.Status == "read"
		status := sunred.UpdateEntryStatusByUrlRequestStatusRead
		if read {
			status = sunred.UpdateEntryStatusByUrlRequestStatusUnread
		}
		return m, setEntryStatusByUrl(m.client, item.Url, status)
	case "s":
		// Toggle star by URL.
		item, ok := m.selectedPreviewItem()
		if !ok {
			return m, nil
		}
		starred := item.Starred != nil && *item.Starred
		return m, toggleStarByUrl(m.client, item, m.preview, !starred)
	case "esc", "h", "left":
		// Leave preview: restore the current sidebar selection.
		m.preview = nil
		m.focus = focusSidebar
		if len(m.items) > 0 {
			m.loading = true
			m.entries = nil
			return m, m.loadSelection(m.items[m.sidebarCursor])
		}
		return m, nil
	case "q":
		return m, tea.Quit
	}
	m.clampPreviewOffset()
	return m, nil
}

// ---- View ------------------------------------------------------------------

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.err != "" {
		return errStyle.Render("error: " + m.err + "\n\nr refresh · q quit")
	}

	sidebarLines := m.renderSidebarLines()
	mainLines := m.renderMainLines()

	// Join sidebar and main manually: each sidebar line gets '│' appended,
	// then the corresponding main line follows. This avoids all lipgloss
	// Width/border interactions with ANSI-colored content.
	// Cap at m.height to prevent output from scrolling past the visible area.
	totalRows := max(len(sidebarLines), len(mainLines))
	if m.height > 0 && totalRows > m.height {
		totalRows = m.height
	}
	innerWidth := sidebarWidth - 1
	borderColor := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))

	var out strings.Builder
	for i := 0; i < totalRows; i++ {
		// sidebar cell
		sl := ""
		if i < len(sidebarLines) {
			sl = sidebarLines[i]
		} else {
			sl = strings.Repeat(" ", innerWidth)
		}
		out.WriteString(sl)
		out.WriteString(borderColor.Render("│"))

		// main cell
		if i < len(mainLines) {
			out.WriteString(mainLines[i])
		}

		if i < totalRows-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

// ---- Sidebar ---------------------------------------------------------------

// renderSidebarLines returns one string per terminal row (no trailing newline),
// each exactly innerWidth visible columns wide.
func (m Model) renderSidebarLines() []string {
	innerWidth := sidebarWidth - 1
	var lines []string

	addLine := func(rendered string) {
		lines = append(lines, sidebarLine(rendered, innerWidth))
	}

	// Header
	addLine(headerStyle.Render(truncate("☀ Sunred", innerWidth-2)))
	addLine(strings.Repeat("─", innerWidth))

	// Search input box replaces the nav items when searching.
	if m.searching {
		// Active input: show cursor.
		prompt := searchInputStyle.Render("/") + " "
		cursor := searchCursorStyle.Render("▌")
		query := searchInputStyle.Render(m.searchQuery)
		addLine(prompt + query + cursor)
		addLine("")
	} else if m.searchQuery != "" {
		// Committed search result: show as a static label.
		label := dimStyle.Render("/ ") + searchInputStyle.Render(m.searchQuery)
		addLine(label)
		addLine("")
	}

	// Preview URL input box (discovery).
	if m.enteringURL {
		prompt := searchInputStyle.Render("p ")
		cursor := searchCursorStyle.Render("▌")
		query := searchInputStyle.Render(m.urlInput)
		addLine(prompt + query + cursor)
		addLine("")
	}

	if !m.searching && m.searchQuery == "" && !m.enteringURL && m.loading && len(m.items) == 0 {
		addLine(dimStyle.Render("  Loading…"))
	}

	feedsLabelShown := false
	for i, item := range m.items {
		// When in search/preview-input mode (active or committed), skip nav items.
		if (m.searching || m.searchQuery != "" || m.enteringURL) && (item.kind == sidebarAll || item.kind == sidebarUnread || item.kind == sidebarStarred) {
			continue
		}
		// Emit the "FEEDS" section label before the first feed or folder.
		if !feedsLabelShown && (item.kind == sidebarFeed || item.kind == sidebarFolder) {
			addLine("")
			label := padRight(" FEEDS", innerWidth)
			addLine(sectionLabelStyle.Render(label))
			feedsLabelShown = true
		}

		isFocused := m.focus == focusSidebar && i == m.sidebarCursor
		isActive := m.focus == focusEntries && i == m.sidebarCursor

		indent := strings.Repeat("  ", item.depth)
		var icon string
		switch item.kind {
		case sidebarAll:
			icon = "≡ "
		case sidebarUnread:
			icon = "○ "
		case sidebarStarred:
			icon = "✦ "
		case sidebarFolder:
			icon = "▸ "
		default:
			icon = "  "
		}

		maxLabel := innerWidth - utf8.RuneCountInString(indent) - 1 - utf8.RuneCountInString(icon)
		label := truncate(item.label, maxLabel)
		plain := padRight(" "+indent+icon+label, innerWidth)

		var rendered string
		switch {
		case isFocused:
			rendered = selectedFocusStyle.Render(plain)
		case isActive:
			rendered = selectedStyle.Render(plain)
		case item.kind == sidebarFolder:
			rendered = folderStyle.Render(plain)
		default:
			rendered = plain
		}
		addLine(rendered)
	}

	// blank rows to fill height (use actual line count as ground truth)
	for len(lines) < m.height-1 {
		addLine("")
	}

	// help line at bottom
	addLine(mutedStyle.Render(truncate("  r reload · q quit", innerWidth)))

	return lines
}

// sidebarLine pads a pre-rendered (possibly ANSI-colored) string to innerWidth
// visible columns by appending plain spaces. It does NOT pass the string
// through any lipgloss Width() render, avoiding the wrapping bug.
func sidebarLine(rendered string, innerWidth int) string {
	visible := ansiStripWidth(rendered)
	need := innerWidth - visible
	if need <= 0 {
		return rendered
	}
	return rendered + strings.Repeat(" ", need)
}

// ansiStripWidth returns the visible (column) width of s, ignoring ANSI escapes.
func ansiStripWidth(s string) int {
	w := 0
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
		w++
	}
	return w
}

// ---- Main panel ------------------------------------------------------------

// renderMainLines returns the main panel content as a slice of lines.
func (m Model) renderMainLines() []string {
	mainWidth := m.width - sidebarWidth - 1
	if mainWidth < 10 {
		mainWidth = 10
	}

	var content string
	switch {
	case m.enteringURL:
		content = dimStyle.Render("  Enter a feed URL to preview, then press enter…")
	case m.loading && m.preview == nil:
		content = dimStyle.Render("  Loading…")
	case m.preview != nil:
		content = m.renderPreview(mainWidth)
	default:
		content = m.renderEntryList(mainWidth)
	}

	return strings.Split(content, "\n")
}

func (m Model) renderEntryList(width int) string {
	var sb strings.Builder

	title := "Entries"
	if m.searching || m.searchQuery != "" {
		title = "Search"
	} else if len(m.items) > 0 && m.sidebarCursor < len(m.items) {
		item := m.items[m.sidebarCursor]
		switch item.kind {
		case sidebarAll:
			title = "All Entries"
		case sidebarUnread:
			title = "Unread"
		case sidebarStarred:
			title = "Starred"
		default:
			title = item.label
		}
	}

	headerLine := headerStyle.Render(title)
	if len(m.entries) > 0 {
		headerLine += " " + dimStyle.Render(formatPos(m.entriesCursor+1, len(m.entries)))
	}
	sb.WriteString(headerLine + "\n")

	// Feed detail header: for a feed selection, show description/site URL and
	// subscriber counts (mirrors the web FeedHeader) above the separator.
	if len(m.items) > 0 && m.sidebarCursor < len(m.items) {
		item := m.items[m.sidebarCursor]
		if item.kind == sidebarFeed {
			if f, ok := m.feedsByID[item.feedID]; ok {
				subtitle := f.SiteUrl
				if f.Description != nil && *f.Description != "" {
					subtitle = truncate(*f.Description, width-2)
				}
				sb.WriteString(dimStyle.Render("  "+truncate(subtitle, width-2)) + "\n")

				subsLine := ""
				if m.subsFeedID == item.feedID && m.subsLast != nil {
					subsLine = fmt.Sprintf("  %d subscribers · %d global", m.subsLast.Count, m.subsLast.GlobalCount)
				} else if m.subsLoading == item.feedID {
					subsLine = "  subscribers…"
				}
				sb.WriteString(dimStyle.Render(truncate(subsLine, width-2)) + "\n")
			} else {
				sb.WriteString("\n\n")
			}
		}
	}

	sb.WriteString(strings.Repeat("─", width) + "\n")

	if len(m.entries) == 0 {
		emptyMsg := "  No entries here."
		if m.searching || m.searchQuery != "" {
			emptyMsg = "  No results for your search."
		} else if len(m.items) > 0 && m.sidebarCursor < len(m.items) {
			switch m.items[m.sidebarCursor].kind {
			case sidebarUnread:
				emptyMsg = "  You're all caught up — nothing left to read."
			case sidebarStarred:
				emptyMsg = "  No starred articles yet."
			case sidebarAll:
				emptyMsg = "  Nothing here yet. Subscribe to feeds to get started."
			}
		}
		sb.WriteString("\n" + dimStyle.Render(emptyMsg) + "\n")
	} else {
		isFocus := m.focus == focusEntries && !m.searching

		// Fixed column widths (all in visible chars):
		//   prefix: " "(1) + dot(2) + star(2) = 5
		//   date:   "2006-01-02"(10) + " "(1) = 11
		//   feed:   up to 20 chars + " "(1) = 21
		//   title:  remainder
		const prefixLen = 5
		const dateLen = 10
		const feedLen = 20
		const gaps = 3 // spaces: after title, after feed, trailing
		titleWidth := width - prefixLen - dateLen - feedLen - gaps
		if titleWidth < 10 {
			titleWidth = 10
		}

		pageSize := m.entriesPageSize()
		start := m.entriesOffset
		end := start + pageSize
		if end > len(m.entries) {
			end = len(m.entries)
		}

		for idx := start; idx < end; idx++ {
			e := m.entries[idx]
			isSelected := idx == m.entriesCursor

			feedName := ""
			if f, ok := m.feedsByID[e.FeedId]; ok {
				feedName = f.Title
			}

			// buildPlain assembles a raw (no-ANSI) row of exact width.
			buildPlain := func(dotCh, starCh string) string {
				t := padRight(truncate(e.Title, titleWidth), titleWidth)
				f := padRight(truncate(feedName, feedLen), feedLen)
				d := e.PublishedAt.Format("2006-01-02")
				return padRight(" "+dotCh+starCh+t+" "+f+" "+d+" ", width)
			}

			dotPlain := "● "
			if e.Status == "read" {
				dotPlain = "· "
			}
			starPlain := "✦ "
			if !e.Starred {
				starPlain = "  "
			}

			var line string
			switch {
			case isSelected && isFocus:
				line = selectedFocusStyle.Render(buildPlain(dotPlain, starPlain))
			case isSelected:
				line = selectedStyle.Render(buildPlain(dotPlain, starPlain))
			case e.Status == "read":
				line = dimStyle.Render(buildPlain(dotPlain, starPlain))
			default:
				// unread: colorize dot and star inline, plain text otherwise
				dot := unreadDotStyle.Render("● ") // 2 visible cols
				star := "  "
				if e.Starred {
					star = starStyle.Render("✦ ")
				}
				t := padRight(truncate(e.Title, titleWidth), titleWidth)
				f := padRight(truncate(feedName, feedLen), feedLen)
				d := dimStyle.Render(e.PublishedAt.Format("2006-01-02"))
				line = " " + dot + star + t + " " + dimStyle.Render(f) + " " + d + " "
			}

			sb.WriteString(line + "\n")
		}
	}

	var helpText string
	onFeed := len(m.items) > 0 && m.sidebarCursor < len(m.items) && m.items[m.sidebarCursor].kind == sidebarFeed
	switch {
	case m.searching:
		helpText = "type to search · esc cancel · enter confirm · / from anywhere"
	case m.focus == focusSidebar:
		helpText = "j/k navigate · l open · / search · p preview · q quit"
	case onFeed:
		helpText = "j/k · enter/o open · u read/unread · s star · R refresh · M mark read · p preview · / search · h sidebar · q quit"
	default:
		helpText = "j/k · ^d/^u · g/G · enter/o open · u read/unread · s star · p preview · / search · h sidebar · q quit"
	}
	sb.WriteString("\n" + helpStyle.Render(helpText))

	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatPos(cur, total int) string {
	return fmt.Sprintf("%d/%d", cur, total)
}

// mainHeaderLines returns the number of main-panel header rows (excluding the
// separator) for the current selection: 1 for the title, plus 2 extra lines
// (subtitle + subscriber count) when a feed is selected.
func (m Model) mainHeaderLines() int {
	if len(m.items) > 0 && m.sidebarCursor < len(m.items) && m.items[m.sidebarCursor].kind == sidebarFeed {
		return 3
	}
	return 1
}

// entriesPageSize returns how many entry rows fit in the main panel.
func (m Model) entriesPageSize() int {
	n := m.height - m.mainHeaderLines() - 3 // header, sep, blank, help
	if n < 1 {
		n = 1
	}
	return n
}

// clampEntriesOffset adjusts entriesOffset so that entriesCursor stays visible.
func (m *Model) clampEntriesOffset() {
	pageSize := m.entriesPageSize()
	if m.entriesCursor < m.entriesOffset {
		m.entriesOffset = m.entriesCursor
	}
	if m.entriesCursor >= m.entriesOffset+pageSize {
		m.entriesOffset = m.entriesCursor - pageSize + 1
	}
	if m.entriesOffset < 0 {
		m.entriesOffset = 0
	}
}

// ---- preview / discovery ----------------------------------------------------

// previewItemCount returns the number of items in the current preview.
func (m Model) previewItemCount() int {
	if m.preview == nil || m.preview.Items == nil {
		return 0
	}
	return len(*m.preview.Items)
}

// selectedPreviewItem returns the preview item under the cursor, if any.
func (m Model) selectedPreviewItem() (sunred.PreviewFeedItem, bool) {
	if m.preview == nil || m.preview.Items == nil {
		return sunred.PreviewFeedItem{}, false
	}
	items := *m.preview.Items
	if m.previewCursor < 0 || m.previewCursor >= len(items) {
		return sunred.PreviewFeedItem{}, false
	}
	return items[m.previewCursor], true
}

// previewPageSize returns how many preview rows fit in the main panel.
// Layout: 3 header + 1 separator + N items + 1 blank + 1 help.
func (m Model) previewPageSize() int {
	n := m.height - 6
	if n < 1 {
		n = 1
	}
	return n
}

// clampPreviewOffset adjusts previewOffset so that previewCursor stays visible.
func (m *Model) clampPreviewOffset() {
	pageSize := m.previewPageSize()
	if m.previewCursor < m.previewOffset {
		m.previewOffset = m.previewCursor
	}
	if m.previewCursor >= m.previewOffset+pageSize {
		m.previewOffset = m.previewCursor - pageSize + 1
	}
	if m.previewOffset < 0 {
		m.previewOffset = 0
	}
}

// renderPreview renders the discovery preview view: a feed header (title,
// description/site URL, subscriber counts) followed by the preview items.
func (m Model) renderPreview(width int) string {
	var sb strings.Builder
	p := m.preview

	title := "Preview"
	if p != nil {
		title = p.Title
	}
	headerLine := headerStyle.Render(truncate(title, width-2))
	if p != nil && p.Items != nil && len(*p.Items) > 0 {
		headerLine += " " + dimStyle.Render(formatPos(m.previewCursor+1, len(*p.Items)))
	}
	sb.WriteString(headerLine + "\n")

	if p != nil {
		subtitle := p.SiteUrl
		if p.Description != nil && *p.Description != "" {
			subtitle = truncate(*p.Description, width-2)
		}
		sb.WriteString(dimStyle.Render("  "+truncate(subtitle, width-2)) + "\n")

		subsLine := ""
		if p.Subscribers != nil {
			subsLine = fmt.Sprintf("  %d subscribers · %d global", p.Subscribers.Count, p.Subscribers.GlobalCount)
		} else {
			subsLine = "  not subscribed on this instance"
		}
		sb.WriteString(dimStyle.Render(truncate(subsLine, width-2)) + "\n")
	} else {
		sb.WriteString("\n\n")
	}
	sb.WriteString(strings.Repeat("─", width) + "\n")

	if p == nil || p.Items == nil || len(*p.Items) == 0 {
		sb.WriteString("\n" + dimStyle.Render("  No preview items.") + "\n")
	} else {
		isFocus := m.focus == focusEntries

		const prefixLen = 5
		const dateLen = 10
		const gaps = 3
		titleWidth := width - prefixLen - dateLen - gaps
		if titleWidth < 10 {
			titleWidth = 10
		}

		pageSize := m.previewPageSize()
		start := m.previewOffset
		end := start + pageSize
		if end > len(*p.Items) {
			end = len(*p.Items)
		}

		for idx := start; idx < end; idx++ {
			it := (*p.Items)[idx]
			isSelected := idx == m.previewCursor

			read := it.Status != nil && *it.Status == "read"
			starred := it.Starred != nil && *it.Starred

			buildPlain := func(dotCh, starCh string) string {
				t := padRight(truncate(it.Title, titleWidth), titleWidth)
				d := it.PublishedAt.Format("2006-01-02")
				return padRight(" "+dotCh+starCh+t+" "+d+" ", width)
			}

			dotPlain := "● "
			if read {
				dotPlain = "· "
			}
			starPlain := "✦ "
			if !starred {
				starPlain = "  "
			}

			var line string
			switch {
			case isSelected && isFocus:
				line = selectedFocusStyle.Render(buildPlain(dotPlain, starPlain))
			case isSelected:
				line = selectedStyle.Render(buildPlain(dotPlain, starPlain))
			case read:
				line = dimStyle.Render(buildPlain(dotPlain, starPlain))
			default:
				dot := unreadDotStyle.Render("● ")
				star := "  "
				if starred {
					star = starStyle.Render("✦ ")
				}
				t := padRight(truncate(it.Title, titleWidth), titleWidth)
				d := dimStyle.Render(it.PublishedAt.Format("2006-01-02"))
				line = " " + dot + star + t + " " + d + " "
			}
			sb.WriteString(line + "\n")
		}
	}

	helpText := "j/k · enter/o open+read · u read/unread · s star · esc back · q quit"
	sb.WriteString("\n" + helpStyle.Render(helpText))
	return sb.String()
}
