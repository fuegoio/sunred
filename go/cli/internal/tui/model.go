// Package tui implements the sunred TUI — an interactive feed browser.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// focus tracks which panel has keyboard focus.
type focus int

const (
	focusSidebar focus = iota
	focusEntries
)

// sidebarItemKind identifies a navigation destination in the sidebar.
type sidebarItemKind int

const (
	sidebarAll sidebarItemKind = iota
	sidebarUnread
	sidebarStarred
	sidebarFeed
	sidebarFolder
	sidebarSearch
)

type sidebarItem struct {
	kind     sidebarItemKind
	label    string
	feedID   int64
	folderID int64
	depth    int
}

// Model holds the TUI state.
type Model struct {
	client  *sunred.ClientWithResponses
	focus   focus
	width   int
	height  int
	loading bool
	err     string

	// sidebar data
	feeds         []sunred.Feed
	folders       []sunred.Folder
	feedsByID     map[int64]sunred.Feed
	items         []sidebarItem
	sidebarCursor int

	// search
	searching   bool
	searchQuery string

	// entries panel
	entries       []sunred.Entry
	entriesCursor int
	entriesOffset int // index of first visible entry (scroll offset)

	// feed header: lazy subscriber counts for the selected feed
	subsCache   map[int64]sunred.FeedSubscribersResponse
	subsLoading int64 // feed ID currently being fetched, or 0
	subsFeedID  int64 // feed ID the cache entry below belongs to (0 = none)
	subsLast    *sunred.FeedSubscribersResponse

	// feed get view (fetch a feed by URL, subscribed or not)
	feedGet       *sunred.PreviewFeedBody
	feedGetCursor int
	feedGetOffset int
	enteringURL   bool
	urlInput      string
}

// ---- styles ----------------------------------------------------------------

const sidebarWidth = 28

// sunColor is the brand primary: oklch(0.8 0.19 39) ≈ #ff8b59 (amber/orange).
const sunColor = "#ff8b59"

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(sunColor)).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true)

	selectedFocusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(sunColor)).
				Bold(true)

	normalStyle = lipgloss.NewStyle()

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	starStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("220"))

	unreadDotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(sunColor))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(0, 1)

	folderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true)

	sectionLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Bold(true)

	searchInputStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255"))

	searchCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(sunColor)).
				Bold(true)
)

// NewModel creates a new TUI model with the given client.
func NewModel(client *sunred.ClientWithResponses) Model {
	return Model{
		client:        client,
		focus:         focusEntries,
		sidebarCursor: 0, // start on Unread (first tab, matching the web default view)
		subsCache:     make(map[int64]sunred.FeedSubscribersResponse),
	}
}

// Init sends the initial command to load sidebar data.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadFeeds(m.client), loadFolders(m.client))
}

// ---- messages --------------------------------------------------------------

type loadFeedsMsg struct {
	feeds []sunred.Feed
	err   error
}

type loadFoldersMsg struct {
	folders []sunred.Folder
	err     error
}

type loadEntriesMsg struct {
	entries []sunred.Entry
	err     error
}

type markReadMsg struct {
	entryID int64
	status  sunred.UpdateEntriesRequestStatus
	err     error
}

type toggleStarMsg struct {
	entryID int64
	starred bool
	err     error
}

type feedSubsMsg struct {
	feedID int64
	subs   *sunred.FeedSubscribersResponse
	err    error
}

type feedRefreshedMsg struct {
	feedID int64
	err    error
}

type feedMarkedReadMsg struct {
	feedID int64
	err    error
}

type feedGetMsg struct {
	feedGet *sunred.PreviewFeedBody
	err     error
}

// byUrlStatusMsg reports the result of a by-URL status update on a feed-get item.
type byUrlStatusMsg struct {
	articleURL string
	status     sunred.UpdateEntryStatusByUrlRequestStatus
	err        error
}

// byUrlStarMsg reports the result of a by-URL star toggle on a feed-get item.
type byUrlStarMsg struct {
	articleURL string
	starred    bool
	err        error
}

// ---- commands --------------------------------------------------------------

func loadFeeds(client *sunred.ClientWithResponses) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListFeedsWithResponse(context.Background())
		if err != nil {
			return loadFeedsMsg{err: err}
		}
		if resp.JSON200 == nil {
			return loadFeedsMsg{err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return loadFeedsMsg{feeds: *resp.JSON200}
	}
}

