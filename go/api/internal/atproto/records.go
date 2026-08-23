package atproto

import "time"

// Lexicon collection IDs for io.sunred.* record types and the Bluesky
// profile record (app.bsky.actor.profile) this instance mirrors the user's
// display name + bio into so the profile is searchable on the network.
const (
	CollectionFollow       = "io.sunred.graph.follow"
	CollectionShare        = "io.sunred.share.article"
	CollectionSubscription = "io.sunred.feed.subscription"
	CollectionProfile      = "app.bsky.actor.profile"

	// ProfileRkey is the fixed record key for the single app.bsky.actor.profile
	// record every repo has at most one of.
	ProfileRkey = "self"
)

// FollowRecord is the io.sunred.graph.follow record.
type FollowRecord struct {
	Type      string `json:"$type"`
	Subject   string `json:"subject"` // followee DID
	CreatedAt string `json:"createdAt"`
}

// ShareRecord is the io.sunred.share.article record.
type ShareRecord struct {
	Type        string `json:"$type"`
	ArticleURL  string `json:"articleUrl"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	FeedURL     string `json:"feedUrl,omitempty"`
	FeedTitle   string `json:"feedTitle,omitempty"`
	FeedSiteURL string `json:"feedSiteUrl,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	SharedAt    string `json:"sharedAt"`
}

// SubscriptionRecord is the io.sunred.feed.subscription record.
type SubscriptionRecord struct {
	Type      string `json:"$type"`
	FeedURL   string `json:"feedUrl"`
	SiteURL   string `json:"siteUrl,omitempty"`
	Title     string `json:"title,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// ProfileRecord is the app.bsky.actor.profile record (rkey "self"). Only the
// text fields Sunred manages are populated; avatar/banner are left for the
// user to set from a Bluesky client. createdAt is preserved across updates so
// the record's creation timestamp stays stable.
type ProfileRecord struct {
	Type        string `json:"$type"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// FormatTime formats a time.Time as an AT Proto datetime string (RFC3339).
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
