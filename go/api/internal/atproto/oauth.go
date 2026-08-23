package atproto

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// OAuthApp resumes a user's OAuth session to obtain a DPoP-bound APIClient
// for writing io.sunred.* records to their PDS. It decouples the API from the
// indigo OAuth types.
type OAuthApp interface {
	WriterClient(ctx context.Context, did, sessionID string) (*atclient.APIClient, error)
}

type oauthAppAdapter struct{ app *oauth.ClientApp }

// NewOAuthAppAdapter wraps the indigo OAuth ClientApp behind the OAuthApp
// interface used by the API for record writes.
func NewOAuthAppAdapter(app *oauth.ClientApp) OAuthApp {
	return oauthAppAdapter{app: app}
}

func (a oauthAppAdapter) WriterClient(ctx context.Context, did, sessionID string) (*atclient.APIClient, error) {
	sess, err := a.app.ResumeSession(ctx, syntax.DID(did), sessionID)
	if err != nil {
		return nil, err
	}
	return sess.APIClient(), nil
}

// NewOAuthApp builds the indigo OAuth ClientApp for Sunred.
//
// For local development (BaseURL on 127.0.0.1 or localhost), it uses
// NewLocalhostConfig, which encodes the client metadata directly in the
// client_id as query parameters — the PDS accepts this without fetching a
// metadata document (which it couldn't reach on a loopback address anyway).
//
// For production (https BaseURL), it uses NewPublicConfig with a real
// client_id URL pointing at the client-metadata.json document this server
// serves.
//
// Sunred is a public (non-confidential) client: it has no shared secret with
// the PDS, relying on PKCE + DPoP instead. The PGStore persists auth-request
// state and sessions in PostgreSQL so logins survive restarts.
func NewOAuthApp(db *sql.DB, clientID, callbackURL string) (*oauth.ClientApp, error) {
	if clientID == "" || callbackURL == "" {
		return nil, fmt.Errorf("oauth client_id and callback URL are required")
	}

	// "atproto" is the base scope (com.atproto.* XRPCs); "transition:email"
	// lets us read the account email via com.atproto.server.getSession on
	// callback. The PDS enforces a per-collection write scope on every repo
	// record, so we request one scope per io.sunred.* collection we write
	// (follows, shares, feed subscriptions) plus app.bsky.actor.profile for
	// the mirrored display name + bio.
	scopes := []string{
		"atproto",
		"transition:email",
		"repo:app.bsky.actor.profile",
		"repo:io.sunred.graph.follow",
		"repo:io.sunred.share.article",
		"repo:io.sunred.feed.subscription",
	}

	var config oauth.ClientConfig
	if isLoopbackURL(callbackURL) {
		config = oauth.NewLocalhostConfig(callbackURL, scopes)
	} else {
		config = oauth.NewPublicConfig(clientID, callbackURL, scopes)
	}

	store := NewPGStore(db)
	app := oauth.NewClientApp(&config, store)
	return app, nil
}

// isLoopbackURL reports whether the URL points at a loopback address
// (127.0.0.1 or localhost), in which case the localhost OAuth client config
// is used.
func isLoopbackURL(rawURL string) bool {
	return strings.Contains(rawURL, "://127.0.0.1") ||
		strings.Contains(rawURL, "://localhost")
}
