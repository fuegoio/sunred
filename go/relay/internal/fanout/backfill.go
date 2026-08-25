package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
)

// backfillCollections are the collections to crawl from the PDS repo during the
// historical backfill phase: the io.sunred.* social-graph collections plus the
// Bluesky profile record (app.bsky.actor.profile) so the relay caches display
// name, bio, and avatar/banner for tracked DIDs.
var backfillCollections = []string{
	"app.bsky.actor.profile",
	"io.sunred.graph.follow",
	"io.sunred.feed.subscription",
	"io.sunred.share.article",
}

// BackfillAndSubscribe crawls existing records from the user's PDS repo and
// emits events for each one, then starts the live firehose subscription.
// This is the tap-style backfill-then-cutover model: historical records are
// processed first, then live events are streamed.
func (f *Fanout) BackfillAndSubscribe(ctx context.Context, did, pdsURL string) {
	slog.Info("fanout: backfilling DID from PDS", "did", did, "pds", pdsURL)
	f.backfillDID(ctx, did, pdsURL)
	// Signal that the backfill is complete so instances can mark the sync as
	// done and dismiss any waiting UI.
	f.emit(ctx, "backfillComplete", did, map[string]any{"did": did})
	slog.Info("fanout: backfill complete, starting live subscription", "did", did)
	f.EnsureSubscribed(ctx, did, pdsURL, 0)
}

func (f *Fanout) backfillDID(ctx context.Context, did, pdsURL string) {
	for _, col := range backfillCollections {
		n, err := f.backfillCollection(ctx, did, pdsURL, col)
		if err != nil {
			slog.Warn("backfill: collection failed", "did", did, "collection", col, "err", err)
			continue
		}
		slog.Info("backfill: collection complete", "did", did, "collection", col, "records", n)
	}
}

// backfillCollection crawls a single collection via listRecords pagination and
// returns the number of records processed.
func (f *Fanout) backfillCollection(ctx context.Context, did, pdsURL, collection string) (int, error) {
	cursor := ""
	count := 0
	for {
		out, err := f.listRecords(ctx, pdsURL, did, collection, cursor)
		if err != nil {
			return count, fmt.Errorf("list records %s: %w", collection, err)
		}
		for _, rec := range out.Records {
			rkey := rkeyFromURI(rec.URI)
			f.processBackfillRecord(ctx, did, pdsURL, collection, rkey, rec.Value)
			count++
		}
		if out.Cursor == "" {
			break
		}
		cursor = out.Cursor
	}
	return count, nil
}

// listRecordsOutput matches com.atproto.repo.listRecords response.
type listRecordsOutput struct {
	Records []struct {
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	} `json:"records"`
	Cursor string `json:"cursor"`
}

func (f *Fanout) listRecords(ctx context.Context, pdsURL, did, collection, cursor string) (*listRecordsOutput, error) {
	u, err := url.Parse(pdsURL)
	if err != nil {
		return nil, fmt.Errorf("parse pds url: %w", err)
	}
	u.Path = "/xrpc/com.atproto.repo.listRecords"
	q := u.Query()
	q.Set("repo", did)
	q.Set("collection", collection)
	q.Set("limit", "100")
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read listRecords response: %w", err)
	}
	if resp.StatusCode >= 400 {
		slog.Warn("backfill: pds listRecords error", "did", did, "collection", collection, "status", resp.StatusCode)
		return nil, fmt.Errorf("pds returned %d for listRecords %s", resp.StatusCode, collection)
	}
	var out listRecordsOutput
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode listRecords response: %w", err)
	}
	return &out, nil
}

func (f *Fanout) processBackfillRecord(ctx context.Context, did, pdsURL, collection, rkey string, raw json.RawMessage) {
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		slog.Warn("backfill: unmarshal record", "collection", collection, "rkey", rkey, "err", err)
		return
	}
	switch collection {
	case "app.bsky.actor.profile":
		f.processProfileRecord(ctx, did, pdsURL, rec)
	case "io.sunred.graph.follow":
		f.processFollowRecord(ctx, did, pdsURL, rkey, rec)
	case "io.sunred.share.article":
		f.processShareRecord(ctx, did, pdsURL, rkey, rec)
	case "io.sunred.feed.subscription":
		f.processFeedSubRecord(ctx, did, pdsURL, rkey, rec)
	default:
		slog.Warn("backfill: unknown collection", "did", did, "collection", collection, "rkey", rkey)
	}
}

func rkeyFromURI(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '/' {
			return uri[i+1:]
		}
	}
	return uri
}
