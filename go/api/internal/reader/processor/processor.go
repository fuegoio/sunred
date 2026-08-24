// Package processor orchestrates the feed refresh pipeline:
// fetch -> parse -> sanitize -> diff -> store.
package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/reader/fetcher"
	"github.com/fuegoio/sunred/go/api/internal/reader/parser"
	"github.com/fuegoio/sunred/go/api/internal/reader/sanitizer"
	"github.com/fuegoio/sunred/go/api/internal/store"
	"github.com/fuegoio/sunred/go/api/internal/urlnorm"
)

// Processor coordinates the fetch-parse-store pipeline for a single feed.
type Processor struct {
	store   *store.Store
	fetcher *fetcher.Fetcher
}

// New returns a Processor bound to the given store and fetcher.
func New(st *store.Store, f *fetcher.Fetcher) *Processor {
	return &Processor{store: st, fetcher: f}
}

// ProcessFeed refreshes a single feed: fetches its URL, parses the response,
// sanitizes the HTML content, and inserts new entries into the store.
func (p *Processor) ProcessFeed(ctx context.Context, feed *store.Feed) error {
	result, err := p.fetcher.Fetch(ctx, feed.FeedURL, feed.EtagHeader, feed.LastModified)
	if err != nil {
		if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, feed.EtagHeader, feed.LastModified, err.Error(), feed.ParsingErrorCount+1, time.Now().Add(15*time.Minute), ""); uerr != nil {
			slog.Warn("processor: persist fetch error state", "feed_id", feed.ID, "err", uerr)
		}
		return fmt.Errorf("fetch feed %d: %w", feed.ID, err)
	}

	if result.NotModified {
		// If the feed has no entries yet, the etag/last-modified were likely
		// set by a previous fetch that never stored entries (e.g. a subscribe
		// before the processor was wired in). Force a non-conditional fetch.
		count, countErr := p.store.CountEntriesByFeed(ctx, feed.ID)
		if countErr == nil && count == 0 {
			slog.Info("feed not modified but has no entries, forcing unconditional fetch", "feed_id", feed.ID, "url", feed.FeedURL)
			result, err = p.fetcher.Fetch(ctx, feed.FeedURL, "", "")
			if err != nil {
				if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, feed.EtagHeader, feed.LastModified, err.Error(), feed.ParsingErrorCount+1, time.Now().Add(15*time.Minute), ""); uerr != nil {
					slog.Warn("processor: persist fetch error state", "feed_id", feed.ID, "err", uerr)
				}
				return fmt.Errorf("fetch feed %d: %w", feed.ID, err)
			}
		} else {
			nextCheck := time.Now().Add(60 * time.Minute)
			if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, feed.EtagHeader, feed.LastModified, "", feed.ParsingErrorCount, nextCheck, ""); uerr != nil {
				slog.Warn("processor: persist not-modified state", "feed_id", feed.ID, "err", uerr)
			}
			slog.Info("feed not modified", "feed_id", feed.ID, "url", feed.FeedURL)
			return nil
		}
	}

	if result.NotModified {
		nextCheck := time.Now().Add(60 * time.Minute)
		if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, feed.EtagHeader, feed.LastModified, "", feed.ParsingErrorCount, nextCheck, ""); uerr != nil {
			slog.Warn("processor: persist not-modified state", "feed_id", feed.ID, "err", uerr)
		}
		slog.Info("feed not modified (unconditional fetch also returned 304)", "feed_id", feed.ID, "url", feed.FeedURL)
		return nil
	}

	parsed, err := parser.Parse(result.Body, result.ContentType)
	if err != nil {
		if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, feed.EtagHeader, feed.LastModified, err.Error(), feed.ParsingErrorCount+1, time.Now().Add(15*time.Minute), ""); uerr != nil {
			slog.Warn("processor: persist parse error state", "feed_id", feed.ID, "err", uerr)
		}
		return fmt.Errorf("parse feed %d: %w", feed.ID, err)
	}

	for _, item := range parsed.Items {
		content := item.Content
		if content == "" {
			content = item.Description
		}

		sanitized, err := sanitizer.Sanitize(content)
		if err != nil {
			slog.Warn("sanitize entry failed", "feed_id", feed.ID, "link", item.Link, "err", err)
			sanitized = content
		}

		publishedAt, err := parseDate(item.PublishedAt)
		if err != nil {
			publishedAt = time.Now()
		}

		hash := hashItem(item)
		description := truncate(sanitizer.StripHTML(item.Description), 400)
		entryID, err := p.store.CreateEntry(ctx, feed.ID, hash, item.Title, item.Link, item.CommentsURL, item.Author, sanitized, description, publishedAt, item.Tags)
		if err != nil {
			slog.Error("create entry failed", "feed_id", feed.ID, "hash", hash, "err", err)
			continue
		}

		if entryID > 0 {
			for _, enc := range item.Enclosures {
				if err := p.store.CreateEnclosure(ctx, entryID, enc.URL, enc.MimeType, enc.Size); err != nil {
					slog.Warn("create enclosure failed", "entry_id", entryID, "err", err)
				}
			}
		}
	}

	nextCheck := computeNextCheck(parsed.Items)
	if uerr := p.store.UpdateFeedFetchState(ctx, feed.ID, result.ETag, result.LastModified, "", 0, nextCheck, parsed.Description); uerr != nil {
		slog.Warn("processor: persist refresh state", "feed_id", feed.ID, "err", uerr)
	}
	slog.Info("feed refreshed", "feed_id", feed.ID, "url", feed.FeedURL, "items", len(parsed.Items))

	return nil
}

