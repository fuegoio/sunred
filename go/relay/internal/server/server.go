// Package server implements the Sunred relay HTTP server.
//
// Endpoints:
//
//	POST  /xrpc/io.sunred.relay.announceUser   — instance registers a new DID
//	GET   /xrpc/io.sunred.relay.getCounts      — global counts for a DID
//	GET   /xrpc/io.sunred.relay.subscribeEvents — WebSocket event stream for instances
//	GET   /health
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/net/websocket"

	"github.com/fuegoio/sunred/go/relay/internal/fanout"
	"github.com/fuegoio/sunred/go/relay/internal/store"
)

// Server is the relay HTTP server.
type Server struct {
	store  *store.Store
	fanout *fanout.Fanout
}

// New returns a Server.
func New(st *store.Store, f *fanout.Fanout) *Server {
	return &Server{store: st, fanout: f}
}

// Handler returns the http.Handler for the relay server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/xrpc/io.sunred.relay.announceUser", s.handleAnnounceUser)
	mux.HandleFunc("/xrpc/io.sunred.relay.getCounts", s.handleGetCounts)
	mux.HandleFunc("/xrpc/io.sunred.relay.searchDIDs", s.handleSearchDIDs)
	mux.HandleFunc("/xrpc/io.sunred.relay.resolveHandle", s.handleResolveHandle)
	mux.Handle("/xrpc/io.sunred.relay.subscribeEvents", websocket.Handler(s.handleSubscribeEvents))
	return s.logRequests(mux)
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests is an access-log middleware recording method, path, status,
// duration, and remote address for every request.
func (s *Server) logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		duration := time.Since(start)
		args := []any{
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", duration, "remote", r.RemoteAddr,
		}
		if q := r.URL.RawQuery; q != "" {
			args = append(args, "query", q)
		}
		switch {
		case rec.status >= 500:
			slog.Error("relay: request", args...)
		case rec.status >= 400:
			slog.Warn("relay: request", args...)
		default:
			slog.Info("relay: request", args...)
		}
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// --- announceUser ---

type announceUserInput struct {
	DID         string `json:"did"`
	PDSUrl      string `json:"pdsUrl"`
	InstanceURL string `json:"instanceUrl"`
	Handle      string `json:"handle"`
}

type announceUserOutput struct {
	Tracked bool `json:"tracked"`
	// New is true when the relay started tracking this DID for the first time
	// (and thus kicked off a backfill that will emit a backfillComplete event).
	// False on re-announce of an already-tracked DID — the API uses this to
	// avoid waiting for a backfill event that will never come.
	New bool `json:"new"`
}

