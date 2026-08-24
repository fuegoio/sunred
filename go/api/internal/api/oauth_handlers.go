package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"

	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/config"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// OAuthHandlers serves the AT Proto OAuth flow: login start, callback,
// client metadata, and signout. These are plain http handlers (not huma)
// because they involve browser redirects and form-encoded callbacks.
type OAuthHandlers struct {
	oauthApp *oauth.ClientApp
	store    *store.Store
	auth     *auth.Auth
	cfg      *config.Config
}

// NewOAuthHandlers builds the OAuth HTTP handlers. oauthApp may be nil when
// only generating the OpenAPI spec (the handlers are not registered then).
func NewOAuthHandlers(oauthApp *oauth.ClientApp, st *store.Store, a *auth.Auth, cfg *config.Config) *OAuthHandlers {
	return &OAuthHandlers{oauthApp: oauthApp, store: st, auth: a, cfg: cfg}
}

// Routes returns the path → handler mappings for the OAuth flow. Mounted on
// the bare mux in main.go so they are reachable without authentication.
func (h *OAuthHandlers) Routes() map[string]http.Handler {
	return map[string]http.Handler{
		"/auth/oauth/login":     http.HandlerFunc(h.handleLogin),
		"/auth/oauth/signup":    http.HandlerFunc(h.handleSignup),
		"/auth/oauth/config":    http.HandlerFunc(h.handleOAuthConfig),
		"/auth/oauth/callback":  http.HandlerFunc(h.handleCallback),
		"/auth/signout":         http.HandlerFunc(h.handleSignout),
		"/client-metadata.json": http.HandlerFunc(h.handleClientMetadata),
	}
}

