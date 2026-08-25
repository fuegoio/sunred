// Package auth handles Sunred authentication: web session cookies and bearer
// API tokens. Identity is provided by AT Proto OAuth (see the atproto package);
// this package only resolves the authenticated user ID for a request.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/config"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

type contextKey int

const userKey contextKey = iota

// SessionCookie is the name of the Sunred web session cookie.
const SessionCookie = "sunred_session"

// SessionTTL is how long a web session cookie remains valid.
const SessionTTL = 30 * 24 * time.Hour

// ErrNoSession is returned when a request has no valid session or token.
var ErrNoSession = errors.New("no valid session")

// Auth resolves the authenticated user for HTTP requests.
type Auth struct {
	Store *store.Store
	DB    *sql.DB
	cfg   *config.Config
}

// New builds an Auth instance. Identity is resolved via web session cookies
// or bearer API tokens; the actual AT Proto OAuth flow is handled by the
// atproto package and the OAuth HTTP handlers.
func New(cfg *config.Config, db *sql.DB, st *store.Store) (*Auth, error) {
	return &Auth{Store: st, DB: db, cfg: cfg}, nil
}

// Middleware wraps protected API routes. It resolves the user ID via a bearer
// API token (Authorization: Bearer ...) or the web session cookie, then injects
// it into the request context.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := a.resolveUserID(r)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeUnauthorized emits a huma ErrorModel (RFC 9457) body for 401 responses.
// The web client reads the `status` field to distinguish auth failures (which
// redirect to /login) from 5xx/network outages (which render an error page), so
// the body must carry `status` like every other huma error in the API.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"$schema": "https://example.com/schemas/ErrorModel.json",
		"title":   "Unauthorized",
		"status":  http.StatusUnauthorized,
		"detail":  "A valid session or API token is required.",
	})
}

func (a *Auth) resolveUserID(r *http.Request) (int, error) {
	if bearer := bearerToken(r); bearer != "" {
		return a.resolveToken(r.Context(), bearer)
	}
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return 0, ErrNoSession
	}
	return a.Store.GetWebSession(r.Context(), cookie.Value)
}

func (a *Auth) resolveToken(ctx context.Context, token string) (int, error) {
	hash := HashToken(token)
	t, err := a.Store.GetAPITokenByHash(ctx, hash)
	if err != nil || t == nil {
		return 0, ErrNoSession
	}
	return t.UserID, nil
}

// IssueSession creates a web session for userID and sets the cookie on w.
func (a *Auth) IssueSession(w http.ResponseWriter, userID int) error {
	token, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("gen session token: %w", err)
	}
	expiresAt := time.Now().Add(SessionTTL)
	if err := a.Store.CreateWebSession(context.Background(), token, userID, expiresAt); err != nil {
		return fmt.Errorf("create web session: %w", err)
	}
	http.SetCookie(w, a.sessionCookie(token, expiresAt))
	return nil
}

// ClearSession deletes the web session for the request's cookie and expires it.
func (a *Auth) ClearSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		_ = a.Store.DeleteWebSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, a.sessionCookie("", time.Time{}))
}

// UserIDFromCtx extracts the authenticated user id stored by Middleware, or 0.
func UserIDFromCtx(ctx context.Context) int {
	v, _ := ctx.Value(userKey).(int)
	return v
}

func (a *Auth) sessionCookie(value string, expires time.Time) *http.Cookie {
	ck := &http.Cookie{
		Name:     SessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: parseSameSite(a.cfg.CookieSameSite),
	}
	if a.cfg.CookieDomain != "" {
		ck.Domain = a.cfg.CookieDomain
	}
	if expires.IsZero() {
		ck.MaxAge = -1
	} else {
		ck.Expires = expires
	}
	return ck
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// ParseSameSite converts a SameSite config string ("lax", "none", "strict")
// to the corresponding http.SameSite value.
func ParseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}

func parseSameSite(s string) http.SameSite {
	return ParseSameSite(s)
}

// randomToken returns a hex-encoded random string of n bytes.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
