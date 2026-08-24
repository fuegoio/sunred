package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"golang.org/x/net/websocket"

	"github.com/fuegoio/sunred/go/api/internal/store"
)

// relayEvent is the JSON shape of a relay subscribeEvents frame, matching
// go/relay/internal/store.RelayEvent. Field names are capitalised because
// the relay struct has no json tags.
type relayEvent struct {
	Seq       int64           `json:"Seq"`
	EventType string          `json:"EventType"`
	DID       string          `json:"DID"`
	Payload   json.RawMessage `json:"Payload"`
	CreatedAt time.Time       `json:"CreatedAt"`
}

// RelayConsumer connects to the relay's subscribeEvents WebSocket and
// processes events into the local cache. It persists a cursor so it can
// resume after a restart without losing events.
type RelayConsumer struct {
	store       *store.Store
	relayURL    string
	instanceURL string
}

// NewRelayConsumer creates a consumer that connects to relayURL and
// identifies as instanceURL.
func NewRelayConsumer(st *store.Store, relayURL, instanceURL string) *RelayConsumer {
	return &RelayConsumer{store: st, relayURL: relayURL, instanceURL: instanceURL}
}

// Start connects to the relay and processes events until ctx is cancelled.
// Reconnects automatically on disconnection with a backoff delay.
func (c *RelayConsumer) Start(ctx context.Context) {
	slog.Info("relay consumer: starting", "relay", c.relayURL, "instance", c.instanceURL)
	for {
		select {
		case <-ctx.Done():
			slog.Info("relay consumer: stopped")
			return
		default:
		}
		if err := c.connect(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("relay consumer: disconnected, reconnecting", "err", err, "backoff", "5s")
		}
		select {
		case <-ctx.Done():
			slog.Info("relay consumer: stopped")
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *RelayConsumer) connect(ctx context.Context) error {
	_, cursor, err := c.store.GetRelayCursor(ctx)
	if err != nil {
		slog.Warn("relay consumer: get cursor", "err", err)
	}

	u, err := url.Parse(c.relayURL)
	if err != nil {
		return fmt.Errorf("parse relay url: %w", err)
	}
	wsScheme := "wss"
	if u.Scheme == "http" {
		wsScheme = "ws"
	}
	wsURL := fmt.Sprintf("%s://%s/xrpc/io.sunred.relay.subscribeEvents?instanceUrl=%s",
		wsScheme, u.Host, url.QueryEscape(c.instanceURL))
	if cursor > 0 {
		wsURL += fmt.Sprintf("&cursor=%d", cursor)
	}

	ws, err := websocket.Dial(wsURL, "", c.relayURL)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	defer func() { _ = ws.Close() }()
	slog.Info("relay consumer: connected", "cursor", cursor)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		var evt relayEvent
		if err := websocket.JSON.Receive(ws, &evt); err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		// Skip keepalive pings (no EventType).
		if evt.EventType == "" {
			continue
		}
		if err := c.processEvent(ctx, &evt); err != nil {
			slog.Warn("relay consumer: process event",
				"seq", evt.Seq, "type", evt.EventType, "did", evt.DID, "err", err)
		}
		if evt.Seq > 0 {
			if err := c.store.UpdateRelayCursor(ctx, c.relayURL, evt.Seq); err != nil {
				slog.Warn("relay consumer: update cursor", "seq", evt.Seq, "err", err)
			}
		}
	}
}

func (c *RelayConsumer) processEvent(ctx context.Context, evt *relayEvent) error {
	switch evt.EventType {
	case "follow":
		return c.handleFollowEvent(ctx, evt)
	case "unfollow":
		return c.handleUnfollowEvent(ctx, evt)
	case "feedSubscription":
		return c.handleFeedSubEvent(ctx, evt)
	case "feedUnsubscription":
		return c.handleFeedUnsubEvent(ctx, evt)
	case "share":
		return c.handleShareEvent(ctx, evt)
	case "unshare":
		return c.handleUnshareEvent(ctx, evt)
	case "star":
		return c.handleStarEvent(ctx, evt)
	case "unstar":
		return c.handleUnstarEvent(ctx, evt)
	case "backfillComplete":
		return c.handleBackfillComplete(ctx, evt)
	default:
		slog.Warn("relay consumer: unknown event type", "type", evt.EventType)
		return nil
	}
}

type followPayload struct {
	SubjectDid string `json:"subjectDid"`
	Rkey       string `json:"rkey"`
	CreatedAt  string `json:"createdAt"`
}

func (c *RelayConsumer) handleFollowEvent(ctx context.Context, evt *relayEvent) error {
	var p followPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal follow payload: %w", err)
	}
	followerID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if followerID == 0 {
		return nil // follower not on this instance
	}
	followeeID, _ := c.store.GetUserIDByDID(ctx, p.SubjectDid)
	if followeeID == 0 {
		return nil // followee not on this instance
	}
	return c.store.UpsertFollowWithRkey(ctx, followerID, followeeID, p.Rkey)
}