// handleLogin starts the OAuth flow: resolves the handle/DID, sends a PAR
// request to the user's PDS, persists the in-flight state, and redirects the
// browser to the PDS authorize endpoint.
func (h *OAuthHandlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauthApp == nil {
		http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	identifier := strings.TrimSpace(r.URL.Query().Get("handle"))
	if identifier == "" && r.Method == http.MethodPost {
		var body struct {
			Handle string `json:"handle"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		identifier = strings.TrimSpace(body.Handle)
	}
	if identifier == "" {
		http.Error(w, `{"error":"handle is required"}`, http.StatusBadRequest)
		return
	}

	// Redirect target after a successful login (defaults to the web app).
	redirectTo := safeOAuthRedirect(r.URL.Query().Get("redirect"), h.cfg.WebURL)

	authURL, err := h.oauthApp.StartAuthFlow(r.Context(), identifier)
	if err != nil {
		slog.Warn("oauth: start auth flow", "identifier", identifier, "err", err)
		http.Error(w, `{"error":"could not start login: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Stash the post-login redirect in a short-lived cookie so the callback
	// can pick it up. The auth request state itself lives in the PGStore.
	http.SetCookie(w, h.redirectCookie(redirectTo, 600))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleSignup starts the OAuth flow against the configured default PDS
// (SUNRED_DEFAULT_PDS). This lets users without an existing AT Proto account
// sign up on the instance's own PDS. Returns 503 if no default PDS is configured.
func (h *OAuthHandlers) handleSignup(w http.ResponseWriter, r *http.Request) {
	if h.oauthApp == nil {
		http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
		return
	}
	if h.cfg.DefaultPDS == "" {
		http.Error(w, `{"error":"signup is not available on this instance"}`, http.StatusServiceUnavailable)
		return
	}

	redirectTo := safeOAuthRedirect(r.URL.Query().Get("redirect"), h.cfg.WebURL)

	// StartAuthFlow accepts a PDS URL as the identifier — it resolves directly
	// to that PDS's auth server metadata rather than resolving a handle → DID.
	authURL, err := h.oauthApp.StartAuthFlow(r.Context(), h.cfg.DefaultPDS)
	if err != nil {
		slog.Warn("oauth: start signup flow", "pds", h.cfg.DefaultPDS, "err", err)
		http.Redirect(w, r, h.cfg.WebURL+"/login?error=signup_failed", http.StatusFound)
		return
	}

	http.SetCookie(w, h.redirectCookie(redirectTo, 600))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// redirectCookie builds the short-lived sunred_oauth_redirect cookie used to
// pass the post-login redirect target from the login/start handler to the
// callback. It obeys the same cookie config (Secure, SameSite, Domain) as the
// session cookie.
func (h *OAuthHandlers) redirectCookie(value string, maxAge int) *http.Cookie {
	ck := &http.Cookie{
		Name:     "sunred_oauth_redirect",
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: auth.ParseSameSite(h.cfg.CookieSameSite),
	}
	if h.cfg.CookieDomain != "" {
		ck.Domain = h.cfg.CookieDomain
	}
	return ck
}

// handleOAuthConfig returns public OAuth configuration so the web frontend
// knows whether signup is available (default PDS configured).
func (h *OAuthHandlers) handleOAuthConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"default_pds": h.cfg.DefaultPDS,
	})
}

// handleCallback completes the OAuth flow: exchanges the authorization code for
// DPoP-bound tokens, creates or looks up the local user by DID, syncs the
// user's io.sunred.* records from their PDS into the local cache, announces
// the user to the relay, issues a web session cookie, and redirects to the app.
func (h *OAuthHandlers) handleCallback(w http.ResponseWriter, r *http.Request) {
	if h.oauthApp == nil {
		http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
		return
	}

	// The OAuth redirect_uri uses 127.0.0.1 (RFC 8252 rejects "localhost").
	// When the PDS redirects the browser back to 127.0.0.1, bounce it to
	// localhost so the session cookie (host-only, no Domain in dev) is scoped
	// to the same host the user browses. This redirect happens before
	// processing the callback, so no cookie is set on 127.0.0.1.
	if strings.HasPrefix(r.Host, "127.0.0.1:") {
		u := *r.URL
		u.Host = "localhost:" + strings.TrimPrefix(r.Host, "127.0.0.1:")
		u.Scheme = "http"
		http.Redirect(w, r, u.String(), http.StatusFound)
		return
	}

	sessData, err := h.oauthApp.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		slog.Warn("oauth: process callback", "err", err)
		// Redirect to the web login with an error so the user sees a friendly page.
		http.Redirect(w, r, h.cfg.WebURL+"/login?error=oauth_failed", http.StatusFound)
		return
	}

	did := string(sessData.AccountDID)

	// Resolve the handle and email from the OAuth session's host for the
	// local user row. The email is available when the transition:email scope
	// was granted by the PDS.
	handle := ""
	if u, herr := h.oauthApp.ResumeSession(r.Context(), sessData.AccountDID, sessData.SessionID); herr == nil {
		if c := u.APIClient(); c != nil {
			var info struct {
				Handle string `json:"handle"`
				DID    string `json:"did"`
			}
			if err := c.Get(r.Context(), "com.atproto.server.getSession", nil, &info); err == nil {
				handle = info.Handle
			}
		}
	}

	userID, created, err := h.store.GetOrCreateUserByDID(r.Context(), did, handle)
	if err != nil {
		slog.Error("oauth: get/create user", "did", did, "err", err)
		http.Redirect(w, r, h.cfg.WebURL+"/login?error=internal", http.StatusFound)
		return
	}
	slog.Info("oauth: user logged in", "did", did, "user_id", userID, "handle", handle, "new", created)

	// Persist the PDS host and OAuth session ID so the API can resume the
	// DPoP-bound session to write io.sunred.* records to the user's PDS on
	// their behalf (follows, shares, feed subscriptions).
	if err := h.store.SetUserPDSSession(r.Context(), userID, sessData.HostURL, sessData.SessionID); err != nil {
		slog.Warn("oauth: persist pds session", "did", did, "err", err)
	}

	// Sync the PDS data into the local cache. When a relay is configured, the
	// relay does the backfill (tap-style: historical records first, then live)
	// and pushes events to the API's relay consumer; the sync status is set
	// to "idle" when the relay emits a backfillComplete event for this DID.
	// When no relay is configured, fall back to pulling records per-collection
	// via listRecords.
	if err := h.store.SetUserSyncStatus(r.Context(), userID, "syncing"); err != nil {
		slog.Warn("oauth: set sync status syncing", "did", did, "err", err)
	}
	go func() {
		bgCtx := context.Background()
		if h.cfg.RelayURL == "" {
			// No relay: fall back to direct listRecords backfill.
			if err := h.syncFromPDS(bgCtx, did, userID, sessData.HostURL); err != nil {
				slog.Warn("oauth: sync from pds", "did", did, "err", err)
				if serr := h.store.SetUserSyncStatus(bgCtx, userID, "failed"); serr != nil {
					slog.Warn("oauth: set sync status failed", "did", did, "err", serr)
				}
			} else {
				if serr := h.store.SetUserSyncStatus(bgCtx, userID, "idle"); serr != nil {
					slog.Warn("oauth: set sync status idle", "did", did, "err", serr)
				}
			}
			return
		}

		// Relay configured: announce so it backfills + tracks the DID. The
		// relay only backfills newly-tracked DIDs and emits backfillComplete
		// for those; an already-tracked DID (a re-login) needs no backfill, so
		// dismiss the spinner right away instead of waiting for an event that
		// will never come.
		isNew, annErr := h.announceToRelay(bgCtx, did, sessData.HostURL, handle)
		if annErr != nil || !isNew {
			if serr := h.store.SetUserSyncStatus(bgCtx, userID, "idle"); serr != nil {
				slog.Warn("oauth: set sync status idle", "did", did, "err", serr)
			}
			return
		}
		// Newly tracked: the relay consumer sets "idle" on backfillComplete.
		// Add a safety net so a lost/late event can't keep the spinner up
		// forever.
		go func() {
			t := time.NewTimer(3 * time.Minute)
			defer t.Stop()
			<-t.C
			if u, _ := h.store.GetUserByID(bgCtx, userID); u != nil && u.PDSSyncStatus == "syncing" {
				slog.Warn("oauth: backfill timed out, marking sync idle", "did", did)
				if err := h.store.SetUserSyncStatus(bgCtx, userID, "idle"); err != nil {
					slog.Warn("oauth: set sync status idle (timeout)", "did", did, "err", err)
				}
			}
		}()
	}()
	_ = created

	if err := h.auth.IssueSession(w, userID); err != nil {
		slog.Error("oauth: issue session", "err", err)
		http.Redirect(w, r, h.cfg.WebURL+"/login?error=internal", http.StatusFound)
		return
	}

	redirectTo := h.cfg.WebURL + "/"
	if c, err := r.Cookie("sunred_oauth_redirect"); err == nil && c.Value != "" {
		http.SetCookie(w, h.redirectCookie("", -1))
		redirectTo = c.Value
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// handleSignout clears the web session cookie.
func (h *OAuthHandlers) handleSignout(w http.ResponseWriter, r *http.Request) {
	h.auth.ClearSession(w, r)
	http.Redirect(w, r, h.cfg.WebURL+"/", http.StatusFound)
}

// handleClientMetadata serves the OAuth client metadata document. The client_id
// is a URL that must point here; the PDS fetches this during the auth flow.
func (h *OAuthHandlers) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	if h.oauthApp == nil {
		http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
		return
	}
	doc := h.oauthApp.Config.ClientMetadata()
	doc.ClientName = ptr("Sunred")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// safeOAuthRedirect sanitizes a redirect target to the configured web origin.
func safeOAuthRedirect(value, webURL string) string {
	if value == "" {
		return webURL + "/"
	}
	// Only allow same-origin paths or the web origin itself.
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") {
		return webURL + value
	}
	return webURL + "/"
}

// --- Sync on login ---

// syncFromPDS backfills the user's io.sunred.* records from their PDS into the
// local cache. Uses unauthenticated listRecords (a public read), so no OAuth
// session resume is needed. Best-effort: failures are logged but never fail
// the login.
func (h *OAuthHandlers) syncFromPDS(ctx context.Context, did string, userID int, pdsURL string) error {
	c := atproto.NewClient(pdsURL, "")
	return backfillUserFromPDS(ctx, c, h.store, userID, did, []string{
		atproto.CollectionFollow,
		atproto.CollectionShare,
		atproto.CollectionStar,
		atproto.CollectionSubscription,
	})
}

// announceToRelay notifies the relay of a user DID (see atproto.go). It returns
// whether the relay newly started tracking the DID (isNew) — when false the
// relay already has the records and live subscription, so no backfill event
// will be emitted and the caller should not wait for one.
func (h *OAuthHandlers) announceToRelay(ctx context.Context, did, pdsURL, handle string) (bool, error) {
	if h.cfg.RelayURL == "" {
		return false, nil
	}
	rc := atproto.NewClient(h.cfg.RelayURL, "")
	type announceIn struct {
		DID         string `json:"did"`
		PDSUrl      string `json:"pdsUrl"`
		InstanceURL string `json:"instanceUrl"`
		Handle      string `json:"handle"`
	}
	var out struct {
		New bool `json:"new"`
	}
	if err := rc.Procedure(ctx, "io.sunred.relay.announceUser", announceIn{
		DID:         did,
		PDSUrl:      pdsURL,
		InstanceURL: h.cfg.BaseURL,
		Handle:      handle,
	}, &out); err != nil {
		slog.Warn("relay: announce user", "did", did, "err", err)
		return false, err
	}
	return out.New, nil
}

// ptr returns a pointer to s. Convenience for optional string fields.
func ptr(s string) *string { return &s }
