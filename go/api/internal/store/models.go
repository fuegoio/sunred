package store

import (
	"database/sql"
	"time"
)

// Folder groups a user's feeds for organisational purposes. Folders can be
// nested via ParentID and ordered via SortOrder.
type Folder struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	ParentID  *int      `json:"parent_id,omitempty"`
	Title     string    `json:"title"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Feed is a global RSS/Atom/JSON Feed source: one row per feed_url, shared
// fetch state. A user's view of a feed (folder, title override) comes from
// their subscription; when listed for a user, FolderID/Title are populated
// from the subscriptions join.
type Feed struct {
	ID                int        `json:"id"`
	FeedURL           string     `json:"feed_url"`
	SiteURL           string     `json:"site_url"`
	Title             string     `json:"title"`
	Description       string     `json:"description,omitempty"`
	FolderID          *int       `json:"folder_id,omitempty"`
	EtagHeader        string     `json:"-"`
	LastModified      string     `json:"-"`
	ParsingError      string     `json:"parsing_error,omitempty"`
	ParsingErrorCount int        `json:"parsing_error_count"`
	Disabled          bool       `json:"disabled"`
	ScraperRules      string     `json:"scraper_rules,omitempty"`
	RewriteRules      string     `json:"rewrite_rules,omitempty"`
	Crawler           bool       `json:"crawler"`
	NextCheckAt       *time.Time `json:"next_check_at,omitempty"`
	LastFetchAt       *time.Time `json:"last_fetch_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Entry is a single article/item belonging to a global feed. Read state lives
// in entry_read_status and star state in entry_stars, both per-user and keyed
// by article URL; absent state means unread/unstarred. Feed is the nested
// global source feed (joined on read) so a consumer can render the source
// feed and offer a subscribe affordance without an extra lookup, including
// for shares from feeds the viewer doesn't subscribe to. SharedBy/SharedByName
// identify the followed user who shared the entry, when the entry reached the
// viewer via a share rather than a subscription.
type Entry struct {
	ID           int64       `json:"id"`
	FeedID       int         `json:"feed_id"`
	Hash         string      `json:"hash"`
	Title        string      `json:"title"`
	URL          string      `json:"url"`
	CommentsURL  string      `json:"comments_url,omitempty"`
	Author       string      `json:"author,omitempty"`
	Content      string      `json:"-"`
	Description  string      `json:"description,omitempty"`
	Status       string      `json:"status"`
	Starred      bool        `json:"starred"`
	PublishedAt  time.Time   `json:"published_at"`
	ChangedAt    time.Time   `json:"changed_at"`
	Tags         []string    `json:"tags,omitempty"`
	Enclosures   []Enclosure `json:"enclosures,omitempty"`
	Feed         *Feed       `json:"feed,omitempty"`
	SharedBy     string      `json:"shared_by,omitempty"`
	SharedByName string      `json:"shared_by_name,omitempty"`
	// ShareID is the viewer's own shared_articles row id for this entry's
	// article_url, or nil when the viewer hasn't shared it. Populated on
	// list/get so clients can render the share toggle's initial state and
	// resolve the unshare DELETE target without a separate bulk fetch.
	ShareID *int64 `json:"share_id,omitempty"`
}