// hashItem computes the per-feed entry dedup hash from the article's
// normalized link, title, and publication date. The link is normalized so
// the same article is not duplicated within a feed when the publisher
// appends or strips tracking query parameters between refreshes.
func hashItem(item parser.Item) string {
	h := sha256.Sum256([]byte(urlnorm.URL(item.Link) + item.Title + item.PublishedAt))
	return hex.EncodeToString(h[:])
}

// ParseDate parses a date string using the formats supported by the feed
// processor. It is exported so the preview handler can reuse the same logic
// without duplicating the format list.
func ParseDate(s string) (time.Time, error) {
	return parseDate(s)
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	formats := []string{
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.ANSIC,
		time.UnixDate,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Now(), fmt.Errorf("unrecognized date format: %s", s)
}

// computeNextCheck estimates the next refresh time based on the average
// publication interval of recent items. Falls back to 60 minutes.
func computeNextCheck(items []parser.Item) time.Time {
	if len(items) < 2 {
		return time.Now().Add(60 * time.Minute)
	}

	var times []time.Time
	for _, item := range items {
		if t, err := parseDate(item.PublishedAt); err == nil {
			times = append(times, t)
		}
	}

	if len(times) < 2 {
		return time.Now().Add(60 * time.Minute)
	}

	// Sort descending
	for i := 0; i < len(times)-1; i++ {
		for j := i + 1; j < len(times); j++ {
			if times[j].After(times[i]) {
				times[i], times[j] = times[j], times[i]
			}
		}
	}

	var totalDiff time.Duration
	count := 0
	for i := 0; i < len(times)-1; i++ {
		diff := times[i].Sub(times[i+1])
		if diff > 0 {
			totalDiff += diff
			count++
		}
	}

	if count == 0 {
		return time.Now().Add(60 * time.Minute)
	}

	avgInterval := totalDiff / time.Duration(count)
	if avgInterval < 10*time.Minute {
		avgInterval = 10 * time.Minute
	}
	if avgInterval > 24*time.Hour {
		avgInterval = 24 * time.Hour
	}

	return time.Now().Add(avgInterval)
}

// truncate caps s at maxLen characters, appending an ellipsis if truncation
// occurred. If s is already maxLen or shorter it is returned unchanged.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
