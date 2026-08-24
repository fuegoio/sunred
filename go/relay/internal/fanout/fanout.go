// Package fanout manages PDS WebSocket subscriptions and aggregates io.sunred.* events.
package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/gorilla/websocket"
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/fuegoio/sunred/go/relay/internal/store"
)

// fanoutStore is the subset of store.Store that the Fanout uses.
// It is an interface so tests can inject a stub implementation.
type fanoutStore interface {
	ListActiveTrackedDIDs(ctx context.Context) ([]store.TrackedDID, error)
	UpdateTrackedDIDCursor(ctx context.Context, did string, seq int64) error
	SetTrackedDIDError(ctx context.Context, did, msg string) error
	RecordFollow(ctx context.Context, followerDID, followeeDID, rkey, pdsURL string, createdAt time.Time) (bool, error)
	DeleteFollow(ctx context.Context, followerDID, rkey string) (string, bool, error)
	RecordShare(ctx context.Context, did, rkey, articleURL, feedURL, title, pdsURL string, sharedAt *time.Time) (bool, error)
	DeleteShare(ctx context.Context, did, rkey string) (bool, error)
	RecordFeedSubscription(ctx context.Context, did, rkey, feedURL, pdsURL string, createdAt *time.Time) (bool, error)
	DeleteFeedSubscription(ctx context.Context, did, rkey string) (bool, error)
	RecordStar(ctx context.Context, did, rkey, articleURL, pdsURL string) (bool, error)
	DeleteStar(ctx context.Context, did, rkey string) (bool, error)
	AppendEvent(ctx context.Context, eventType, did string, payload any) (int64, error)
}

// Fanout manages one goroutine per tracked DID that subscribes to its PDS repo stream.
type Fanout struct {
	store          fanoutStore
	reconnectDelay time.Duration
	httpClient     *http.Client
	subscribers    map[string]chan *store.RelayEvent
	subsMu         sync.RWMutex
	workers        map[string]context.CancelFunc
	wmu            sync.Mutex
	// testPDSURL overrides the PDS URL used for fetchRecord calls in unit tests.
	// In production this is always empty and the DID's real pdsURL is used.
	testPDSURL string
}

// New returns a Fanout.
func New(st *store.Store, reconnectDelay time.Duration) *Fanout {
	return &Fanout{
		store:          st,
		reconnectDelay: reconnectDelay,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		subscribers:    make(map[string]chan *store.RelayEvent),
		workers:        make(map[string]context.CancelFunc),
	}
}

// Start loads all active tracked DIDs and begins subscribing. Blocks until ctx is done.
func (f *Fanout) Start(ctx context.Context) {
	dids, err := f.store.ListActiveTrackedDIDs(ctx)
	if err != nil {
		slog.Error("fanout: list active dids", "err", err)
		return
	}
	slog.Info("fanout: starting", "tracked_dids", len(dids))
	for _, d := range dids {
		f.EnsureSubscribed(ctx, d.DID, d.PDSUrl, d.CursorSeq)
	}
	<-ctx.Done()
	slog.Info("fanout: stopping", "tracked_dids", len(dids))
}

// EnsureSubscribed starts a subscription goroutine for did if none is running.
func (f *Fanout) EnsureSubscribed(parentCtx context.Context, did, pdsURL string, cursorSeq int64) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	if _, ok := f.workers[did]; ok {
		return
	}
	workerCtx, cancel := context.WithCancel(parentCtx)
	f.workers[did] = cancel
	go f.runWorker(workerCtx, did, pdsURL, cursorSeq)
}

// Subscribe registers an instance listener and returns its event channel.
func (f *Fanout) Subscribe(instanceURL string) chan *store.RelayEvent {
	ch := make(chan *store.RelayEvent, 256)
	f.subsMu.Lock()
	f.subscribers[instanceURL] = ch
	f.subsMu.Unlock()
	return ch
}

// Unsubscribe removes an instance listener.
func (f *Fanout) Unsubscribe(instanceURL string) {
	f.subsMu.Lock()
	delete(f.subscribers, instanceURL)
	f.subsMu.Unlock()
}

func (f *Fanout) runWorker(ctx context.Context, did, pdsURL string, cursorSeq int64) {
	slog.Info("fanout: starting worker", "did", did, "pds", pdsURL, "cursor", cursorSeq)
	for {
		select {
		case <-ctx.Done():
			slog.Info("fanout: worker stopped", "did", did)
			return
		default:
		}
		if err := f.subscribe(ctx, did, pdsURL, &cursorSeq); err != nil && ctx.Err() == nil {
			slog.Warn("fanout: subscription error", "did", did, "err", err)
			_ = f.store.SetTrackedDIDError(context.Background(), did, err.Error())
		}
		select {
		case <-ctx.Done():
			slog.Info("fanout: worker stopped", "did", did)
			return
		case <-time.After(f.reconnectDelay):
		}
	}
}

