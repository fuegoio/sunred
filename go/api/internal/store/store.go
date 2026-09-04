// Package store wraps *sql.DB with domain-specific query helpers for the
// Sunred RSS reader schema.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"

	"github.com/fuegoio/sunred/go/api/internal/urlnorm"
)

// ErrFeedNotFound is returned when a feed lookup returns no row.
var ErrFeedNotFound = fmt.Errorf("feed not found")

// ErrEntryNotFound is returned when an entry lookup returns no row.
var ErrEntryNotFound = fmt.Errorf("entry not found")

// --- Folders ---

// CreateFolder inserts a new folder for the given user.
func (s *Store) CreateFolder(ctx context.Context, userID int, title string, parentID *int) (*Folder, error) {
	var f Folder
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO folders (user_id, title, parent_id)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, parent_id, title, sort_order, created_at, updated_at`,
		userID, title, parentID,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Title, &f.SortOrder, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create folder: %w", err)
	}
	return &f, nil
}

// ListFolders returns all folders for the given user, ordered by sort_order then title.
func (s *Store) ListFolders(ctx context.Context, userID int) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, parent_id, title, sort_order, created_at, updated_at
		 FROM folders WHERE user_id = $1 ORDER BY sort_order ASC, title ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Title, &f.SortOrder, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// GetFolderByID returns the folder with the given id scoped to userID, or nil.
func (s *Store) GetFolderByID(ctx context.Context, id, userID int) (*Folder, error) {
	var f Folder
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, parent_id, title, sort_order, created_at, updated_at
		 FROM folders WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Title, &f.SortOrder, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateFolder updates the title and/or parent_id of a folder. parentID can be
// nil to move the folder to the root, or point to another folder to nest it.
func (s *Store) UpdateFolder(ctx context.Context, id, userID int, title string, parentID *int) (*Folder, error) {
	var f Folder
	err := s.DB.QueryRowContext(ctx,
		`UPDATE folders SET title = $3, parent_id = $4, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2
		 RETURNING id, user_id, parent_id, title, sort_order, created_at, updated_at`,
		id, userID, title, parentID,
	).Scan(&f.ID, &f.UserID, &f.ParentID, &f.Title, &f.SortOrder, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update folder: %w", err)
	}
	return &f, nil
}

// DeleteFolder removes a folder. Feeds in the folder are re-assigned to
// NULL folder_id via ON DELETE SET NULL. Child folders are also re-assigned
// to NULL parent_id via ON DELETE SET NULL.
func (s *Store) DeleteFolder(ctx context.Context, id, userID int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM folders WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- Feeds ---

// feedColumns is the canonical column list for the global feeds table, in the
// order scanFeedGlobal expects.
const feedColumns = `id, feed_url, site_url, title, description,
	etag_header, last_modified_header, parsing_error, parsing_error_count,
	disabled, scraper_rules, rewrite_rules, crawler,
	next_check_at, last_fetch_at, created_at, updated_at`

func scanFeedGlobalRow(row *sql.Row, f *Feed) error {
	return row.Scan(&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
		&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
		&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
		&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt)
}

func scanFeedGlobal(rows *sql.Rows, f *Feed) error {
	return rows.Scan(&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
		&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
		&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
		&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt)
}

// GetOrCreateFeed returns the global feed for feedURL, creating it (with the
// provided metadata) if missing. Existing metadata is upgraded in place when
// the caller supplies non-empty values. Idempotent on feed_url.
func (s *Store) GetOrCreateFeed(ctx context.Context, feedURL, siteURL, title, description string) (*Feed, error) {
	feedURL = urlnorm.URL(feedURL)
	var f Feed
	row := s.DB.QueryRowContext(ctx,
		`INSERT INTO feeds (feed_url, site_url, title, description)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (feed_url) DO UPDATE
		   SET site_url    = COALESCE(NULLIF(EXCLUDED.site_url, ''), feeds.site_url),
		       title       = COALESCE(NULLIF(EXCLUDED.title, ''), feeds.title),
		       description = COALESCE(NULLIF(EXCLUDED.description, ''), feeds.description),
		       updated_at  = NOW()
		 RETURNING `+feedColumns,
		feedURL, siteURL, title, description,
	)
	if err := scanFeedGlobalRow(row, &f); err != nil {
		return nil, fmt.Errorf("get or create feed: %w", err)
	}
	return &f, nil
}

// GetFeedGlobal returns the global feed with the given id, or nil.
func (s *Store) GetFeedGlobal(ctx context.Context, id int) (*Feed, error) {
	var f Feed
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM feeds WHERE id = $1`, id)
	err := scanFeedGlobalRow(row, &f)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFeedByURL returns the global feed matching feedURL, or nil.
