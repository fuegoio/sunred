package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrHandleTaken is returned when a requested handle is already in use.
var ErrHandleTaken = fmt.Errorf("handle already taken")

// ErrHandleInvalid is returned when a handle does not match the allowed format.
var ErrHandleInvalid = fmt.Errorf("handle must be 3–64 characters: letters, digits, hyphens, or underscores")

// ErrProfileNotFound is returned when no profile row exists for the given handle.
var ErrProfileNotFound = fmt.Errorf("user not found")

// ErrAlreadyFollowing is returned when the follow relationship already exists.
var ErrAlreadyFollowing = fmt.Errorf("already following this user")

// ErrCannotFollowSelf is returned when a user tries to follow themselves.
var ErrCannotFollowSelf = fmt.Errorf("cannot follow yourself")

// ErrShareNotFound is returned when a shared article lookup returns no row.
var ErrShareNotFound = fmt.Errorf("shared article not found")

var handleRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

// --- Handles / Profiles ---

// UpsertHandle creates or updates the handle and bio for a user.
// Returns ErrHandleInvalid for bad format, ErrHandleTaken for conflicts.
func (s *Store) UpsertHandle(ctx context.Context, userID int, handle, bio string) (*UserProfile, error) {
	handle = strings.TrimSpace(handle)
	if !handleRe.MatchString(handle) {
		return nil, ErrHandleInvalid
	}

	var p UserProfile
	err := s.DB.QueryRowContext(ctx, `
		UPDATE users SET handle = $2, bio = $3
		WHERE id = $1
		RETURNING id, COALESCE(handle, ''), COALESCE(display_name, ''), bio, created_at`,
		userID, handle, bio,
	).Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_users_handle") ||
			strings.Contains(err.Error(), "unique") {
			return nil, ErrHandleTaken
		}
		return nil, fmt.Errorf("upsert handle: %w", err)
	}
	return &p, nil
}

// GetProfileByHandle returns the public profile for the given handle.
// viewerID is used to populate IsFollowing (0 = no viewer context).
func (s *Store) GetProfileByHandle(ctx context.Context, handle string, viewerID int) (*UserProfile, error) {
	var p UserProfile
	err := s.DB.QueryRowContext(ctx, `
		SELECT
		  u.id,
		  u.handle,
		  u.bio,
		  u.created_at,
		  COALESCE(u.display_name, ''),
		  COALESCE(u.did, ''),
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id),
		  (SELECT COUNT(*) FROM user_follows WHERE follower_id = u.id),
		  CASE WHEN $2 > 0 THEN
		    EXISTS(SELECT 1 FROM user_follows WHERE follower_id = $2 AND followee_id = u.id)
		  ELSE false END
		FROM users u
		WHERE u.handle = $1`,
		handle, viewerID,
	).Scan(
		&p.UserID, &p.Handle, &p.Bio,
		&p.CreatedAt,
		&p.DisplayName, &p.DID,
		&p.FollowerCount, &p.FollowingCount,
		&p.IsFollowing,
	)
	if err == sql.ErrNoRows {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &p, nil
}

// GetProfileByUserID returns the social profile for the given user id, or nil.
func (s *Store) GetProfileByUserID(ctx context.Context, userID int) (*UserProfile, error) {
	var p UserProfile
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, COALESCE(handle, ''), bio, created_at
		FROM users WHERE id = $1`, userID,
	).Scan(&p.UserID, &p.Handle, &p.Bio, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get profile by user id: %w", err)
	}
	return &p, nil
}

// --- Follows ---

// FollowUser creates a follow relationship. Returns ErrCannotFollowSelf or
// ErrAlreadyFollowing when applicable, ErrProfileNotFound when the followee
// has no profile.
func (s *Store) FollowUser(ctx context.Context, followerID int, followeeHandle string) error {
	// Resolve handle → user_id.
	var followeeID int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE handle = $1`, followeeHandle,
	).Scan(&followeeID)
	if err == sql.ErrNoRows {
		return ErrProfileNotFound
	}
	if err != nil {
		return fmt.Errorf("resolve handle: %w", err)
	}
	if followerID == followeeID {
		return ErrCannotFollowSelf
	}

	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO user_follows (follower_id, followee_id) VALUES ($1, $2)`,
		followerID, followeeID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return ErrAlreadyFollowing
		}
		return fmt.Errorf("follow user: %w", err)
	}
	return nil
}

// UnfollowUser removes the follow relationship.
func (s *Store) UnfollowUser(ctx context.Context, followerID int, followeeHandle string) error {
	var followeeID int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE handle = $1`, followeeHandle,
	).Scan(&followeeID)
	if err == sql.ErrNoRows {
		return ErrProfileNotFound
	}
	if err != nil {
		return fmt.Errorf("resolve handle: %w", err)
	}

	_, err = s.DB.ExecContext(ctx,
		`DELETE FROM user_follows WHERE follower_id = $1 AND followee_id = $2`,
		followerID, followeeID,
	)
	return err
}