// repoOp describes a single record mutation inside a commit event.
type repoOp struct {
	Action string // create | update | delete
	Path   string // <collection>/<rkey>
}

func (f *Fanout) subscribe(ctx context.Context, did, pdsURL string, cursorSeq *int64) error {
	u, err := url.Parse(pdsURL)
	if err != nil {
		return fmt.Errorf("parse pds url: %w", err)
	}
	wsScheme := "wss"
	if u.Scheme == "http" {
		wsScheme = "ws"
	}
	wsURL := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?wantedDids=%s",
		wsScheme, u.Host, url.QueryEscape(did))
	if *cursorSeq > 0 {
		wsURL += fmt.Sprintf("&cursor=%d", *cursorSeq)
		slog.Info("fanout: resuming subscription from cursor", "did", did, "cursor", *cursorSeq)
	}
	dialer := websocket.Dialer{}
	ws, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = ws.Close() }()
	slog.Info("fanout: connected to PDS", "did", did)
	cr := cbg.NewCborReader(nil)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		mt, reader, err := ws.NextReader()
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		if mt != websocket.BinaryMessage {
			return fmt.Errorf("receive: expected binary message, got text")
		}
		cr.SetReader(reader)
		var header events.EventHeader
		if err := header.UnmarshalCBOR(cr); err != nil {
			return fmt.Errorf("decode header: %w", err)
		}
		switch header.Op {
		case events.EvtKindMessage:
			// handled below
		case events.EvtKindErrorFrame:
			var errFrame events.ErrorFrame
			if e := errFrame.UnmarshalCBOR(cr); e == nil && errFrame.Error != "" {
				return fmt.Errorf("receive: stream error: %s: %s", errFrame.Error, errFrame.Message)
			}
			return fmt.Errorf("receive: stream error")
		default:
			continue
		}
		if header.MsgType != "#commit" {
			continue
		}
		var evt comatproto.SyncSubscribeRepos_Commit
		if err := evt.UnmarshalCBOR(cr); err != nil {
			slog.Warn("fanout: decode commit", "did", did, "err", err)
			continue
		}
		for _, op := range evt.Ops {
			f.processOp(context.Background(), evt.Repo, pdsURL, repoOp{
				Action: op.Action,
				Path:   op.Path,
			})
		}
		if evt.Seq > 0 {
			if err := f.store.UpdateTrackedDIDCursor(context.Background(), did, evt.Seq); err != nil {
				slog.Warn("fanout: update tracked did cursor", "did", did, "seq", evt.Seq, "err", err)
			}
			*cursorSeq = evt.Seq
		}
	}
}

func (f *Fanout) processOp(ctx context.Context, did, pdsURL string, op repoOp) {
	parts := strings.SplitN(op.Path, "/", 2)
	if len(parts) != 2 {
		slog.Warn("fanout: malformed op path", "did", did, "path", op.Path)
		return
	}
	col, rkey := parts[0], parts[1]
	// Allow test hook to override the PDS URL for fetchRecord calls.
	effectivePDS := pdsURL
	if f.testPDSURL != "" {
		effectivePDS = f.testPDSURL
	}
	switch col {
	case "io.sunred.graph.follow":
		f.handleFollow(ctx, did, effectivePDS, rkey, op.Action)
	case "io.sunred.share.article":
		f.handleShare(ctx, did, effectivePDS, rkey, op.Action)
	case "io.sunred.feed.subscription":
		f.handleFeedSub(ctx, did, effectivePDS, rkey, op.Action)
	case "io.sunred.entry.star":
		f.handleStar(ctx, did, effectivePDS, rkey, op.Action)
	default:
		slog.Debug("fanout: unknown collection", "did", did, "collection", col, "rkey", rkey)
	}
}

