# Sunred API Server

Sunred is an ATProto-backed RSS reader and social timeline. The API server
exposes a REST surface for subscribing to RSS/Atom/JSON feeds, reading entries,
organizing folders, following users, and sharing articles. It authenticates
users via ATProto OAuth (no email/password), and replicates user actions as
`io.sunred.*` records to their PDS via DPoP-bound sessions.

The local PostgreSQL database is a **cache** for ATProto data. The user's PDS
is the source of truth for identity and profile; the API server caches feeds,
entries, follows, shares, subscriptions, and sessions locally so it can serve
a fast, queryable reader experience.

```
Browser ──session cookie──> API (huma REST + plain OAuth handlers)
                              │
                              ├── Postgres (cache: users, feeds, entries, subscriptions,
                              │              follows, shares, sessions, oauth state)
                              ├── Scheduler ──> Worker pool ──> Processor (fetch→parse→sanitize→store)
                              ├── RelayConsumer (WebSocket, resume cursor in PG)
                              └── PDS (via indigo OAuth / DPoP) ── writes io.sunred.* records
```

## What it does

1. **Authenticates** users via ATProto OAuth — browsers use a session cookie,
   CLIs use RFC 8628 device flow, and API clients use bearer tokens.
2. **Fetches and parses** RSS/Atom/JSON feeds on a schedule, sanitizes entry
   HTML, and stores global entries shared across all subscribers.
3. **Serves** a REST API for listing entries (filterable by feed, folder,
   status, search, and source), toggling read/starred, and managing
   subscriptions and folders.
4. **Social** — users set a handle, follow other users, share articles to a
   social timeline, and see entries from people they follow.
5. **Replicates** follows, shares, and feed subscriptions as
   `io.sunred.*` records to the user's PDS, and (optionally) consumes a
   relay WebSocket to backfill and live-fan-out cross-instance activity.

## Dependencies

- **PostgreSQL** — primary cache store. Migrations are embedded into the
  binary and run on boot. `lib/pq` driver.
- **ATProto PDS** (per user, discovered at OAuth time) — OAuth PAR/authorize/
  callback, `com.atproto.server.getSession`, `com.atproto.repo.listRecords`
  (fallback backfill when no relay), `com.atproto.repo.putRecord`/
  `deleteRecord` for `io.sunred.*` writes via DPoP.
- **Sunred Relay** (optional, `SUNRED_RELAY_URL`) — XRPC `announceUser`,
  `searchDIDs`, `resolveHandle`; WebSocket `subscribeEvents` for cursor-resumable
  cross-instance event ingestion.
- **External HTTP** — feed fetching (conditional GET, configurable user agent,
  timeout, and max body size); Google S2 favicons (preview only).

No Redis or external cache. Rate limiting is in-memory (single-instance only).

## Repository layout