// Enclosure is a media attachment (podcast, image, file) on an entry.
type Enclosure struct {
	ID       int64  `json:"id"`
	EntryID  int64  `json:"entry_id"`
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// FeedIcon stores the favicon data for a feed.
type FeedIcon struct {
	FeedID int    `json:"feed_id"`
	Data   []byte `json:"-"`
}

// APIToken is a hashed API token issued to a user for bearer auth.
// ExpiresAt is nil for non-expiring tokens (e.g. created via the web UI);
// device-flow tokens carry a 14-day expiry. Origin records how the token
// was issued: "manual" or "device_flow".
type APIToken struct {
	ID         int        `json:"id"`
	UserID     int        `json:"user_id"`
	Label      string     `json:"label"`
	TokenHash  string     `json:"-"`
	Origin     string     `json:"origin"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// DeviceCode is an in-flight RFC 8628 device authorization grant.
// DeviceCode is stored hashed (SHA-256), like APIToken.TokenHash; the
// plaintext is only known to the CLI/TUI that initiated the flow.
// TokenPlaintext is set on confirm and returned once to the polling CLI,
// then the grant row is deleted (single-use).
type DeviceCode struct {
	ID             int64      `json:"-"`
	DeviceCode     string     `json:"-"`         // hash
	UserCode       string     `json:"user_code"` // "PLN-XXXX-XXXX"
	Status         string     `json:"status"`    // pending|authorized|denied|expired
	UserID         *int       `json:"-"`
	TokenID        *int       `json:"-"`
	TokenPlaintext *string    `json:"-"` // populated on authorize, consumed once
	IntervalSecs   int        `json:"interval"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	LastPolledAt   *time.Time `json:"-"`
}

// User represents an authenticated account. The local database is a cache of
// the user's ATProto data; the PDS is the source of truth.
type User struct {
	ID          int    `json:"id"`
	Handle      string `json:"handle"`
	DID         string `json:"did,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Bio         string `json:"bio,omitempty"`
	// Avatar/Banner are the public PDS getBlob URLs, kept for the image
	// proxy to fetch from. They are never serialized; clients use the
	// /users/{handle}/avatar|banner endpoints, gated by HasAvatar/HasBanner.
	Avatar    string    `json:"-"`
	Banner    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	// PDSSyncStatus tracks the post-login backfill from the user's PDS:
	// "syncing" while in progress, "idle" once done, "failed" on error.
	// The web UI polls this to show a waiting state on first login.
	PDSSyncStatus string     `json:"pds_sync_status"`
	PDSSyncedAt   *time.Time `json:"pds_synced_at,omitempty"`
	// HasAvatar/HasBanner tell the client an image exists without leaking
	// the PDS URL; the client fetches the bytes via the proxy endpoints.
	HasAvatar bool `json:"has_avatar,omitempty"`
	HasBanner bool `json:"has_banner,omitempty"`
}

// UserProfile holds the public profile data for a user, with denormalised
// social counts populated by query joins.
type UserProfile struct {
	UserID      int    `json:"user_id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name,omitempty"`
	Bio         string `json:"bio,omitempty"`
	// Avatar/Banner are the public PDS getBlob URLs, kept for the image
	// proxy to fetch from. Never serialized; see User for details.
	Avatar    string    `json:"-"`
	Banner    string    `json:"-"`
	DID       string    `json:"did,omitempty"`
	PDSUrl    string    `json:"pds_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// HasAvatar/HasBanner tell the client an image exists without leaking
	// the PDS URL; the client fetches the bytes via the proxy endpoints.
	HasAvatar bool `json:"has_avatar,omitempty"`
	HasBanner bool `json:"has_banner,omitempty"`
	// Denormalised fields set by query joins.
	FollowerCount  int  `json:"follower_count"`
	FollowingCount int  `json:"following_count"`
	IsFollowing    bool `json:"is_following,omitempty"`
}

// ATProtoCredentials holds the PDS session tokens for a user. Never exposed
// via the API — only read/written by internal AT Proto sync code.
type ATProtoCredentials struct {
	UserID       int
	DID          string
	PDSUrl       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	// Handle is the user's AT Proto handle. Populated by ListUsersWithATProto
	// (e.g. to announce the user to the relay on startup); not set by
	// GetATProtoCredentials.
	Handle string
}

// SharedArticle is an article that a user shared on the social timeline.
type SharedArticle struct {
	ID          int64      `json:"id"`
	UserID      int        `json:"user_id"`
	ArticleURL  string     `json:"article_url"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	FeedURL     string     `json:"feed_url,omitempty"`
	FeedTitle   string     `json:"feed_title,omitempty"`
	FeedSiteURL string     `json:"feed_site_url,omitempty"`
	Author      string     `json:"author,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	SharedAt    time.Time  `json:"shared_at"`
	EntryID     *int64     `json:"entry_id,omitempty"`
	// Viewer state, populated when fetched with a viewer context.
	Status  string `json:"status,omitempty"`
	Starred bool   `json:"starred,omitempty"`
	// Sharer info, populated on social timeline queries.
	SharerHandle      string `json:"sharer_handle,omitempty"`
	SharerDisplayName string `json:"sharer_display_name,omitempty"`
}

// Store wraps a *sql.DB with query helpers for the Sunred schema.
type Store struct {
	DB *sql.DB
}

// New returns a Store backed by the given database.
func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

// setHasImages derives the HasAvatar/HasBanner flags from the cached PDS URLs.
// Called by queries after scanning Avatar/Banner so the boolean is consistent
// with the column values without a separate SQL expression.
func (p *UserProfile) setHasImages() {
	p.HasAvatar = p.Avatar != ""
	p.HasBanner = p.Banner != ""
}

func (u *User) setHasImages() {
	u.HasAvatar = u.Avatar != ""
	u.HasBanner = u.Banner != ""
}