func (f *Fanout) handleFollow(ctx context.Context, did, pdsURL, rkey, action string) {
	if action == "delete" {
		followeeDID, deleted, err := f.store.DeleteFollow(ctx, did, rkey)
		if err != nil {
			slog.Warn("fanout: delete follow", "did", did, "rkey", rkey, "err", err)
			return
		}
		if deleted {
			f.emit(ctx, "unfollow", did, map[string]any{"did": did, "subjectDid": followeeDID, "rkey": rkey})
		}
		return
	}
	rec, err := f.fetchRecord(ctx, pdsURL, did, "io.sunred.graph.follow", rkey)
	if err != nil {
		slog.Warn("fanout: fetch follow", "did", did, "rkey", rkey, "err", err)
		return
	}
	f.processFollowRecord(ctx, did, pdsURL, rkey, rec)
}

// processFollowRecord records a follow and emits an event. Shared by the live
// firehose path (after fetchRecord) and the backfill path (with the record
// value from listRecords).
func (f *Fanout) processFollowRecord(ctx context.Context, did, pdsURL, rkey string, rec map[string]any) {
	subject, _ := rec["subject"].(string)
	if subject == "" {
		slog.Warn("fanout: follow record missing subject", "did", did, "rkey", rkey)
		return
	}
	createdAt := parseTime(rec["createdAt"])
	isNew, err := f.store.RecordFollow(ctx, did, subject, rkey, pdsURL, createdAt)
	if err != nil {
		slog.Warn("fanout: record follow", "did", did, "subject", subject, "rkey", rkey, "err", err)
		return
	}
	if isNew {
		f.emit(ctx, "follow", did, map[string]any{
			"did": did, "subjectDid": subject, "rkey": rkey, "createdAt": createdAt,
		})
	}
}

func (f *Fanout) handleShare(ctx context.Context, did, pdsURL, rkey, action string) {
	if action == "delete" {
		deleted, err := f.store.DeleteShare(ctx, did, rkey)
		if err != nil {
			slog.Warn("fanout: delete share", "did", did, "rkey", rkey, "err", err)
			return
		}
		if deleted {
			f.emit(ctx, "unshare", did, map[string]any{"did": did, "rkey": rkey})
		}
		return
	}
	rec, err := f.fetchRecord(ctx, pdsURL, did, "io.sunred.share.article", rkey)
	if err != nil {
		slog.Warn("fanout: fetch share", "did", did, "rkey", rkey, "err", err)
		return
	}
	f.processShareRecord(ctx, did, pdsURL, rkey, rec)
}

// processShareRecord records a share and emits an event. Shared by the live
// firehose path and the backfill path.
func (f *Fanout) processShareRecord(ctx context.Context, did, pdsURL, rkey string, rec map[string]any) {
	articleURL, _ := rec["articleUrl"].(string)
	feedURL, _ := rec["feedUrl"].(string)
	title, _ := rec["title"].(string)
	description, _ := rec["description"].(string)
	feedTitle, _ := rec["feedTitle"].(string)
	feedSiteURL, _ := rec["feedSiteUrl"].(string)
	author, _ := rec["author"].(string)
	sharedAt := parseTime(rec["sharedAt"])
	var publishedAt *time.Time
	if v, ok := rec["publishedAt"].(string); ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			utc := t.UTC()
			publishedAt = &utc
		} else {
			slog.Warn("fanout: parse publishedAt", "did", did, "rkey", rkey, "publishedAt", v, "err", err)
		}
	}
	isNew, err := f.store.RecordShare(ctx, did, rkey, articleURL, feedURL, title, pdsURL, &sharedAt)
	if err != nil {
		slog.Warn("fanout: record share", "did", did, "rkey", rkey, "err", err)
		return
	}
	if isNew {
		payload := map[string]any{
			"did": did, "rkey": rkey, "articleUrl": articleURL,
			"feedUrl": feedURL, "title": title, "description": description,
			"feedTitle": feedTitle, "feedSiteUrl": feedSiteURL,
			"author": author, "sharedAt": sharedAt,
		}
		if publishedAt != nil {
			payload["publishedAt"] = publishedAt.Format(time.RFC3339)
		}
		f.emit(ctx, "share", did, payload)
	}
}

func (f *Fanout) handleFeedSub(ctx context.Context, did, pdsURL, rkey, action string) {
	if action == "delete" {
		deleted, err := f.store.DeleteFeedSubscription(ctx, did, rkey)
		if err != nil {
			slog.Warn("fanout: delete feed sub", "did", did, "rkey", rkey, "err", err)
			return
		}
		if deleted {
			f.emit(ctx, "feedUnsubscription", did, map[string]any{"did": did, "rkey": rkey})
		}
		return
	}
	rec, err := f.fetchRecord(ctx, pdsURL, did, "io.sunred.feed.subscription", rkey)
	if err != nil {
		slog.Warn("fanout: fetch feed sub", "did", did, "rkey", rkey, "err", err)
		return
	}
	f.processFeedSubRecord(ctx, did, pdsURL, rkey, rec)
}