```
go/api/
├── cmd/sunred/main.go              # entrypoint: migrate, wire subsystems, serve
├── internal/
│   ├── api/                        # HTTP layer (huma REST + plain OAuth/device handlers)
│   │   ├── api.go                  # API struct, route registration, feed subscription flow
│   │   ├── atproto.go              # PDS record side-effects + relay XRPC calls
│   │   ├── device.go              # RFC 8628 device authorization flow
│   │   ├── oauth_handlers.go       # ATProto OAuth login/callback/signout (plain http)
│   │   ├── relay_consumer.go       # relay WebSocket consumer (resume cursor in PG)
│   │   ├── sync.go                 # direct-PDS backfill fallback (no relay)
│   │   └── social.go              # social endpoints (handle, follow, share, timeline)
│   ├── atproto/                    # ATProto client subsystem (indigo-backed)
│   │   ├── oauth.go                # OAuthApp (localhost vs public config), PG-backed store
│   │   ├── oauthstore.go           # oauth.ClientAuthStore over Postgres tables
│   │   ├── records.go              # io.sunred.* record structs + collection NSIDs
│   │   ├── tid.go                  # base32-sortable TID generation (rkeys)
│   │   ├── writer.go               # Writer: Put/Delete follow/share/feedSubscription
│   │   └── xrpc.go                 # standalone thin XRPC client (relay calls, tests)
│   ├── auth/                       # session cookies + bearer token middleware
│   ├── config/config.go            # env-based configuration
│   ├── cors/cors.go                # origin allowlist middleware
│   ├── httplog/httplog.go          # request logging middleware (slog)
│   ├── logging/logging.go          # slog init (pretty/json)
│   ├── ratelimit/limiter.go        # in-memory token bucket
│   ├── migrations/                 # embedded SQL migrations (embed.FS)
│   ├── reader/                     # feed-discovery → fetch → parse → sanitize pipeline
│   │   ├── discoverer/discoverer.go
│   │   ├── fetcher/fetcher.go
│   │   ├── parser/parser.go
│   │   ├── processor/processor.go
│   │   └── sanitizer/sanitizer.go
│   ├── scheduler/scheduler.go      # periodic feed-refresh scheduler
│   ├── store/                      # *sql.DB query helpers (Postgres)
│   │   ├── store.go                # feeds, entries, subscriptions, folders, tokens, devices
│   │   ├── models.go               # domain structs (JSON-tagged)
│   │   ├── atproto.go              # ATProto credentials, rkeys, relay cursor
│   │   ├── sessions.go             # web_sessions, users-by-DID, sync helpers
│   │   └── social.go               # follows, shares, profiles, timeline
│   └── worker/worker.go            # feed-processing worker pool
├── Dockerfile
├── docker-compose.yml              # local Postgres only
├── Makefile
├── go.mod / go.sum
├── .env.example
└── .gitignore
```

## Architecture

### Startup

`cmd/sunred/main.go` wires the service together:

1. Parse flags: `--migrate` (run migrations and exit), `--openapi` (dump
   OpenAPI JSON and exit, no DB/auth needed).
2. `godotenv.Load()` (best-effort); `config.Load()`; `logging.Init`; open
   Postgres (25 max open, 5 idle, 5 min lifetime); ping; `migrations.Run`.
   If `--migrate`, exit.
3. Build `store`, `auth`, `atproto.NewOAuthApp` (indigo), the huma router
   (`humago`), `fetcher`, and `api.New(...)`. Set the OAuth adapter and
   `RegisterRoutes()`.
4. Mount handlers on a root `http.ServeMux` (see [HTTP routes](#http-routes)
   below for which paths are public vs authenticated).
5. If `!cfg.DisableSched`: start `scheduler.Start(ctx)` in a goroutine
   (processor + worker pool).
6. If `cfg.RelayURL != ""`: start `api.NewRelayConsumer(...).Start(ctx)` in a
   goroutine.
7. Wrap the mux with `httplog.Middleware(cors.Middleware(...))` and serve on
   `cfg.HTTPAddr` with a 10s read-header timeout. Graceful shutdown on
   `SIGINT`/`SIGTERM` with a 10s drain.

### Request flow

```
HTTP request
  → httplog.Middleware      (log status / duration / body snippet)
  → cors.Middleware          (origin allowlist)
  → mux dispatch
  → authInst.Middleware      (bearer token OR session cookie → userID in ctx)
                              [skipped for public paths]
  → huma router → handler → store → Postgres
  → fire-and-forget ATProtoSync* goroutine (Writer → PDS putRecord/deleteRecord,
                                            store rkey locally)
```

### Background jobs

1. **Scheduler** (unless `DisableSched`) — every `PollingFreq`, selects up to
   `BatchSize` due global feeds and submits each to the worker pool
   (`WorkerPool` goroutines). Each worker runs
   `processor.ProcessFeed` (fetch/parse/sanitize/upsert entries, update fetch
   state + `next_check_at`).
2. **RelayConsumer** (if `RelayURL` set) — persistent WebSocket to the relay,
   resuming from the `atproto_relay_cursor` singleton. Processes
   `follow`/`unfollow`/`feedSubscription`/`feedUnsubscription`/`share`/`unshare`/
   `backfillComplete` events into the local cache, advancing the cursor per
   event. Reconnects with a 5s backoff.
3. **Post-login PDS sync** (per OAuth callback) — if no relay, `syncFromPDS`
   lists the user's `io.sunred.*` records (follows, shares, feed
   subscriptions) directly from the PDS and upserts locally; if relay,
   `announceToRelay` registers the DID so the relay backfills. A 3-minute
   safety-net timer flips a stuck `syncing` status to `idle`.