func (s *Store) GetFeedByURL(ctx context.Context, feedURL string) (*Feed, error) {
	feedURL = urlnorm.URL(feedURL)
	var f Feed
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM feeds WHERE feed_url = $1`, feedURL)
	err := scanFeedGlobalRow(row, &f)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFeeds returns the feeds the given user subscribes to, joined to their
// subscription (folder + title override). Ordered by subscription sort then title.
func (s *Store) ListFeeds(ctx context.Context, userID int) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.id, f.feed_url, f.site_url, COALESCE(s.title_override, f.title), f.description,
		        s.folder_id,
		        f.etag_header, f.last_modified_header, f.parsing_error, f.parsing_error_count,
		        f.disabled, f.scraper_rules, f.rewrite_rules, f.crawler,
		        f.next_check_at, f.last_fetch_at, f.created_at, f.updated_at
		 FROM feeds f
		 JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = $1
		 ORDER BY s.sort_order ASC, COALESCE(s.title_override, f.title) ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
			&f.FolderID,
			&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
			&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
			&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// GetSubscriptionFeed returns the feed with the given id if the user subscribes
// to it, joined to their subscription (folder + title override), or nil.
func (s *Store) GetSubscriptionFeed(ctx context.Context, id, userID int) (*Feed, error) {
	var f Feed
	err := s.DB.QueryRowContext(ctx,
		`SELECT f.id, f.feed_url, f.site_url, COALESCE(s.title_override, f.title), f.description,
		        s.folder_id,
		        f.etag_header, f.last_modified_header, f.parsing_error, f.parsing_error_count,
		        f.disabled, f.scraper_rules, f.rewrite_rules, f.crawler,
		        f.next_check_at, f.last_fetch_at, f.created_at, f.updated_at
		 FROM feeds f
		 JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = $2
		 WHERE f.id = $1`, id, userID,
	).Scan(&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
		&f.FolderID,
		&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
		&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
		&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateSubscription links a user to a global feed. Idempotent on (user_id,
// feed_id); returns the joined feed view. folderID and titleOverride are
// applied only when creating a new subscription.
func (s *Store) CreateSubscription(ctx context.Context, userID, feedID int, folderID *int, titleOverride string) (*Feed, error) {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, feed_id, folder_id, title_override)
		 VALUES ($1, $2, $3, NULLIF($4, ''))
		 ON CONFLICT (user_id, feed_id) DO NOTHING`,
		userID, feedID, folderID, titleOverride)
	if err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return s.GetSubscriptionFeed(ctx, feedID, userID)
}

// UpdateSubscription updates the folder and/or title override on a user's
// subscription. Returns the joined feed view, or nil if not subscribed.
func (s *Store) UpdateSubscription(ctx context.Context, userID, feedID int, folderID *int, title string) (*Feed, error) {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE subscriptions SET folder_id = $3, title_override = NULLIF($4, ''), updated_at = NOW()
		 WHERE user_id = $1 AND feed_id = $2`,
		userID, feedID, folderID, title)
	if err != nil {
		return nil, fmt.Errorf("update subscription: %w", err)
	}
	return s.GetSubscriptionFeed(ctx, feedID, userID)
}

// DeleteSubscription removes a user's subscription. The global feed survives.
func (s *Store) DeleteSubscription(ctx context.Context, userID, feedID int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id = $1 AND feed_id = $2`, userID, feedID)
	return err
}