// ListFollowing returns the profiles of users that followerID is following.
func (s *Store) ListFollowing(ctx context.Context, followerID int) ([]UserProfile, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
		  u.id, u.handle, u.bio, u.created_at,
		  COALESCE(u.display_name, ''),
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id),
		  (SELECT COUNT(*) FROM user_follows WHERE follower_id = u.id)
		FROM user_follows f
		JOIN users u ON u.id = f.followee_id
		WHERE f.follower_id = $1
		ORDER BY f.created_at DESC`, followerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list following: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(
			&p.UserID, &p.Handle, &p.Bio, &p.CreatedAt,
			&p.DisplayName, &p.FollowerCount, &p.FollowingCount,
		); err != nil {
			return nil, err
		}
		p.IsFollowing = true
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListFollowers returns the profiles of users following userID.
func (s *Store) ListFollowers(ctx context.Context, userID, viewerID int) ([]UserProfile, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
		  u.id, u.handle, u.bio, u.created_at,
		  COALESCE(u.display_name, ''),
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id),
		  (SELECT COUNT(*) FROM user_follows WHERE follower_id = u.id),
		  CASE WHEN $2 > 0 THEN
		    EXISTS(SELECT 1 FROM user_follows WHERE follower_id = $2 AND followee_id = u.id)
		  ELSE false END
		FROM user_follows f
		JOIN users u ON u.id = f.follower_id
		WHERE f.followee_id = $1
		ORDER BY f.created_at DESC`, userID, viewerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list followers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(
			&p.UserID, &p.Handle, &p.Bio, &p.CreatedAt,
			&p.DisplayName, &p.FollowerCount, &p.FollowingCount, &p.IsFollowing,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SearchUsers returns up to limit profiles whose handle or display_name match
// the query (case-insensitive prefix/substring match). The viewer is excluded
// from the results, and IsFollowing is populated for each row.
func (s *Store) SearchUsers(ctx context.Context, q string, viewerID, limit int) ([]UserProfile, error) {
	q = strings.TrimSpace(q)
	if q == "" || limit <= 0 {
		return []UserProfile{}, nil
	}
	// Escape SQL LIKE wildcards in the query so user input is matched literally.
	like := "%" + strings.NewReplacer(
		"\\", "\\\\",
		"%", "\\%",
		"_", "\\_",
	).Replace(q) + "%"

	rows, err := s.DB.QueryContext(ctx, `
		SELECT
		  u.id, u.handle, u.bio, u.created_at,
		  COALESCE(u.display_name, ''),
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id),
		  (SELECT COUNT(*) FROM user_follows WHERE follower_id = u.id),
		  CASE WHEN $3 > 0 THEN
		    EXISTS(SELECT 1 FROM user_follows WHERE follower_id = $3 AND followee_id = u.id)
		  ELSE false END
		FROM users u
		WHERE u.id <> $3
		  AND u.handle IS NOT NULL
		  AND (u.handle ILIKE $1 OR u.display_name ILIKE $1)
		ORDER BY
		  (u.handle ILIKE $2) DESC,
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id) DESC
		LIMIT $4`,
		like, q+"%", viewerID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(
			&p.UserID, &p.Handle, &p.Bio, &p.CreatedAt,
			&p.DisplayName, &p.FollowerCount, &p.FollowingCount, &p.IsFollowing,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- Shared articles ---

// ShareArticle creates or replaces a shared article for the user.
// ensureSharedEntry materializes a shared article as a global entry against its
// source feed (creating the feed if missing) so that the article appears in
// followers' entry streams via the ListEntries UNION. The entry is keyed by a
// hash of the article URL within the source feed. Returns the entry id (0 if
// the share carries no feed_url or on insert failure).
func (s *Store) ensureSharedEntry(ctx context.Context,
	articleURL, title, description, feedURL, feedTitle, feedSiteURL, author string,
	publishedAt *time.Time,
) int64 {
	if feedURL == "" {
		return 0
	}
	feed, err := s.GetOrCreateFeed(ctx, feedURL, feedSiteURL, feedTitle, "")
	if err != nil || feed == nil {
		return 0
	}
	h := sha256.Sum256([]byte(articleURL))
	hash := hex.EncodeToString(h[:])
	pubAt := time.Now()
	if publishedAt != nil {
		pubAt = *publishedAt
	}
	eid, _ := s.CreateEntry(ctx, feed.ID, hash, title, articleURL, "", author, "", description, pubAt, nil)
	return eid
}

func (s *Store) ShareArticle(ctx context.Context, userID int,
	articleURL, title, description, feedURL, feedTitle, feedSiteURL, author string,
	publishedAt *time.Time,
) (*SharedArticle, error) {
	entryID := s.ensureSharedEntry(ctx, articleURL, title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt)

	var sa SharedArticle
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO shared_articles
		  (user_id, article_url, title, description, feed_url, feed_title, feed_site_url, author, published_at, entry_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (user_id, article_url) DO UPDATE
		  SET title        = EXCLUDED.title,
		      description  = EXCLUDED.description,
		      feed_url     = EXCLUDED.feed_url,
		      feed_title   = EXCLUDED.feed_title,
		      feed_site_url= EXCLUDED.feed_site_url,
		      author       = EXCLUDED.author,
		      published_at = EXCLUDED.published_at,
		      entry_id     = COALESCE(shared_articles.entry_id, EXCLUDED.entry_id),
		      shared_at    = NOW()
		RETURNING id, user_id, article_url, title, description,
		          feed_url, feed_title, feed_site_url, author, published_at, shared_at, entry_id`,
		userID, articleURL, title, description, feedURL, feedTitle, feedSiteURL, author, publishedAt, sql.NullInt64{Int64: entryID, Valid: entryID > 0},
	).Scan(
		&sa.ID, &sa.UserID, &sa.ArticleURL, &sa.Title, &sa.Description,
		&sa.FeedURL, &sa.FeedTitle, &sa.FeedSiteURL, &sa.Author, &sa.PublishedAt, &sa.SharedAt, &sa.EntryID,
	)
	if err != nil {
		return nil, fmt.Errorf("share article: %w", err)
	}
	return &sa, nil
}

// UnshareArticle removes a shared article owned by userID.
func (s *Store) UnshareArticle(ctx context.Context, id int64, userID int) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM shared_articles WHERE id = $1 AND user_id = $2`, id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrShareNotFound
	}
	return nil
}

// GetSharedArticleByURL returns the share for a given user+URL, or nil.
func (s *Store) GetSharedArticleByURL(ctx context.Context, userID int, articleURL string) (*SharedArticle, error) {
	var sa SharedArticle
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, article_url, title, description,
		       feed_url, feed_title, feed_site_url, author, published_at, shared_at, entry_id
		FROM shared_articles WHERE user_id = $1 AND article_url = $2`,
		userID, articleURL,
	).Scan(
		&sa.ID, &sa.UserID, &sa.ArticleURL, &sa.Title, &sa.Description,
		&sa.FeedURL, &sa.FeedTitle, &sa.FeedSiteURL, &sa.Author, &sa.PublishedAt, &sa.SharedAt, &sa.EntryID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sa, nil
}

// ListSharedArticlesByUser returns shared articles for the given user_id,
// newest first. Used for profile view.
func (s *Store) ListSharedArticlesByUser(ctx context.Context, userID int) ([]SharedArticle, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, article_url, title, description,
		       feed_url, feed_title, feed_site_url, author, published_at, shared_at, entry_id
		FROM shared_articles
		WHERE user_id = $1
		ORDER BY shared_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SharedArticle
	for rows.Next() {
		var sa SharedArticle
		if err := rows.Scan(
			&sa.ID, &sa.UserID, &sa.ArticleURL, &sa.Title, &sa.Description,
			&sa.FeedURL, &sa.FeedTitle, &sa.FeedSiteURL, &sa.Author, &sa.PublishedAt, &sa.SharedAt, &sa.EntryID,
		); err != nil {
			return nil, err
		}
		out = append(out, sa)
	}
	return out, rows.Err()
}

// ListSocialTimeline returns shared articles from users that followerID follows,
// newest first. Includes sharer metadata for rendering.
func (s *Store) ListSocialTimeline(ctx context.Context, followerID, limit, offset int) ([]SharedArticle, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
		  sa.id, sa.user_id, sa.article_url, sa.title, sa.description,
		  sa.feed_url, sa.feed_title, sa.feed_site_url, sa.author,
		  sa.published_at, sa.shared_at, sa.entry_id,
		  COALESCE(u.handle, ''),
		  COALESCE(u.display_name, '')
		FROM shared_articles sa
		JOIN user_follows f ON f.followee_id = sa.user_id AND f.follower_id = $1
		JOIN users u ON u.id = sa.user_id
		ORDER BY sa.shared_at DESC
		LIMIT $2 OFFSET $3`,
		followerID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SharedArticle
	for rows.Next() {
		var sa SharedArticle
		if err := rows.Scan(
			&sa.ID, &sa.UserID, &sa.ArticleURL, &sa.Title, &sa.Description,
			&sa.FeedURL, &sa.FeedTitle, &sa.FeedSiteURL, &sa.Author,
			&sa.PublishedAt, &sa.SharedAt, &sa.EntryID,
			&sa.SharerHandle, &sa.SharerDisplayName,
		); err != nil {
			return nil, err
		}
		out = append(out, sa)
	}
	return out, rows.Err()
}

// --- Feed subscribers ---

// CountFeedSubscribers returns the number of users subscribed to the given feed.
func (s *Store) CountFeedSubscribers(ctx context.Context, feedID int) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM subscriptions WHERE feed_id = $1`, feedID,
	).Scan(&n)
	return n, err
}

// ListFeedSubscribers returns the public profiles of users subscribed to the
// given feed. Profiles without a handle are excluded (anonymous subscribers).
func (s *Store) ListFeedSubscribers(ctx context.Context, feedID int) ([]UserProfile, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
		  u.id, u.handle, u.bio, u.created_at,
		  COALESCE(u.display_name, ''),
		  (SELECT COUNT(*) FROM user_follows WHERE followee_id = u.id),
		  (SELECT COUNT(*) FROM user_follows WHERE follower_id = u.id)
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.feed_id = $1
		  AND u.handle IS NOT NULL
		ORDER BY u.handle ASC`, feedID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(
			&p.UserID, &p.Handle, &p.Bio, &p.CreatedAt,
			&p.DisplayName, &p.FollowerCount, &p.FollowingCount,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPublicFeedsByUser returns the feeds a user subscribes to (for the public
// profile view), joined to their subscription for the title.
func (s *Store) ListPublicFeedsByUser(ctx context.Context, userID int) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.id, f.feed_url, f.site_url, COALESCE(s.title_override, f.title), f.description,
		        f.etag_header, f.last_modified_header,
		        f.parsing_error, f.parsing_error_count, f.disabled,
		        f.scraper_rules, f.rewrite_rules, f.crawler,
		        f.next_check_at, f.last_fetch_at, f.created_at, f.updated_at
		FROM feeds f
		JOIN subscriptions s ON s.feed_id = f.id AND s.user_id = $1
		ORDER BY COALESCE(s.title_override, f.title) ASC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Feed
	for rows.Next() {
		var f Feed
		if err := rows.Scan(
			&f.ID, &f.FeedURL, &f.SiteURL, &f.Title, &f.Description,
			&f.EtagHeader, &f.LastModified,
			&f.ParsingError, &f.ParsingErrorCount, &f.Disabled,
			&f.ScraperRules, &f.RewriteRules, &f.Crawler,
			&f.NextCheckAt, &f.LastFetchAt, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