4. **Followee backfill** (per follow action) — `backfillFollowee` reads the
   followed user's `io.sunred.share.article` and `io.sunred.feed.subscription`
   records directly from their PDS via unauthenticated `listRecords` and ingests
   them locally, so the follower sees their shares in the timeline and their
   subscribed feeds on their profile. Works with or without a relay.

Both backfills share a single code path (`backfillUserFromPDS`) that paginates
`com.atproto.repo.listRecords` per collection and upserts via the idempotent
store methods. The login sync pulls all three collections; the followee
backfill pulls only shares + feed subscriptions (the followee's own follow graph
is irrelevant to the follower).

## Reader pipeline

```
discoverer.Discover → fetcher.Fetch → parser.Parse → sanitizer.Sanitize → processor.ProcessFeed → store
```

- **discoverer** — if a URL isn't already a feed, parses HTML `<link rel=...>`
  candidates, ranks by type (rss > atom > json), and returns the first match.
- **fetcher** — conditional GET (`If-None-Match`/`If-Modified-Since`), `Accept`
  with feed MIME types, body capped at `maxBody`, up to 10 redirects. `304` →
  `NotModified`.
- **parser** — detects JSON (content-type or leading `{`) vs XML, then RSS 2.0
  vs Atom. Normalizes to a common `Feed`/`Item` shape with tags and enclosures.
- **sanitizer** — allowlist-based HTML sanitizer (goquery). ~50 allowed tags;
  `a` hrefs are sanitized (blocks `javascript:`/`data:`) and get
  `rel="noopener noreferrer"`. Also exposes `StripHTML` for plain-text
  snippets.
- **processor** — fetches (forces an unconditional re-fetch on a 304 for a feed
  with no entries), parses, sanitizes per item, hashes items (sha256 of
  link+title+publishedAt), idempotently creates global entries + enclosures, and
  updates feed fetch state (ETag/Last-Modified, error count, `next_check_at`).
  `computeNextCheck` averages observed publish intervals (clamped 10m–24h).

Feeds and entries are **global**: one row per feed URL shared across all
subscribers. Per-user state (read/starred, folder placement, title override)
lives in `subscriptions` and `entry_state`. A shared article is also
materialized as a global entry against a synthetic source feed (keyed by
`sha256(articleURL)`), so it appears in followers' entry streams via the same
`ListEntries` query.

## ATProto integration

- **OAuth** — `atproto.NewOAuthApp` builds an indigo OAuth config. For loopback
  origins it uses `NewLocalhostConfig` (client metadata encoded in `client_id`
  query params, no fetch needed); for public origins it uses
  `NewPublicConfig` (real `client_id` URL). Scopes: `atproto`,
  `transition:email`. Auth state and sessions persist in Postgres via
  `oauthstore.PGStore` (`oauth_auth_requests`, `oauth_sessions`).
- **Writer** — `writer.Writer{client, did}` generates a base32-sortable TID as
  the record rkey and calls `com.atproto.repo.putRecord`/`deleteRecord` for the
  three `io.sunred.*` collections. A 20s-timeout HTTP client is used.
- **Records** — `io.sunred.graph.follow` (`FollowRecord`),
  `io.sunred.share.article` (`ShareRecord`), and
  `io.sunred.feed.subscription` (`SubscriptionRecord`). `tid.NewTID()` produces
  13-char base32-sortable rkeys (53-bit microsecond timestamp + 10-bit random
  clock id), monotonic within a process.
- **PDS session** — DPoP-bound sessions are resumed via
  `oauthApp.WriterClient(ctx, did, sessionID)`. `users.oauth_session_id`
  identifies the active session. A fallback to an unauthenticated client exists
  for tests.
- **Side-effects** — `ATProtoSyncFollow`, `ATProtoSyncShare`, and
  `ATProtoSyncFeedSubscription` are fire-and-forget goroutines that write (or
  delete) records on the PDS and store the resulting rkey locally so subsequent
  deletes target the right record.