// UpdateFeedFetchState stores the ETag/Last-Modified headers and sets
// last_fetch_at + next_check_at after a refresh. If description is non-empty,
// it also updates the feed's description from the freshly parsed feed.
func (s *Store) UpdateFeedFetchState(ctx context.Context, feedID int, etag, lastModified string, parsingError string, errorCount int, nextCheckAt time.Time, description string) error {
	if description != "" {
		_, err := s.DB.ExecContext(ctx,
			`UPDATE feeds SET etag_header = $2, last_modified_header = $3,
			         parsing_error = $4, parsing_error_count = $5,
			         last_fetch_at = NOW(), next_check_at = $6, updated_at = NOW(),
			         description = $7
			 WHERE id = $1`,
			feedID, etag, lastModified, parsingError, errorCount, nextCheckAt, description)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE feeds SET etag_header = $2, last_modified_header = $3,
		         parsing_error = $4, parsing_error_count = $5,
		         last_fetch_at = NOW(), next_check_at = $6, updated_at = NOW()
		 WHERE id = $1`,
		feedID, etag, lastModified, parsingError, errorCount, nextCheckAt)
	return err
}

// ListFeedsDueForRefresh returns up to limit global feeds whose next_check_at <= now.
func (s *Store) ListFeedsDueForRefresh(ctx context.Context, limit int) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+feedColumns+`
		 FROM feeds WHERE (next_check_at IS NULL OR next_check_at <= NOW()) AND disabled = false
		 ORDER BY next_check_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var feeds []Feed
	for rows.Next() {
		var f Feed
		if err := scanFeedGlobal(rows, &f); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// --- Entries ---

// CreateEntry inserts a global entry (one per feed+hash), returning its id.
// Returns 0 (no error) when the hash already existed for this feed.
func (s *Store) CreateEntry(ctx context.Context, feedID int, hash, title, url, commentsURL, author, content, description string, publishedAt time.Time, tags []string) (int64, error) {
	url = urlnorm.URL(url)
	var id int64
	if tags == nil {
		tags = []string{}
	}
	tagArr := pq.Array(tags)
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO entries (feed_id, hash, title, url, comments_url, author, content, description, published_at, tags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (feed_id, hash) DO NOTHING
		 RETURNING id`,
		feedID, hash, title, url, commentsURL, author, content, description, publishedAt, tagArr,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("create entry: %w", err)
	}
	return id, nil
}

// visibleEntryFilter returns a SQL fragment matching entry IDs the user is
// allowed to see (subscribed feeds or shared by followed users), used to scope
// state mutations. userID is inlined as an integer literal.
func visibleEntryFilter(userID int) string {
	uid := fmt.Sprintf("%d", userID)
	return `(e.feed_id IN (SELECT feed_id FROM subscriptions WHERE user_id = ` + uid + `)
	   OR e.id IN (SELECT sa.entry_id FROM shared_articles sa JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = ` + uid + `))`
}

// ListEntries returns entries visible to the user — entries from feeds they
// subscribe to, UNION entries shared by users they follow — with per-user
// read state from entry_read_status and star state from entry_stars, both
// joined by article URL. Filters: feed, folder (subscription folder), status,
// starred, full-text search, source (feeds = own subscriptions only,
// follows = shared by followed users only). Paginated.
func (s *Store) ListEntries(ctx context.Context, userID int, feedID *int, folderID *int, status string, starred *bool, search string, source string, limit, offset int) ([]Entry, error) {
	q := `SELECT e.id, e.feed_id, e.hash, e.title, e.url, e.comments_url,
	             e.author, '' AS content, LEFT(e.description, 400) AS description,
	             COALESCE(rs.status, 'read'), (es.article_url IS NOT NULL),
	             e.published_at, GREATEST(COALESCE(rs.changed_at, e.created_at), COALESCE(es.starred_at, e.created_at)), e.tags,
	             f.id, f.feed_url, f.site_url, COALESCE(s.title_override, f.title), f.description,
	             f.etag_header, f.last_modified_header, f.parsing_error, f.parsing_error_count,
	             f.disabled, f.scraper_rules, f.rewrite_rules, f.crawler,
	             f.next_check_at, f.last_fetch_at, f.created_at, f.updated_at,
	             COALESCE(sh.handle, ''), COALESCE(sh.display_name, ''),
	             my_sa.id
	      FROM entries e
	      JOIN feeds f ON f.id = e.feed_id
	      LEFT JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = $1
	      LEFT JOIN entry_read_status rs ON rs.user_id = $1 AND rs.article_url = e.url
	      LEFT JOIN entry_stars es ON es.user_id = $1 AND es.article_url = e.url
	      LEFT JOIN LATERAL (
	        SELECT sa.user_id, sa.entry_id FROM shared_articles sa
	        JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = $1
	        WHERE sa.entry_id = e.id
	        LIMIT 1
	      ) sh_row ON true
	      LEFT JOIN users sh ON sh.id = sh_row.user_id
	      LEFT JOIN shared_articles my_sa ON my_sa.user_id = $1 AND my_sa.article_url = e.url`
	args := []interface{}{userID}
	argIdx := 2

	// The visibility scope is the union of own subscriptions and articles
	// shared by followed users. The `source` filter narrows it to one branch,
	// excluding the overlap so the two views partition the timeline:
	//   feeds   → own subscriptions, not shared by follows
	//   follows → shared by follows, not in own subscriptions
	switch source {
	case "feeds":
		q += " WHERE e.feed_id IN (SELECT feed_id FROM subscriptions WHERE user_id = $1)"
		q += " AND e.id NOT IN (SELECT sa.entry_id FROM shared_articles sa JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = $1)"
	case "follows":
		q += " WHERE e.id IN (SELECT sa.entry_id FROM shared_articles sa JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = $1)"
		q += " AND e.feed_id NOT IN (SELECT feed_id FROM subscriptions WHERE user_id = $1)"
	default:
		q += " WHERE (e.feed_id IN (SELECT feed_id FROM subscriptions WHERE user_id = $1)"
		q += " OR e.id IN (SELECT sa.entry_id FROM shared_articles sa JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = $1))"
	}

	if folderID != nil {
		q += fmt.Sprintf(" AND e.feed_id IN (SELECT s.feed_id FROM subscriptions s WHERE s.user_id = $1 AND s.folder_id IN (WITH RECURSIVE folder_tree AS (SELECT id FROM folders WHERE id = $%d AND user_id = $1 UNION ALL SELECT child.id FROM folders child JOIN folder_tree ft ON child.parent_id = ft.id) SELECT id FROM folder_tree))", argIdx)
		args = append(args, *folderID)
		argIdx++
	}
	if feedID != nil {
		q += fmt.Sprintf(" AND e.feed_id = $%d", argIdx)
		args = append(args, *feedID)
		argIdx++
	}
	if status != "" {
		q += fmt.Sprintf(" AND COALESCE(rs.status, 'read') = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if starred != nil {
		q += fmt.Sprintf(" AND (es.article_url IS NOT NULL) = $%d", argIdx)
		args = append(args, *starred)
		argIdx++
	}
	if search != "" {
		q += fmt.Sprintf(" AND e.document @@ plainto_tsquery($%d)", argIdx)
		args = append(args, search)
		argIdx++
	}

	q += fmt.Sprintf(" ORDER BY e.published_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var entries []Entry
	for rows.Next() {
		var e Entry
		var f Feed
		if err := rows.Scan(&e.ID, &e.FeedID, &e.Hash, &e.Title, &e.URL, &e.CommentsURL,
			&e.Author, &e.Content, &e.Description, &e.Status, &e.Starred,
			&e.PublishedAt, &e.ChangedAt, pq.Array(&e.Tags),
			&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
			&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
			&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
			&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt,
			&e.SharedBy, &e.SharedByName, &e.ShareID); err != nil {
			return nil, err
		}
		e.Feed = &f
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetEntryByID returns a visible entry with per-user state + feed/sharer info,
// or nil if not visible to the user.
func (s *Store) GetEntryByID(ctx context.Context, id int64, userID int) (*Entry, error) {
	var e Entry
	var f Feed
	err := s.DB.QueryRowContext(ctx,
		`SELECT e.id, e.feed_id, e.hash, e.title, e.url, e.comments_url,
		        e.author, '' AS content, LEFT(e.description, 400) AS description,
		        COALESCE(rs.status, 'read'), (es.article_url IS NOT NULL),
		        e.published_at, GREATEST(COALESCE(rs.changed_at, e.created_at), COALESCE(es.starred_at, e.created_at)), e.tags,
		        f.id, f.feed_url, f.site_url, COALESCE(s.title_override, f.title), f.description,
		        f.etag_header, f.last_modified_header, f.parsing_error, f.parsing_error_count,
		        f.disabled, f.scraper_rules, f.rewrite_rules, f.crawler,
		        f.next_check_at, f.last_fetch_at, f.created_at, f.updated_at,
		        COALESCE(sh.handle, ''), COALESCE(sh.display_name, ''),
		        my_sa.id
		 FROM entries e
		 JOIN feeds f ON f.id = e.feed_id
		 LEFT JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = $2
		 LEFT JOIN entry_read_status rs ON rs.user_id = $2 AND rs.article_url = e.url
		 LEFT JOIN entry_stars es ON es.user_id = $2 AND es.article_url = e.url
		 LEFT JOIN LATERAL (
		   SELECT sa.user_id FROM shared_articles sa
		   JOIN user_follows uf ON uf.followee_id = sa.user_id AND uf.follower_id = $2
		   WHERE sa.entry_id = e.id LIMIT 1
		 ) sh_row ON true
		 LEFT JOIN users sh ON sh.id = sh_row.user_id
		 LEFT JOIN shared_articles my_sa ON my_sa.user_id = $2 AND my_sa.article_url = e.url
		 WHERE e.id = $1 AND (`+visibleEntryFilter(userID)+`)`, id, userID,
	).Scan(&e.ID, &e.FeedID, &e.Hash, &e.Title, &e.URL, &e.CommentsURL,
		&e.Author, &e.Content, &e.Description, &e.Status, &e.Starred,
		&e.PublishedAt, &e.ChangedAt, pq.Array(&e.Tags),
		&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
		&f.EtagHeader, &f.LastModified, &f.ParsingError, &f.ParsingErrorCount,
		&f.Disabled, &f.ScraperRules, &f.RewriteRules, &f.Crawler,
		&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt,
		&e.SharedBy, &e.SharedByName, &e.ShareID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Feed = &f
	return &e, nil
}

// EntryStateByURL holds the read status and starred flag for a single
// article URL, used to populate preview items with the user's existing state.
type EntryStateByURL struct {
	Status  string
	Starred bool
}

// GetEntryStatesByURLs returns the user's read status and starred state for
// each article URL, keyed by the input URL. Absent means read + unstarred.
// Used by the preview endpoint to show the user's existing state for preview
// items. The lookup is performed on normalized URLs (so state set via one
// source variant is visible for a textually-different variant), but the
// returned map preserves the original input keys the caller passed.
func (s *Store) GetEntryStatesByURLs(ctx context.Context, userID int, urls []string) (map[string]EntryStateByURL, error) {
	if len(urls) == 0 {
		return map[string]EntryStateByURL{}, nil
	}
	normalized := make([]string, len(urls))
	for i, u := range urls {
		normalized[i] = urlnorm.URL(u)
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.article_url, COALESCE(rs.status, 'read'), (es.article_url IS NOT NULL)
		FROM (SELECT unnest($2::text[]) AS article_url) u
		LEFT JOIN entry_read_status rs ON rs.user_id = $1 AND rs.article_url = u.article_url
		LEFT JOIN entry_stars es ON es.user_id = $1 AND es.article_url = u.article_url`,
		userID, pq.Array(normalized))
	if err != nil {
		return nil, fmt.Errorf("get entry states by url: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byNorm := make(map[string]EntryStateByURL, len(urls))
	for rows.Next() {
		var u, status string
		var starred bool
		if err := rows.Scan(&u, &status, &starred); err != nil {
			return nil, err
		}
		byNorm[u] = EntryStateByURL{Status: status, Starred: starred}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Re-key by the original input URLs the caller passed.
	out := make(map[string]EntryStateByURL, len(urls))
	for i, raw := range urls {
		out[raw] = byNorm[normalized[i]]
	}
	return out, nil
}

// UpdateEntryStatus sets the status of a set of visible entries for the user
// via upsert into entry_read_status, keyed by (user_id, article_url). Storing
// an explicit 'read' row (rather than relying on the absence default) is a
// stronger signal that the user has seen the article.
func (s *Store) UpdateEntryStatus(ctx context.Context, entryIDs []int64, userID int, status string) error {
	if len(entryIDs) == 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT DISTINCT ON (e.url) $2, e.url, e.id, $3, NOW()
		 FROM entries e
		 WHERE e.id = ANY($1) AND (`+visibleEntryFilter(userID)+`)
		 ORDER BY e.url
		 ON CONFLICT (user_id, article_url) DO UPDATE SET status = EXCLUDED.status, changed_at = NOW()`,
		pq.Array(entryIDs), userID, status)
	return err
}

// ToggleEntryStarred stars or unstars an entry for the user. Star = upsert into
// entry_stars (carrying article metadata from the entry+feed join); unstar =
// delete the row. Keyed by (user_id, article_url) so state survives entry
// deletion and can exist before materialization.
func (s *Store) ToggleEntryStarred(ctx context.Context, id int64, userID int, starred bool) error {
	if starred {
		_, err := s.DB.ExecContext(ctx,
			`INSERT INTO entry_stars
			   (user_id, article_url, entry_id, title, description,
			    feed_url, feed_title, feed_site_url, author, published_at, starred_at)
			 SELECT $2, e.url, e.id, e.title, e.description,
			        f.feed_url, f.title, f.site_url, e.author, e.published_at, NOW()
			 FROM entries e
			 JOIN feeds f ON f.id = e.feed_id
			 WHERE e.id = $1 AND (`+visibleEntryFilter(userID)+`)
			 ON CONFLICT (user_id, article_url) DO UPDATE
			   SET entry_id  = COALESCE(entry_stars.entry_id, EXCLUDED.entry_id),
			       starred_at = NOW()`,
			id, userID)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM entry_stars
		 WHERE user_id = $2
		   AND article_url = (SELECT e.url FROM entries e WHERE e.id = $1)`,
		id, userID)
	return err
}

// ToggleEntryStarredByURL stars or unstars an article by URL, without requiring
// a materialized entry. On star, the article metadata is stored directly; if the
// entry already exists, entry_id is linked. On unstar, the star row is deleted
// by (user_id, article_url). Used by the URL-based star endpoint for preview
// and shared articles that may not have a local entry yet.
func (s *Store) ToggleEntryStarredByURL(ctx context.Context, userID int,
	articleURL, title, description, feedURL, feedTitle, feedSiteURL, author string,
	publishedAt *time.Time, starred bool,
) error {
	articleURL = urlnorm.URL(articleURL)
	feedURL = urlnorm.URL(feedURL)
	if starred {
		entryID := s.ensureSharedEntry(ctx, articleURL, title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt)
		_, err := s.DB.ExecContext(ctx,
			`INSERT INTO entry_stars
			   (user_id, article_url, entry_id, title, description,
			    feed_url, feed_title, feed_site_url, author, published_at, starred_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			 ON CONFLICT (user_id, article_url) DO UPDATE
			   SET title        = EXCLUDED.title,
			       description  = EXCLUDED.description,
			       feed_url     = EXCLUDED.feed_url,
			       feed_title   = EXCLUDED.feed_title,
			       feed_site_url= EXCLUDED.feed_site_url,
			       author       = EXCLUDED.author,
			       published_at = EXCLUDED.published_at,
			       entry_id     = COALESCE(entry_stars.entry_id, EXCLUDED.entry_id),
			       starred_at   = NOW()`,
			userID, articleURL, sql.NullInt64{Int64: entryID, Valid: entryID > 0},
			title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt, time.Now())
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM entry_stars WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL)
	return err
}

// UpdateEntryStatusByURL sets the read status of an article by URL, without
// requiring a materialized entry. If the entry exists, entry_id is linked
// (picks the first match if the same URL appears in multiple feeds);
// otherwise the status row is created with a null entry_id. Used by the
// URL-based read endpoint for preview and shared articles. Storing an
// explicit 'read' row is a stronger signal than relying on absence.
func (s *Store) UpdateEntryStatusByURL(ctx context.Context, userID int, articleURL, status string) error {
	articleURL = urlnorm.URL(articleURL)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT $1, $2, (SELECT e.id FROM entries e WHERE e.url = $2 LIMIT 1), $3, NOW()
		 ON CONFLICT (user_id, article_url) DO UPDATE SET status = EXCLUDED.status, changed_at = NOW()`,
		userID, articleURL, status)
	return err
}

// MarkFeedEntriesRead marks all entries in the given feed as read for the
// user (upserts entry_read_status to 'read').
func (s *Store) MarkFeedEntriesRead(ctx context.Context, feedID, userID int) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT DISTINCT ON (e.url) $2, e.url, e.id, 'read', NOW()
		 FROM entries e
		 WHERE e.feed_id = $1 AND (`+visibleEntryFilter(userID)+`)
		 ORDER BY e.url
		 ON CONFLICT (user_id, article_url) DO UPDATE SET status = 'read', changed_at = NOW()`,
		feedID, userID)
	return err
}

// CountEntriesByFeed returns the number of entries for the given global feed.
func (s *Store) CountEntriesByFeed(ctx context.Context, feedID int) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE feed_id = $1`, feedID,
	).Scan(&count)
	return count, err
}

// MarkAllEntriesRead marks every visible entry as read for the user (upserts
// entry_read_status to 'read' for all subscribed feeds and shares by followed
// users).
func (s *Store) MarkAllEntriesRead(ctx context.Context, userID int) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT DISTINCT ON (e.url) $1, e.url, e.id, 'read', NOW()
		 FROM entries e
		 WHERE (`+visibleEntryFilter(userID)+`)
		 ORDER BY e.url
		 ON CONFLICT (user_id, article_url) DO UPDATE SET status = 'read', changed_at = NOW()`,
		userID)
	return err
}

// MarkShareUnreadForFollowers inserts an 'unread' entry_read_status row for
// every user following the sharer. Called when a new share is created (via the
// API or a live relay event) so followers see the article as unread. The row
// is keyed by article_url, matching an existing entry when one is materialized.
// Followers who already have a row are not overwritten.
func (s *Store) MarkShareUnreadForFollowers(ctx context.Context, sharerID int, articleURL string, entryID int64) error {
	articleURL = urlnorm.URL(articleURL)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT uf.follower_id, $2, $3, 'unread', NOW()
		 FROM user_follows uf
		 WHERE uf.followee_id = $1
		 ON CONFLICT (user_id, article_url) DO NOTHING`,
		sharerID, articleURL, sql.NullInt64{Int64: entryID, Valid: entryID > 0})
	return err
}

// MarkEntryUnreadForSubscribers inserts an 'unread' entry_read_status row for
// every user subscribed to the entry's feed. Called by the feed processor
// when a new entry is materialized so subscribers see it as unread (absent
// rows still mean read). Subscribers who already have a row (e.g. the
// article was shared to them and marked unread) are not overwritten.
func (s *Store) MarkEntryUnreadForSubscribers(ctx context.Context, feedID int, articleURL string, entryID int64) error {
	articleURL = urlnorm.URL(articleURL)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
		 SELECT sub.user_id, $2, $3, 'unread', NOW()
		 FROM subscriptions sub
		 WHERE sub.feed_id = $1
		 ON CONFLICT (user_id, article_url) DO NOTHING`,
		feedID, articleURL, sql.NullInt64{Int64: entryID, Valid: entryID > 0})
	return err
}

// --- Enclosures ---

// CreateEnclosure inserts a media attachment for an entry.
func (s *Store) CreateEnclosure(ctx context.Context, entryID int64, url, mimeType string, size int64) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO enclosures (entry_id, url, mime_type, size) VALUES ($1, $2, $3, $4)
		 ON CONFLICT DO NOTHING`,
		entryID, url, mimeType, size)
	return err
}

// ListEnclosuresByEntry returns all enclosures for the given entries.
func (s *Store) ListEnclosuresByEntry(ctx context.Context, entryIDs []int64) (map[int64][]Enclosure, error) {
	if len(entryIDs) == 0 {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, entry_id, url, mime_type, size FROM enclosures WHERE entry_id = ANY($1)`,
		pq.Array(entryIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	m := make(map[int64][]Enclosure)
	for rows.Next() {
		var enc Enclosure
		if err := rows.Scan(&enc.ID, &enc.EntryID, &enc.URL, &enc.MimeType, &enc.Size); err != nil {
			return nil, err
		}
		m[enc.EntryID] = append(m[enc.EntryID], enc)
	}
	return m, rows.Err()
}

// --- API Tokens ---

// CreateAPIToken inserts an API token for the given user. expiresAt may be
// nil for a non-expiring token; origin is "manual" or "device_flow".
func (s *Store) CreateAPIToken(ctx context.Context, userID int, label, tokenHash, origin string, expiresAt *time.Time) (*APIToken, error) {
	var t APIToken
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO api_tokens (user_id, label, token_hash, origin, expires_at) VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, label, token_hash, origin, created_at, last_used_at, expires_at`,
		userID, label, tokenHash, origin, expiresAt,
	).Scan(&t.ID, &t.UserID, &t.Label, &t.TokenHash, &t.Origin, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("create api token: %w", err)
	}
	return &t, nil
}

// ListAPITokens returns API tokens for the given user.
func (s *Store) ListAPITokens(ctx context.Context, userID int) ([]APIToken, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, label, token_hash, origin, created_at, last_used_at, expires_at
		 FROM api_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Label, &t.TokenHash, &t.Origin, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// GetAPITokenByHash returns the API token matching the hash, or nil. It also
// bumps last_used_at. Expired tokens (expires_at < now) are treated as
// invalid and return nil.
func (s *Store) GetAPITokenByHash(ctx context.Context, tokenHash string) (*APIToken, error) {
	var t APIToken
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, label, token_hash, origin, created_at, last_used_at, expires_at
		 FROM api_tokens
		 WHERE token_hash = $1
		   AND (expires_at IS NULL OR expires_at > NOW())`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.Label, &t.TokenHash, &t.Origin, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = NOW() WHERE id = $1`, t.ID); err != nil {
		slog.Warn("store: update token last_used_at", "token_id", t.ID, "err", err)
	}
	return &t, nil
}

// DeleteAPIToken removes an API token scoped to the owning user.
func (s *Store) DeleteAPIToken(ctx context.Context, id, userID int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- Device Codes (RFC 8628) ---

// CreateDeviceCode inserts a new device authorization grant. deviceCodeHash
// is the SHA-256 hash of the plaintext device code (which is only returned
// to the caller, never stored).
func (s *Store) CreateDeviceCode(ctx context.Context, deviceCodeHash, userCode string, intervalSecs int, expiresAt time.Time) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO device_codes (device_code, user_code, interval_s, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, device_code, user_code, status, user_id, token_id, token_plaintext, interval_s, created_at, expires_at, last_polled_at`,
		deviceCodeHash, userCode, intervalSecs, expiresAt,
	).Scan(&dc.ID, &dc.DeviceCode, &dc.UserCode, &dc.Status, &dc.UserID, &dc.TokenID, &dc.TokenPlaintext, &dc.IntervalSecs, &dc.CreatedAt, &dc.ExpiresAt, &dc.LastPolledAt)
	if err != nil {
		return nil, fmt.Errorf("create device code: %w", err)
	}
	return &dc, nil
}