func (s *Server) handleAnnounceUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var in announceUserInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if in.DID == "" || in.PDSUrl == "" || in.InstanceURL == "" {
		http.Error(w, "did, pdsUrl, and instanceUrl are required", http.StatusUnprocessableEntity)
		return
	}

	ctx := r.Context()

	instanceID, err := s.store.UpsertInstance(ctx, in.InstanceURL)
	if err != nil {
		slog.Error("relay: upsert instance", "instance", in.InstanceURL, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, isNew, err := s.store.UpsertTrackedDID(ctx, in.DID, in.PDSUrl, in.Handle, instanceID)
	if err != nil {
		slog.Error("relay: upsert tracked did", "did", in.DID, "pds", in.PDSUrl, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if isNew {
		slog.Info("relay: tracking new DID", "did", in.DID, "pds", in.PDSUrl, "instance", in.InstanceURL)
		// Backfill existing records from the PDS, then start the live
		// firehose subscription (tap-style backfill-then-cutover).
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("relay: backfill goroutine panic",
						"did", in.DID, "pds", in.PDSUrl, "panic", r)
				}
			}()
			s.fanout.BackfillAndSubscribe(context.Background(), in.DID, in.PDSUrl)
		}()
	} else {
		slog.Debug("relay: re-announce of already-tracked DID", "did", in.DID, "pds", in.PDSUrl, "instance", in.InstanceURL)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(announceUserOutput{Tracked: true, New: isNew})
}

// --- getCounts ---

type getCountsOutput struct {
	DID                   string `json:"did"`
	FollowerCount         int64  `json:"followerCount"`
	ShareCount            int64  `json:"shareCount"`
	FeedSubscriptionCount int64  `json:"feedSubscriptionCount"`
}

func (s *Server) handleGetCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	did := r.URL.Query().Get("did")
	if did == "" {
		http.Error(w, "did is required", http.StatusBadRequest)
		return
	}

	followers, shares, feedSubs, err := s.store.GetCounts(r.Context(), did)
	if err != nil {
		slog.Error("relay: get counts", "did", did, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(getCountsOutput{
		DID:                   did,
		FollowerCount:         followers,
		ShareCount:            shares,
		FeedSubscriptionCount: feedSubs,
	})
}

// --- searchDIDs ---

type searchDIDsOutput struct {
	Results []store.SearchResult `json:"results"`
}

func (s *Server) handleSearchDIDs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		} else {
			slog.Warn("relay: invalid limit, using default", "limit", limitStr, "default", limit)
		}
	}
	results, err := s.store.SearchDIDs(r.Context(), q, limit)
	if err != nil {
		slog.Error("relay: search dids", "q", q, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []store.SearchResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(searchDIDsOutput{Results: results})
}

// --- resolveHandle ---

type resolveHandleOutput struct {
	DID    string `json:"did"`
	PDSUrl string `json:"pdsUrl"`
}

func (s *Server) handleResolveHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	handle := r.URL.Query().Get("handle")
	if handle == "" {
		http.Error(w, "handle is required", http.StatusBadRequest)
		return
	}
	did, pdsURL, err := s.store.ResolveHandle(r.Context(), handle)
	if err != nil {
		slog.Error("relay: resolve handle", "handle", handle, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if did == "" {
		http.Error(w, "handle not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resolveHandleOutput{DID: did, PDSUrl: pdsURL})
}

// --- subscribeEvents ---
// to the subscribing instance. Supports cursor-based replay.
//
// To avoid the replay/subscribe race, the instance is registered for live
// events BEFORE replaying. Events that arrive during replay are held in the
// subscriber channel's buffer (capacity 256). After replay completes, the
// main read loop drains the buffer and continues with live events. Any
// events already replayed are skipped by seq comparison.
func (s *Server) handleSubscribeEvents(ws *websocket.Conn) {
	r := ws.Request()
	instanceURL := r.URL.Query().Get("instanceUrl")
	cursorStr := r.URL.Query().Get("cursor")

	var cursor int64
	if cursorStr != "" {
		c, err := strconv.ParseInt(cursorStr, 10, 64)
		if err != nil {
			slog.Warn("relay: invalid cursor, replaying from start", "instance", instanceURL, "cursor", cursorStr)
		} else {
			cursor = c
		}
	}

	slog.Info("relay: instance subscribed", "instance", instanceURL, "cursor", cursor)
	defer slog.Info("relay: instance disconnected", "instance", instanceURL)

	// Register for live events first so nothing is missed during replay.
	// The channel buffer (capacity 256) holds events emitted while we replay.
	ch := s.fanout.Subscribe(instanceURL)
	defer s.fanout.Unsubscribe(instanceURL)

	// Replay missed events from cursor.
	if cursor > 0 {
		replayed, err := s.replaySince(ws, cursor)
		if err != nil {
			slog.Warn("relay: replay error", "instance", instanceURL, "replayed", replayed, "err", err)
			return
		}
		slog.Info("relay: replay complete", "instance", instanceURL, "from_cursor", cursor, "events", replayed)
	}

	// Stream live events until the connection closes. Any events that were
	// emitted during replay are still in the channel buffer and will be
	// delivered here; skip ones already replayed by seq.
	var sent int64
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				slog.Info("relay: event channel closed", "instance", instanceURL)
				return
			}
			if evt.Seq <= cursor {
				continue // already replayed
			}
			if err := websocket.JSON.Send(ws, evt); err != nil {
				slog.Info("relay: live send failed, disconnecting", "instance", instanceURL, "seq", evt.Seq, "sent", sent, "err", err)
				return
			}
			cursor = evt.Seq
			sent++
		case <-time.After(30 * time.Second):
			// Send a keepalive ping frame.
			if err := websocket.JSON.Send(ws, map[string]string{"$type": "#ping"}); err != nil {
				slog.Info("relay: keepalive ping failed, disconnecting", "instance", instanceURL, "err", err)
				return
			}
		}
	}
}

// replaySince replays relay_events with seq > cursor in batches and returns
// the number of events sent to the client.
func (s *Server) replaySince(ws *websocket.Conn, cursor int64) (int, error) {
	const batchSize = 200
	var total int
	for {
		events, err := s.store.ListEventsSince(context.Background(), cursor, batchSize)
		if err != nil {
			return total, err
		}
		for _, evt := range events {
			if err := websocket.JSON.Send(ws, evt); err != nil {
				slog.Info("relay: replay send failed", "seq", evt.Seq, "sent", total, "err", err)
				return total, err
			}
			cursor = evt.Seq
			total++
		}
		if len(events) < batchSize {
			break
		}
	}
	return total, nil
}