- **Followee backfill** — `backfillFollowee` reads the followed user's shares
  and feed subscriptions directly from their PDS (unauthenticated `listRecords`)
  so the follower's timeline and the followee's profile populate immediately.
  Works independently of the relay.

## Auth model

- **Identity** — ATProto OAuth via indigo. No email/password (dropped in
  migration 011). The PDS is the source of truth; the local `users` row caches
  DID, handle, display_name, bio, `pds_url`, and PDS tokens.
- **Web sessions** — opaque `sunred_session` cookie → `web_sessions(token,
  user_id, expires_at)`, 30-day TTL, HttpOnly, Secure/SameSite/Domain per config.
- **API tokens** — `pla_<32 bytes hex>` plaintext shown once at creation; stored
  as a SHA-256 hash in `api_tokens`. Origin `manual` (web UI) or `device_flow`
  (14-day expiry). Bearer auth via `Authorization: Bearer …`.
- **Device flow (RFC 8628)** — CLI/TUI requests a `PLN-XXXX-XXXX` user code plus a
  hashed device code; the user approves in the browser (session auth); the CLI
  polls and receives the plaintext token once (single-use grant).
- **Middleware** — `auth.Middleware` resolves the user via bearer token *or* the
  session cookie and injects `userID` into the request context. Returns 401 on
  failure.

## HTTP routes

The codebase uses two distinct route prefix families — core reader routes at
`/v1/...` and social routes at `/api/v1/...`. Both are served behind auth
middleware (except the explicitly public paths listed first).

### Public (no auth)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/health` | Health check (`{"status":"ok"}`) |
| POST | `/auth/device/code` | Begin device-flow login (rate-limited) |
| POST | `/auth/device/token` | Poll device-flow result (rate-limited) |
| GET/POST | `/auth/oauth/login` | Start ATProto OAuth (PAR → PDS authorize) |
| GET | `/auth/oauth/signup` | Start OAuth against `DefaultPDS` |
| GET | `/auth/oauth/config` | Public OAuth config (`default_pds`) |
| GET | `/auth/oauth/callback` | OAuth callback (creates/looks up user, sync, session cookie) |
| GET | `/auth/signout` | Clear session cookie |
| GET | `/client-metadata.json` | OAuth client metadata document |
| GET | `/.well-known/atproto-did` | Resolve handle (subdomain or `?handle=`) → DID |
| GET | `/docs`, `/openapi.json` | Huma docs / OpenAPI spec |

### Authenticated — core reader (`/v1`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/me` | Current user |
| PATCH | `/v1/me` | Update display_name |
| DELETE | `/v1/me` | Delete account |
| POST/GET | `/v1/folders` | Create / list folders |
| PATCH/DELETE | `/v1/folders/{folderId}` | Update / delete folder |
| POST/GET | `/v1/feeds` | Subscribe (discover+fetch+parse) / list subscriptions |
| GET/DELETE/PATCH | `/v1/feeds/{feedId}` | Get / unsubscribe (+PDS delete) / update folder+title |
| POST | `/v1/feeds/{feedId}/mark-all-read` | Mark feed entries read |
| POST | `/v1/feeds/{feedId}/refresh` | Manual refresh via processor |
| POST | `/v1/feeds/preview` | Discover+fetch+parse without subscribing (≤20 items) |
| GET | `/v1/entries` | List entries (feed_id, folder_id, status, starred, search, source) |
| GET | `/v1/entries/{entryId}` | Get entry (visible only) |
| PUT | `/v1/entries` | Bulk status update (null entry_ids = mark all read) |
| PUT | `/v1/entries/{entryId}/starred` | Toggle starred |
| POST/GET | `/v1/tokens` | Create (returns plaintext `pla_…` once) / list API tokens |
| DELETE | `/v1/tokens/{tokenId}` | Delete API token |
| GET | `/v1/opml/export` | Export subscriptions+folders as OPML 2.0 |
| POST | `/v1/opml/import` | Import OPML (creates folders, subscribes, idempotent) |
| GET | `/api/v1/me/atproto` | ATProto identity status |

### Authenticated — social (`/api/v1`)