// GetDeviceCodeByHash returns the device code grant matching the hash, or
// nil. Expired grants are returned with Status set to "expired" so callers
// can distinguish "expired" from "pending"/"authorized".
func (s *Store) GetDeviceCodeByHash(ctx context.Context, deviceCodeHash string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, device_code, user_code, status, user_id, token_id, token_plaintext, interval_s, created_at, expires_at, last_polled_at
		 FROM device_codes WHERE device_code = $1`, deviceCodeHash,
	).Scan(&dc.ID, &dc.DeviceCode, &dc.UserCode, &dc.Status, &dc.UserID, &dc.TokenID, &dc.TokenPlaintext, &dc.IntervalSecs, &dc.CreatedAt, &dc.ExpiresAt, &dc.LastPolledAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if dc.Status == "pending" && time.Now().After(dc.ExpiresAt) {
		dc.Status = "expired"
	}
	return &dc, nil
}

// GetDeviceCodeByUserCode returns the device code grant matching the
// human-readable user code, or nil. Used by the confirm endpoint.
func (s *Store) GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, device_code, user_code, status, user_id, token_id, token_plaintext, interval_s, created_at, expires_at, last_polled_at
		 FROM device_codes WHERE user_code = $1`, userCode,
	).Scan(&dc.ID, &dc.DeviceCode, &dc.UserCode, &dc.Status, &dc.UserID, &dc.TokenID, &dc.TokenPlaintext, &dc.IntervalSecs, &dc.CreatedAt, &dc.ExpiresAt, &dc.LastPolledAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if dc.Status == "pending" && time.Now().After(dc.ExpiresAt) {
		dc.Status = "expired"
	}
	return &dc, nil
}

