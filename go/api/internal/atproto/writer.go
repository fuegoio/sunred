package atproto

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// NSIDs for the record operations the Writer performs.
var (
	nsidPutRecord    = mustNSID("com.atproto.repo.putRecord")
	nsidDeleteRecord = mustNSID("com.atproto.repo.deleteRecord")
)

func mustNSID(s string) syntax.NSID {
	n, err := syntax.ParseNSID(s)
	if err != nil {
		panic(err)
	}
	return n
}

// writerHTTPClient is the HTTP client used by Writer when the supplied
// APIClient has none set. A bounded timeout keeps an unresponsive PDS from
// blocking the fire-and-forget sync goroutines forever.
var writerHTTPClient = &http.Client{Timeout: 20 * time.Second}

// Writer performs high-level AT Proto record writes for a single user's repo.
// It wraps an authenticated atclient.APIClient (the indigo OAuth session's
// DPoP-bound client) with the user's DID.
type Writer struct {
	client *atclient.APIClient
	did    string
}

// NewWriter returns a Writer that writes to the user's PDS through the given
// APIClient. In production the client is the user's OAuth session client
// (oauth.ClientSession.APIClient()) so writes carry the DPoP proof the PDS
// requires. For tests, an unauthenticated atclient.NewAPIClient(pdsURL) is
// enough against a mock PDS that ignores auth.
func NewWriter(c *atclient.APIClient, did string) *Writer {
	if c != nil && c.Client == nil {
		c.Client = writerHTTPClient
	}
	return &Writer{client: c, did: did}
}

// PutFollow writes an io.sunred.graph.follow record and returns the rkey.
func (w *Writer) PutFollow(ctx context.Context, subjectDID string) (string, error) {
	rkey := NewTID()
	_, err := w.putRecord(ctx, CollectionFollow, rkey, FollowRecord{
		Type:      CollectionFollow,
		Subject:   subjectDID,
		CreatedAt: FormatTime(time.Now()),
	})
	if err != nil {
		return "", fmt.Errorf("put follow: %w", err)
	}
	return rkey, nil
}

// DeleteFollow removes the io.sunred.graph.follow record at rkey.
func (w *Writer) DeleteFollow(ctx context.Context, rkey string) error {
	return w.deleteRecord(ctx, CollectionFollow, rkey)
}

// PutShare writes an io.sunred.share.article record and returns the rkey.
func (w *Writer) PutShare(ctx context.Context,
	articleURL, title, description,
	feedURL, feedTitle, feedSiteURL, author string,
	publishedAt *time.Time, sharedAt time.Time,
) (string, error) {
	rkey := NewTID()
	rec := ShareRecord{
		Type:        CollectionShare,
		ArticleURL:  articleURL,
		Title:       title,
		Description: description,
		FeedURL:     feedURL,
		FeedTitle:   feedTitle,
		FeedSiteURL: feedSiteURL,
		Author:      author,
		SharedAt:    FormatTime(sharedAt),
	}
	if publishedAt != nil {
		rec.PublishedAt = FormatTime(*publishedAt)
	}
	_, err := w.putRecord(ctx, CollectionShare, rkey, rec)
	if err != nil {
		return "", fmt.Errorf("put share: %w", err)
	}
	return rkey, nil
}

// DeleteShare removes the io.sunred.share.article record at rkey.
func (w *Writer) DeleteShare(ctx context.Context, rkey string) error {
	return w.deleteRecord(ctx, CollectionShare, rkey)
}

// PutFeedSubscription writes an io.sunred.feed.subscription record
// and returns the rkey.
func (w *Writer) PutFeedSubscription(ctx context.Context, feedURL, siteURL, title string, createdAt time.Time) (string, error) {
	rkey := NewTID()
	_, err := w.putRecord(ctx, CollectionSubscription, rkey, SubscriptionRecord{
		Type:      CollectionSubscription,
		FeedURL:   feedURL,
		SiteURL:   siteURL,
		Title:     title,
		CreatedAt: FormatTime(createdAt),
	})
	if err != nil {
		return "", fmt.Errorf("put feed subscription: %w", err)
	}
	return rkey, nil
}

// DeleteFeedSubscription removes the io.sunred.feed.subscription record.
func (w *Writer) DeleteFeedSubscription(ctx context.Context, rkey string) error {
	return w.deleteRecord(ctx, CollectionSubscription, rkey)
}

// putRecord creates or replaces a record in the user's repo via the OAuth
// session's APIClient (DPoP-bound). Returns the record URI.
func (w *Writer) putRecord(ctx context.Context, collection, rkey string, record any) (string, error) {
	if w.client == nil {
		return "", errNoWriterClient
	}
	var out PutRecordOutput
	if err := w.client.Post(ctx, nsidPutRecord, PutRecordInput{
		Repo:       w.did,
		Collection: collection,
		Rkey:       rkey,
		Record:     record,
	}, &out); err != nil {
		return "", err
	}
	return out.URI, nil
}

// deleteRecord removes a record from the repo via the OAuth session's
// APIClient. A missing record is treated as success by the PDS.
func (w *Writer) deleteRecord(ctx context.Context, collection, rkey string) error {
	if w.client == nil {
		return errNoWriterClient
	}
	return w.client.Post(ctx, nsidDeleteRecord, DeleteRecordInput{
		Repo:       w.did,
		Collection: collection,
		Rkey:       rkey,
	}, nil)
}

var errNoWriterClient = fmt.Errorf("atproto: writer has no PDS client")