| Method | Path | Purpose |
|---|---|---|
| PATCH | `/api/v1/me/handle` | Set/update handle + bio |
| GET | `/api/v1/users/{handle}` | Public profile (+shared articles +feeds) |
| POST | `/api/v1/users/{handle}/follow` | Follow (relay-resolve fallback → remote user) |
| DELETE | `/api/v1/users/{handle}/follow` | Unfollow |
| GET | `/api/v1/social/following` | Who I follow |
| GET | `/api/v1/social/search` | Search users (local + relay) |
| GET | `/api/v1/users/{handle}/followers` | Followers of a user |
| GET | `/api/v1/users/{handle}/following` | Who a user follows |
| POST | `/api/v1/social/shares` | Share an article (+PDS write) |
| DELETE | `/api/v1/social/shares/{shareId}` | Unshare (+PDS delete) |
| GET | `/api/v1/social/timeline` | Shares from followed users |
| GET | `/api/v1/social/shares` | My shares |
| GET | `/api/v1/feeds/{feedId}/subscribers` | Subscriber count + profiles |

### Authenticated — device flow (session-cookie only)

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/device/confirm` | Approve/deny a device code |
| GET | `/auth/device/status` | Device code status |

## Configuration

The server is configured entirely through environment variables (loaded via
`godotenv` from `.env` if present). See `.env.example` for a ready-to-copy
template.

| Variable | Default | Description |
|---|---|---|
| `SUNRED_HTTP_ADDR` | `:8080` | HTTP listen address |
| `SUNRED_DATABASE_URL` | `postgres://sunred:sunred@localhost:5432/sunred?sslmode=disable` | Postgres DSN |
| `SUNRED_BASE_URL` | `http://127.0.0.1:8080` | Public base URL of the API (used for OAuth client_id, callback) |
| `SUNRED_WEB_URL` | `http://localhost:3000` | Public base URL of the web app (OAuth redirects, CORS) |
| `SUNRED_LOG_FORMAT` | `pretty` | `pretty`/`text` (slog text) or `json` |
| `SUNRED_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `SUNRED_POLLING_FREQUENCY` | `60s` | Scheduler tick interval |
| `SUNRED_BATCH_SIZE` | `100` | Max feeds refreshed per scheduler tick |
| `SUNRED_WORKER_POOL_SIZE` | `5` | Feed-processing worker pool size |
| `SUNRED_HTTP_CLIENT_TIMEOUT` | `20s` | Feed-fetch HTTP client timeout |
| `SUNRED_HTTP_CLIENT_MAX_BODY` | `15728640` (15 MiB) | Max feed body size in bytes |
| `SUNRED_CLEANUP_FREQUENCY` | `24h` | Old-entry purge interval |
| `SUNRED_ENTRY_MAX_AGE_DAYS` | `60` | Purge entries older than this |
| `SUNRED_DISABLE_SCHEDULER` | unset | Any non-empty value disables the scheduler |
| `SUNRED_COOKIE_SECURE` | false | Set `Secure` on the session cookie |
| `SUNRED_COOKIE_SAMESITE` | `lax` | `lax`/`none`/`strict` (`none` requires `Secure`) |
| `SUNRED_COOKIE_DOMAIN` | "" (bare host) | Cookie `Domain` (must be a bare host, no scheme/path/port) |
| `SUNRED_TRUSTED_ORIGINS` | nil (permissive) | Comma-separated CORS allowlist; auto-includes `WebURL` when set |
| `SUNRED_RELAY_URL` | "" (disabled) | Sunred relay base URL (enables the relay consumer + relay XRPC calls) |
| `SUNRED_DEFAULT_PDS` | `https://snrd.social` | PDS used for `/auth/oauth/signup` |

Derived: `OAuthClientID = <BaseURL>/client-metadata.json`,
`OAuthCallbackURL = <BaseURL>/auth/oauth/callback`.

## Database schema

Migrations live in `internal/migrations/` and are embedded into the binary
(`embed.FS`). The runner creates a `schema_migrations` bookkeeping table and
applies each `*.sql` file in filename order inside its own transaction. There
is no `003_*` file; the applied set is `001`–`002`, `004`–`015`.