// AuthorizeDeviceCode marks a pending grant as authorized by userID and
// attaches the freshly minted tokenID and the plaintext token (returned
// once to the polling CLI, then the row is deleted). Returns the updated
// grant, or nil if the grant was not pending (already confirmed/denied/
// expired/missing).
func (s *Store) AuthorizeDeviceCode(ctx context.Context, userCode string, userID, tokenID int, tokenPlaintext string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.DB.QueryRowContext(ctx,
		`UPDATE device_codes
		   SET status = 'authorized', user_id = $2, token_id = $3, token_plaintext = $4
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > NOW()
		 RETURNING id, device_code, user_code, status, user_id, token_id, token_plaintext, interval_s, created_at, expires_at, last_polled_at`,
		userCode, userID, tokenID, tokenPlaintext,
	).Scan(&dc.ID, &dc.DeviceCode, &dc.UserCode, &dc.Status, &dc.UserID, &dc.TokenID, &dc.TokenPlaintext, &dc.IntervalSecs, &dc.CreatedAt, &dc.ExpiresAt, &dc.LastPolledAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authorize device code: %w", err)
	}
	return &dc, nil
}

// DenyDeviceCode marks a pending grant as denied. Returns the updated grant,
// or nil if the grant was not pending.
func (s *Store) DenyDeviceCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.DB.QueryRowContext(ctx,
		`UPDATE device_codes SET status = 'denied'
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > NOW()
		 RETURNING id, device_code, user_code, status, user_id, token_id, token_plaintext, interval_s, created_at, expires_at, last_polled_at`,
		userCode,
	).Scan(&dc.ID, &dc.DeviceCode, &dc.UserCode, &dc.Status, &dc.UserID, &dc.TokenID, &dc.TokenPlaintext, &dc.IntervalSecs, &dc.CreatedAt, &dc.ExpiresAt, &dc.LastPolledAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("deny device code: %w", err)
	}
	return &dc, nil
}

