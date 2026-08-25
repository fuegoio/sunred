package atproto

import (
	"net/url"
	"time"
)

// Lexicon collection IDs for io.sunred.* record types and the Bluesky
// profile record (app.bsky.actor.profile) this instance mirrors the user's
// display name + bio into so the profile is searchable on the network.
const (
	CollectionFollow       = "io.sunred.graph.follow"
	CollectionShare        = "io.sunred.share.article"
	CollectionSubscription = "io.sunred.feed.subscription"
	CollectionStar         = "io.sunred.entry.star"
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

// StarRecord is the io.sunred.entry.star record. It carries the same article
// metadata as ShareRecord so a remote instance can materialize the entry from
// a star event alone, without needing the feed to be fetched locally.
type StarRecord struct {
	Type        string `json:"$type"`
	ArticleURL  string `json:"articleUrl"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	FeedURL     string `json:"feedUrl,omitempty"`
	FeedTitle   string `json:"feedTitle,omitempty"`
	FeedSiteURL string `json:"feedSiteUrl,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// ProfileRecord is the app.bsky.actor.profile record (rkey "self"). Sunred
// mirrors the user's display name and bio out to this record and reads the
// avatar/banner blob refs back in to cache as getBlob URLs. Avatar/Banner are
// preserved across Sunred-authored putRecord writes so a local edit to the
// text fields doesn't wipe images the user set from a Bluesky client.
// createdAt is preserved across updates so the record's creation timestamp
// stays stable.
type ProfileRecord struct {
	Type        string   `json:"$type"`
	DisplayName string   `json:"displayName,omitempty"`
	Description string   `json:"description,omitempty"`
	Avatar      *BlobRef `json:"avatar,omitempty"`
	Banner      *BlobRef `json:"banner,omitempty"`
	CreatedAt   string   `json:"createdAt"`
}

// BlobRef is an AT Proto blob reference as embedded in a record. The CID is
// in ref.$link; mimeType/size are metadata. A nil BlobRef means the field is
// unset on the record.
type BlobRef struct {
	Type     string `json:"$type"`
	Ref      Link   `json:"ref"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Link is the inner reference of a BlobRef; $link holds the content CID.
type Link struct {
	Link string `json:"$link"`
}

// BlobURL resolves a cached blob ref to the public getBlob URL on the PDS:
// {pdsURL}/xrpc/com.atproto.sync.getBlob?did={did}&cid={cid}. Returns "" when
// the blob ref is nil or has no CID, so callers can store/serve an empty
// string as "no image set".
func (b *BlobRef) BlobURL(pdsURL, did string) string {
	if b == nil || b.Ref.Link == "" {
		return ""
	}
	u, err := url.Parse(pdsURL)
	if err != nil {
		return ""
	}
	u.Path = "/xrpc/com.atproto.sync.getBlob"
	q := u.Query()
	q.Set("did", did)
	q.Set("cid", b.Ref.Link)
	u.RawQuery = q.Encode()
	return u.String()
}

// FormatTime formats a time.Time as an AT Proto datetime string (RFC3339).
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