// processFeedSubRecord records a feed subscription and emits an event with the
// full record metadata (feedUrl, siteUrl, title). Shared by the live firehose
// path and the backfill path.
func (f *Fanout) processFeedSubRecord(ctx context.Context, did, pdsURL, rkey string, rec map[string]any) {
	feedURL, _ := rec["feedUrl"].(string)
	if feedURL == "" {
		slog.Warn("fanout: feed sub record missing feedUrl", "did", did, "rkey", rkey)
		return
	}
	siteURL, _ := rec["siteUrl"].(string)
	title, _ := rec["title"].(string)
	createdAt := parseTime(rec["createdAt"])
	isNew, err := f.store.RecordFeedSubscription(ctx, did, rkey, feedURL, pdsURL, &createdAt)
	if err != nil {
		slog.Warn("fanout: record feed sub", "did", did, "rkey", rkey, "err", err)
		return
	}
	if isNew {
		f.emit(ctx, "feedSubscription", did, map[string]any{
			"did": did, "rkey": rkey, "feedUrl": feedURL,
			"siteUrl": siteURL, "title": title, "createdAt": createdAt,
		})
	}
}

func (f *Fanout) handleStar(ctx context.Context, did, pdsURL, rkey, action string) {
	if action == "delete" {
		deleted, err := f.store.DeleteStar(ctx, did, rkey)
		if err != nil {
			slog.Warn("fanout: delete star", "did", did, "rkey", rkey, "err", err)
			return
		}
		if deleted {
			f.emit(ctx, "unstar", did, map[string]any{"did": did, "rkey": rkey})
		}
		return
	}
	rec, err := f.fetchRecord(ctx, pdsURL, did, "io.sunred.entry.star", rkey)
	if err != nil {
		slog.Warn("fanout: fetch star", "did", did, "rkey", rkey, "err", err)
		return
	}
	articleURL, _ := rec["articleUrl"].(string)
	if articleURL == "" {
		slog.Warn("fanout: star record missing articleUrl", "did", did, "rkey", rkey)
		return
	}
	isNew, err := f.store.RecordStar(ctx, did, rkey, articleURL, pdsURL)
	if err != nil {
		slog.Warn("fanout: record star", "did", did, "rkey", rkey, "err", err)
		return
	}
	if isNew {
		f.emit(ctx, "star", did, map[string]any{
			"did": did, "rkey": rkey, "articleUrl": articleURL,
		})
	}
}

func (f *Fanout) emit(ctx context.Context, eventType, did string, payload any) {
	seq, err := f.store.AppendEvent(ctx, eventType, did, payload)
	if err != nil {
		slog.Warn("fanout: append event", "eventType", eventType, "did", did, "err", err)
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("fanout: marshal event payload", "eventType", eventType, "did", did, "err", err)
	}
	evt := &store.RelayEvent{Seq: seq, EventType: eventType, DID: did, Payload: b}
	f.subsMu.RLock()
	defer f.subsMu.RUnlock()
	for instance, ch := range f.subscribers {
		select {
		case ch <- evt:
		default:
			slog.Warn("fanout: subscriber channel full, dropping event",
				"instance", instance, "seq", seq, "eventType", eventType, "did", did)
		}
	}
}

type getRecordResp struct {
	Value map[string]any `json:"value"`
}

func (f *Fanout) fetchRecord(ctx context.Context, pdsURL, did, collection, rkey string) (map[string]any, error) {
	u := fmt.Sprintf("%s/xrpc/com.atproto.repo.getRecord?repo=%s&collection=%s&rkey=%s",
		pdsURL, url.QueryEscape(did), url.QueryEscape(collection), url.QueryEscape(rkey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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
		return nil, fmt.Errorf("read getRecord response: %w", err)
	}
	if resp.StatusCode >= 400 {
		slog.Warn("fanout: pds getRecord error", "did", did, "collection", collection, "rkey", rkey, "status", resp.StatusCode)
		return nil, fmt.Errorf("pds returned %d for %s/%s", resp.StatusCode, collection, rkey)
	}
	var out getRecordResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

func parseTime(v any) time.Time {
	s, _ := v.(string)
	if s == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		slog.Warn("fanout: parse timestamp failed, using now", "value", s, "err", err)
		return time.Now().UTC()
	}
	return t.UTC()
}