| Table | Purpose |
|---|---|
| `users` | DID-based identity cache (handle, display_name, bio, pds_url, atproto tokens, sync status, oauth_session_id) |
| `sessions` | Legacy Limen sessions (largely unused) |
| `web_sessions` | Cookie → user_id (30-day TTL) |
| `oauth_auth_requests` | In-flight OAuth state (state → JSONB) |
| `oauth_sessions` | Persisted indigo OAuth sessions (did + session_id → JSONB) |
| `api_tokens` | Hashed bearer tokens (origin manual/device_flow, optional expiry) |
| `device_codes` | RFC 8628 device grants (hashed device_code, user_code, status, single-use plaintext) |
| `folders` | Nestable feed folders (parent_id self-ref, sort_order) |
| `feeds` | Global RSS sources (one per feed_url; shared fetch state) |
| `subscriptions` | Per-user link to a global feed (folder, title override, atproto_rkey) |
| `entries` | Global articles (one per feed+hash; generated `document` tsvector) |
| `entry_state` | Per-user read/starred/liked (absence = unread) |
| `enclosures` | Entry media attachments |
| `feed_icons` | Feed favicon bytes |
| `user_follows` | Follow graph (follower/followee unique; atproto_rkey) |
| `shared_articles` | Social shares (user+article_url unique; atproto_rkey; entry_id link) |
| `atproto_relay_cursor` | Singleton relay WebSocket resume cursor (id = 1) |
| `schema_migrations` | Applied migration filenames |

Notable migration history: `011` dropped email/password (ATProto-only identity),
`013` converted feeds/entries from per-user to global (introducing
`subscriptions` + `entry_state`), and `014` folded `user_profiles` into `users`.

## Running

### Local development

```sh
make db-up      # start local Postgres on :5432
make run        # build + run the API (migrations run on boot)
```

Copy `.env.example` to `.env` and adjust values as needed.

### Migrate only

```sh
make migrate    # run migrations and exit (equivalent to ./sunred --migrate)
```

### OpenAPI generation

```sh
make gen-openapi   # dump openapi.json, copy to ts/apps/docs, regenerate TS client
```

### Docker

The Dockerfile is a multi-stage build producing a fully static, stripped binary
in an Alpine 3.20 image with `ca-certificates` and `tzdata`, running as a
non-root user `sunred` (uid 10001). It exposes port `8080` and runs
`/app/sunred` as its entrypoint. Migrations run on every boot.

```sh
docker build -t sunred-api -f go/api/Dockerfile go/api
docker run --env-file go/api/.env.example -p 8080:8080 sunred-api
```

### Tests

```sh
make test       # go test ./... -count=1
```

`internal/api` and `internal/store` tests are integration tests against a real
Postgres (env `SUNRED_DATABASE_URL`, or fallback
`postgres://sunred:sunred@localhost:5432/sunred_test?sslmode=disable`); they
skip if the database is unreachable. `internal/reader/parser` and
`internal/atproto` tests are unit tests needing no database. ATProto write
paths are exercised against a mock PDS; relay consumer tests use a mock
WebSocket relay server.

## Operational notes

- **Graceful shutdown:** `SIGINT`/`SIGTERM` triggers a 10s HTTP shutdown drain.
- **Scheduler:** disabled entirely with any non-empty `SUNRED_DISABLE_SCHEDULER`.
  Useful for running API replicas that only serve requests while a single
  replica owns the scheduler.
- **Relay consumer:** runs as a single in-process goroutine. The resume cursor
  lives in Postgres (`atproto_relay_cursor`), so multiple replicas would need
  coordination to avoid duplicate consumers.
- **Rate limiting:** in-memory token bucket — single-instance only. The source
  notes that Redis is recommended for multi-replica deployments.
- **Cookie sharing:** the OAuth callback rewrites `127.0.0.1` to `localhost`
  so the host-only session cookie is shared across the web (`:3000`) and API
  (`:8080`) ports during local development.
- **CORS:** when `SUNRED_TRUSTED_ORIGINS` is unset, CORS is permissive (reflects
  any origin with credentials). Set it explicitly for production.