func (c *RelayConsumer) handleUnfollowEvent(ctx context.Context, evt *relayEvent) error {
	var p followPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal unfollow payload: %w", err)
	}
	followerID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if followerID == 0 {
		return nil
	}
	return c.store.DeleteFollowByRkey(ctx, followerID, p.Rkey)
}

type feedSubPayload struct {
	Rkey      string `json:"rkey"`
	FeedURL   string `json:"feedUrl"`
	SiteURL   string `json:"siteUrl"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
}

func (c *RelayConsumer) handleFeedSubEvent(ctx context.Context, evt *relayEvent) error {
	var p feedSubPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal feedSubscription payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	return c.store.UpsertFeedSubscriptionWithRkey(ctx, userID, p.FeedURL, p.SiteURL, p.Title, p.Rkey)
}

func (c *RelayConsumer) handleFeedUnsubEvent(ctx context.Context, evt *relayEvent) error {
	var p struct {
		Rkey string `json:"rkey"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal feedUnsubscription payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	return c.store.DeleteFeedByRkey(ctx, userID, p.Rkey)
}

type sharePayload struct {
	Rkey        string `json:"rkey"`
	ArticleURL  string `json:"articleUrl"`
	Title       string `json:"title"`
	Description string `json:"description"`
	FeedURL     string `json:"feedUrl"`
	FeedTitle   string `json:"feedTitle"`
	FeedSiteURL string `json:"feedSiteUrl"`
	Author      string `json:"author"`
	PublishedAt string `json:"publishedAt"`
	SharedAt    string `json:"sharedAt"`
}

func (c *RelayConsumer) handleShareEvent(ctx context.Context, evt *relayEvent) error {
	var p sharePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal share payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	var publishedAt *time.Time
	if p.PublishedAt != "" {
		t, err := time.Parse(time.RFC3339, p.PublishedAt)
		if err == nil {
			utc := t.UTC()
			publishedAt = &utc
		}
	}
	return c.store.UpsertShareWithRkey(ctx, userID,
		p.ArticleURL, p.Title, p.Description,
		p.FeedURL, p.FeedTitle, p.FeedSiteURL,
		p.Author, publishedAt, p.Rkey,
	)
}

func (c *RelayConsumer) handleUnshareEvent(ctx context.Context, evt *relayEvent) error {
	var p struct {
		Rkey string `json:"rkey"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal unshare payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	return c.store.DeleteShareByRkey(ctx, userID, p.Rkey)
}

type starPayload struct {
	Rkey       string `json:"rkey"`
	ArticleURL string `json:"articleUrl"`
	CreatedAt  string `json:"createdAt"`
}

func (c *RelayConsumer) handleStarEvent(ctx context.Context, evt *relayEvent) error {
	var p starPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal star payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	return c.store.UpsertStarWithRkey(ctx, userID, p.ArticleURL, p.Rkey)
}

func (c *RelayConsumer) handleUnstarEvent(ctx context.Context, evt *relayEvent) error {
	var p struct {
		Rkey string `json:"rkey"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal unstar payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, evt.DID)
	if userID == 0 {
		return nil
	}
	return c.store.DeleteStarByRkey(ctx, userID, p.Rkey)
}

func (c *RelayConsumer) handleBackfillComplete(ctx context.Context, evt *relayEvent) error {
	var p struct {
		DID string `json:"did"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal backfillComplete payload: %w", err)
	}
	userID, _ := c.store.GetUserIDByDID(ctx, p.DID)
	if userID == 0 {
		return nil
	}
	slog.Info("relay consumer: backfill complete", "did", p.DID, "user_id", userID)
	return c.store.SetUserSyncStatus(ctx, userID, "idle")
}