// TouchDeviceCodePoll bumps last_polled_at on the grant row so the
// slow_down check can detect too-frequent polling.
func (s *Store) TouchDeviceCodePoll(ctx context.Context, deviceCodeHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE device_codes SET last_polled_at = NOW() WHERE device_code = $1`, deviceCodeHash)
	return err
}

// ConsumeDeviceCode deletes an authorized grant after its token has been
// handed to the CLI, so the device code is single-use and cannot be replayed.
func (s *Store) ConsumeDeviceCode(ctx context.Context, deviceCodeHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM device_codes WHERE device_code = $1 AND status = 'authorized'`, deviceCodeHash)
	return err
}

// PurgeExpiredDeviceCodes removes expired or consumed grants older than the
// given retention window. Called lazily or by a background sweeper.
func (s *Store) PurgeExpiredDeviceCodes(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM device_codes
		 WHERE (status IN ('expired', 'denied') OR expires_at < NOW() - $1::interval)`,
		retention.String())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Users (Limen-managed table, read-only from here) ---

// UpdateUser updates the display_name of the user with the given id, and the
// bio when bio is non-nil (a nil bio leaves the existing value untouched, so
// display-name-only edits don't clobber the bio).
func (s *Store) UpdateUser(ctx context.Context, id int, displayName string, bio *string) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`UPDATE users SET display_name = $2, bio = COALESCE($3, bio) WHERE id = $1
		 RETURNING id, COALESCE(handle, ''), COALESCE(did, ''), COALESCE(display_name, ''), COALESCE(bio, ''),
		           COALESCE(avatar, ''), COALESCE(banner, ''),
		           created_at, pds_sync_status, pds_synced_at, onboarded`,
		id, displayName, bio,
	).Scan(&u.ID, &u.Handle, &u.DID, &u.DisplayName, &u.Bio, &u.Avatar, &u.Banner, &u.CreatedAt, &u.PDSSyncStatus, &u.PDSSyncedAt, &u.Onboarded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.setHasImages()
	return &u, nil
}

// UpdateUserAvatar caches the PDS getBlob URL for the user's avatar. Pass an
// empty string to clear the avatar. The banner is left unchanged.
func (s *Store) UpdateUserAvatar(ctx context.Context, id int, avatar string) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`UPDATE users SET avatar = $2 WHERE id = $1
		 RETURNING id, COALESCE(handle, ''), COALESCE(did, ''), COALESCE(display_name, ''), COALESCE(bio, ''),
		           COALESCE(avatar, ''), COALESCE(banner, ''),
		           created_at, pds_sync_status, pds_synced_at, onboarded`,
		id, avatar,
	).Scan(&u.ID, &u.Handle, &u.DID, &u.DisplayName, &u.Bio, &u.Avatar, &u.Banner, &u.CreatedAt, &u.PDSSyncStatus, &u.PDSSyncedAt, &u.Onboarded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.setHasImages()
	return &u, nil
}

// UpdateUserProfileFromPDS caches the text + image fields read from the user's
// app.bsky.actor.profile record on their PDS. It is the inbound sync path — the
// PDS is the source of truth for display_name, bio, avatar, and banner, so this
// overwrites the local cache unconditionally. avatar/banner are full public getBlob
// URLs (pds + did + cid) so the web can render them directly. Called from the
// login backfill, the followee backfill, and the relay profile event consumer.
func (s *Store) UpdateUserProfileFromPDS(ctx context.Context, userID int, displayName, bio, avatar, banner string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET display_name = $2, bio = $3, avatar = $4, banner = $5 WHERE id = $1`,
		userID, displayName, bio, avatar, banner,
	)
	if err != nil {
		return fmt.Errorf("update profile from pds: %w", err)
	}
	return nil
}