func loadFolders(client *sunred.ClientWithResponses) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListFoldersWithResponse(context.Background())
		if err != nil {
			return loadFoldersMsg{err: err}
		}
		if resp.JSON200 == nil {
			return loadFoldersMsg{err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return loadFoldersMsg{folders: *resp.JSON200}
	}
}

func loadEntriesByParams(client *sunred.ClientWithResponses, params *sunred.ListEntriesParams) tea.Cmd {
	return func() tea.Msg {
		if params.Limit == nil {
			params.Limit = ptr(int64(200))
		}
		resp, err := client.ListEntriesWithResponse(context.Background(), params)
		if err != nil {
			return loadEntriesMsg{err: err}
		}
		if resp.JSON200 == nil {
			return loadEntriesMsg{err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return loadEntriesMsg{entries: *resp.JSON200}
	}
}

func searchEntries(client *sunred.ClientWithResponses, query string) tea.Cmd {
	return loadEntriesByParams(client, &sunred.ListEntriesParams{
		Search: ptr(query),
	})
}

func setEntryStatus(client *sunred.ClientWithResponses, entryID int64, status sunred.UpdateEntriesRequestStatus) tea.Cmd {
	return func() tea.Msg {
		ids := []int64{entryID}
		_, err := client.UpdateEntriesWithResponse(context.Background(), sunred.UpdateEntriesRequest{
			EntryIds: &ids,
			Status:   status,
		})
		return markReadMsg{entryID: entryID, status: status, err: err}
	}
}

func toggleStar(client *sunred.ClientWithResponses, entryID int64, starred bool) tea.Cmd {
	return func() tea.Msg {
		_, err := client.ToggleEntryStarredWithResponse(context.Background(), entryID, sunred.ToggleEntryStarredRequest{
			Starred: starred,
		})
		return toggleStarMsg{entryID: entryID, starred: starred, err: err}
	}
}

func fetchFeedSubs(client *sunred.ClientWithResponses, feedID int64) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.FeedSubscribersWithResponse(context.Background(), feedID)
		if err != nil {
			return feedSubsMsg{feedID: feedID, err: err}
		}
		if resp.JSON200 == nil {
			return feedSubsMsg{feedID: feedID, err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return feedSubsMsg{feedID: feedID, subs: resp.JSON200}
	}
}

func refreshFeed(client *sunred.ClientWithResponses, feedID int64) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.RefreshFeedWithResponse(context.Background(), feedID)
		if err != nil {
			return feedRefreshedMsg{feedID: feedID, err: err}
		}
		if resp.StatusCode() != 204 {
			return feedRefreshedMsg{feedID: feedID, err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return feedRefreshedMsg{feedID: feedID}
	}
}

func markFeedRead(client *sunred.ClientWithResponses, feedID int64) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.MarkFeedReadWithResponse(context.Background(), feedID)
		if err != nil {
			return feedMarkedReadMsg{feedID: feedID, err: err}
		}
		if resp.StatusCode() != 204 {
			return feedMarkedReadMsg{feedID: feedID, err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return feedMarkedReadMsg{feedID: feedID}
	}
}

func getFeed(client *sunred.ClientWithResponses, feedURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.PreviewFeedWithResponse(context.Background(), sunred.PreviewFeedJSONRequestBody{
			FeedUrl: feedURL,
		})
		if err != nil {
			return feedGetMsg{err: err}
		}
		if resp.JSON200 == nil {
			return feedGetMsg{err: fmt.Errorf("API error (status %d)", resp.StatusCode())}
		}
		return feedGetMsg{feedGet: resp.JSON200}
	}
}

// setEntryStatusByUrl marks a feed-get article read/unread by URL.
func setEntryStatusByUrl(client *sunred.ClientWithResponses, articleURL string, status sunred.UpdateEntryStatusByUrlRequestStatus) tea.Cmd {
	return func() tea.Msg {
		_, err := client.UpdateEntryStatusByUrlWithResponse(context.Background(), sunred.UpdateEntryStatusByUrlRequest{
			ArticleUrl: articleURL,
			Status:     status,
		})
		return byUrlStatusMsg{articleURL: articleURL, status: status, err: err}
	}
}

// toggleStarByUrl toggles a feed-get article's star by URL. The item supplies
// the metadata the server needs to record the article.
func toggleStarByUrl(client *sunred.ClientWithResponses, item sunred.PreviewFeedItem, feed *sunred.PreviewFeedBody, starred bool) tea.Cmd {
	return func() tea.Msg {
		body := sunred.ToggleEntryStarredByUrlRequest{
			ArticleUrl:  item.Url,
			Title:       item.Title,
			Starred:     starred,
			Author:      item.Author,
			Description: item.Description,
		}
		if feed != nil {
			body.FeedUrl = ptr(feed.FeedUrl)
			body.FeedTitle = ptr(feed.Title)
			body.FeedSiteUrl = ptr(feed.SiteUrl)
		}
		body.PublishedAt = ptr(item.PublishedAt)
		_, err := client.ToggleEntryStarredByUrlWithResponse(context.Background(), body)
		return byUrlStarMsg{articleURL: item.Url, starred: starred, err: err}
	}
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}

// ---- sidebar helpers -------------------------------------------------------

// rebuildSidebar constructs the flat sidebar item list from feeds and folders.
func (m *Model) rebuildSidebar() {
	items := []sidebarItem{
		{kind: sidebarUnread, label: "Unread"},
		{kind: sidebarAll, label: "All"},
		{kind: sidebarStarred, label: "Starred"},
	}

	// feeds without a folder (depth 0)
	for _, f := range m.feeds {
		if f.FolderId == nil {
			items = append(items, sidebarItem{kind: sidebarFeed, label: f.Title, feedID: f.Id})
		}
	}

	// folders with their feeds nested (depth 1)
	for _, folder := range m.folders {
		items = append(items, sidebarItem{kind: sidebarFolder, label: folder.Title, folderID: folder.Id})
		for _, f := range m.feeds {
			if f.FolderId != nil && *f.FolderId == folder.Id {
				items = append(items, sidebarItem{kind: sidebarFeed, label: f.Title, feedID: f.Id, depth: 1})
			}
		}
	}

	m.items = items

	// rebuild lookup map
	m.feedsByID = make(map[int64]sunred.Feed, len(m.feeds))
	for _, f := range m.feeds {
		m.feedsByID[f.Id] = f
	}
}

// entriesParamsForItem returns API params matching the selected sidebar item.
func entriesParamsForItem(item sidebarItem) *sunred.ListEntriesParams {
	switch item.kind {
	case sidebarAll:
		return &sunred.ListEntriesParams{}
	case sidebarUnread:
		s := sunred.ListEntriesParamsStatusUnread
		return &sunred.ListEntriesParams{Status: &s}
	case sidebarStarred:
		return &sunred.ListEntriesParams{Starred: ptr(true)}
	case sidebarFeed:
		return &sunred.ListEntriesParams{FeedId: &item.feedID}
	case sidebarFolder:
		return &sunred.ListEntriesParams{FolderId: &item.folderID}
	}
	return &sunred.ListEntriesParams{}
}

// loadSelection returns the commands to load the main panel for the given
// sidebar item: entries, plus subscriber counts when the item is a feed.
func (m *Model) loadSelection(item sidebarItem) tea.Cmd {
	cmds := []tea.Cmd{loadEntriesByParams(m.client, entriesParamsForItem(item))}
	if item.kind == sidebarFeed {
		m.subsFeedID = item.feedID
		if cached, ok := m.subsCache[item.feedID]; ok {
			c := cached
			m.subsLast = &c
		} else {
			m.subsLast = nil
			m.subsLoading = item.feedID
			cmds = append(cmds, fetchFeedSubs(m.client, item.feedID))
		}
	} else {
		m.subsFeedID = 0
		m.subsLast = nil
	}
	return tea.Batch(cmds...)
}

// ---- utility ---------------------------------------------------------------

func ptr[T any](v T) *T { return &v }

// truncate cuts s to at most n visible runes, appending "…" when cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// padRight right-pads s to w visible runes.
func padRight(s string, w int) string {
	need := w - utf8.RuneCountInString(s)
	if need <= 0 {
		return s
	}
	return s + strings.Repeat(" ", need)
}