// DeleteUser deletes the user with the given id and all their associated data.
func (s *Store) DeleteUser(ctx context.Context, id int) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// GetUserByID returns the user with the given id, or nil.
func (s *Store) GetUserByID(ctx context.Context, id int) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, COALESCE(handle, ''), COALESCE(did, ''), COALESCE(display_name, ''), COALESCE(bio, ''),
		       COALESCE(avatar, ''), COALESCE(banner, ''),
		       created_at, pds_sync_status, pds_synced_at, onboarded
		 FROM users
		 WHERE id = $1`, id,
	).Scan(&u.ID, &u.Handle, &u.DID, &u.DisplayName, &u.Bio, &u.Avatar, &u.Banner, &u.CreatedAt, &u.PDSSyncStatus, &u.PDSSyncedAt, &u.Onboarded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.setHasImages()
	return &u, nil
}

// SetUserSyncStatus updates the post-login PDS sync status for a user. When
// status is a terminal state ("idle" or "failed"), pds_synced_at is stamped to
// NOW(); for "syncing" it is left untouched.
func (s *Store) SetUserSyncStatus(ctx context.Context, userID int, status string) error {
	q := `UPDATE users SET pds_sync_status = $1 WHERE id = $2`
	switch status {
	case "idle", "failed":
		q = `UPDATE users SET pds_sync_status = $1, pds_synced_at = NOW() WHERE id = $2`
	}
	_, err := s.DB.ExecContext(ctx, q, status, userID)
	return err
}

// MarkUserOnboarded flips the user's onboarding flag to true and returns the
// updated user. Idempotent: re-marking an already-onboarded user is a no-op
// that still returns the current row. Called when the web client finishes
// (or dismisses) the first-run onboarding overlay.
func (s *Store) MarkUserOnboarded(ctx context.Context, userID int) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`UPDATE users SET onboarded = TRUE WHERE id = $1
		 RETURNING id, COALESCE(handle, ''), COALESCE(did, ''), COALESCE(display_name, ''), COALESCE(bio, ''),
		           COALESCE(avatar, ''), COALESCE(banner, ''),
		           created_at, pds_sync_status, pds_synced_at, onboarded`,
		userID,
	).Scan(&u.ID, &u.Handle, &u.DID, &u.DisplayName, &u.Bio, &u.Avatar, &u.Banner, &u.CreatedAt, &u.PDSSyncStatus, &u.PDSSyncedAt, &u.Onboarded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.setHasImages()
	return &u, nil
}

// --- Cleanup ---

// PurgeOldEntries removes entries with status 'removed' older than maxAgeDays.
func (s *Store) PurgeOldEntries(ctx context.Context, maxAgeDays int) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM entries WHERE status = 'removed' AND changed_at < NOW() - ($1 || ' days')::interval`,
		fmt.Sprintf("%d", maxAgeDays))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- scan helper ---
