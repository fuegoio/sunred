# Sunred Relay

The Sunred relay is a Go service that federates ATProto (`com.atproto`) activity
across independent Sunred instances. Each Sunred instance announces the DIDs of
its connected users; the relay subscribes to those DIDs' PDS repo streams,
aggregates `io.sunred.*` record events into global counts (followers, shares,
feed subscriptions), and fans the events back out to subscribed instances over a
WebSocket endpoint.

```
Sunred instance A ─┐                              ┌─ PDS for DID x  (wss subscribeRepos)
                   ├─ POST announceUser ──► RELAY ─►─ PDS for DID y  (wss subscribeRepos)
Sunred instance B ─┘  ◄── WS subscribeEvents ──  ── PDS for DID z  (wss subscribeRepos)
                            (global event fanout)
```

## What it does

1. **Accepts announcements** from Sunred instances when users connect ATProto
   identities (`io.sunred.relay.announceUser`).
2. **Maintains persistent WebSocket subscriptions** to each announced DID's PDS
   repo stream (`com.atproto.sync.subscribeRepos`).
3. **Aggregates** `io.sunred.*` record events into global counts (followers,
   shares, feed subs) and stores them in PostgreSQL.
4. **Streams events** back to subscribed instances via the
   `io.sunred.relay.subscribeEvents` WebSocket endpoint, with cursor-based
   replay for reconnecting clients.

## Dependencies

- **PostgreSQL** — the relay has its own database (default `sunred_relay` on
  port `5433` when running via docker-compose, distinct from any app DB on
  `5432`).
- **Outbound HTTPS/WebSocket** to PDSes — `com.atproto.sync.subscribeRepos`
  (live firehose), `com.atproto.repo.getRecord` (live record fetch), and
  `com.atproto.repo.listRecords` (historical backfill).

No other external services.

## Repository layout

```
go/relay/
├── cmd/relay/main.go            # entrypoint: load env, migrate, start fanout + HTTP server
├── internal/
│   ├── config/config.go         # env-based configuration
│   ├── migrations/              # embedded SQL migrations (embed.FS)
│   │   ├── migrations.go
│   │   ├── 001_relay_tables.sql
│   │   └── 002_handle_index.sql
│   ├── fanout/                  # PDS WebSocket subscriptions + event aggregation
│   │   ├── fanout.go            # live firehose: subscribeRepos → processOp → store + emit
│   │   ├── backfill.go          # historical backfill via listRecords, then cutover to live
│   │   ├── fanout_test.go
│   │   └── backfill_test.go
│   ├── server/server.go         # HTTP/WS endpoints exposed to instances
│   └── store/store.go           # *sql.DB query helpers (PostgreSQL)
├── Dockerfile
├── docker-compose.yml           # local Postgres only (no relay service)
├── Makefile
├── go.mod / go.sum
├── .env.example
└── .gitignore
```

## Architecture

### Startup

`cmd/relay/main.go` wires the service together:

1. Parse the `--migrate` flag; `godotenv.Load()` (best-effort).
2. `config.Load()` → open a Postgres pool (25 max open conns, 5 idle, 5 min
   conn lifetime) and ping it.
3. `migrations.Run(db)` — runs on every boot (idempotent via the
   `schema_migrations` table). If `--migrate` was passed, exits after migrating.
4. `store.New(db)`; `fanout.New(st, cfg.ReconnectDelay)`; `go fan.Start(ctx)`
   subscribes to all active tracked DIDs.
5. Background goroutine: hourly `PurgeOldEvents(cfg.EventRetention)`.
6. `server.New(st, fan)`; `http.Server{ReadHeaderTimeout: 10s}`; graceful
   shutdown on `SIGINT`/`SIGTERM` with a 10s drain timeout.

### Fanout engine (`internal/fanout`)

The core engine. **One goroutine per tracked DID** subscribes to that DID's PDS
repo firehose, processes `#commit` frames, records observations, and emits
fanout events.

- `Start(ctx)` loads all active tracked DIDs, calls `EnsureSubscribed` for
  each, then blocks until the context is cancelled.
- `EnsureSubscribed(ctx, did, pdsURL, cursorSeq)` — idempotent; starts a worker
  goroutine for a DID (guarded by a mutex).
- `runWorker(ctx, did, pdsURL, cursorSeq)` — the per-DID loop: `subscribe(...)`;
  on error marks the DID `status='error'` and waits `reconnectDelay` (or ctx
  done) before retrying.
- `subscribe(ctx, did, pdsURL, cursorSeq)` — dials
  `ws(s)://<host>/xrpc/com.atproto.sync.subscribeRepos?wantedDids=<did>&cursor=<seq>`,
  receives frames, skips non-`#commit` frames, dispatches each op to
  `processOp`, and updates `cursor_seq` when `frame.Seq > 0`.
- `processOp(ctx, did, pdsURL, op)` — splits `op.Path` into `collection`/`rkey`
  and routes to the matching handler. Unknown collections and malformed paths
  are ignored.

### Record collections

The relay handles the four `io.sunred.*` social-graph collections:

| Collection | Event types | Store methods |
|---|---|---|
| `io.sunred.graph.follow` | `follow`, `unfollow` | `RecordFollow`, `DeleteFollow` |
| `io.sunred.share.article` | `share`, `unshare` | `RecordShare`, `DeleteShare` |
| `io.sunred.entry.star` | `star`, `unstar` | `RecordStar`, `DeleteStar` |
| `io.sunred.feed.subscription` | `feedSubscription`, `feedUnsubscription` | `RecordFeedSubscription`, `DeleteFeedSubscription` |

On `delete` ops the relay calls the store delete method and emits the matching
`un*` event if a row was actually removed. On `create`/`update` it fetches the
full record from the PDS via `com.atproto.repo.getRecord` and, if the record is
new (unique key not already present), emits an event with the full record
metadata.

### Backfill (`internal/fanout/backfill.go`)

Newly announced DIDs get a historical backfill-then-cutover ("tap-style")
flow. `BackfillAndSubscribe` paginates `com.atproto.repo.listRecords` across the
four handled collections, processes each record through the same
`process*Record` functions used by the live path (so backfill and live produce
identical events), emits a `backfillComplete` event, and then starts the live
firehose subscription with cursor `0`. Instances use `backfillComplete` to
dismiss waiting UI.

### Event emission

`emit(ctx, eventType, did, payload)` appends a row to `relay_events` (getting
back its monotonic `seq`), then broadcasts the `*RelayEvent` to all registered
subscriber channels. Channels are buffered (cap 256) and sends are
non-blocking — events are dropped if a subscriber's buffer is full.

### Server (`internal/server/server.go`)

The HTTP/WebSocket server exposes XRPC endpoints to Sunred instances.

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | `{"status":"ok"}` liveness probe |
| POST | `/xrpc/io.sunred.relay.announceUser` | Register a new DID; kicks off backfill if new |
| GET | `/xrpc/io.sunred.relay.getFollowerCount` | Global follower count for a DID |
| GET | `/xrpc/io.sunred.relay.getShareCount` | Global share count for a DID |
| GET | `/xrpc/io.sunred.relay.getFeedSubscriptionCount` | Global feed subscription count for a DID |
| GET | `/xrpc/io.sunred.relay.getFeedSubscriberCount` | Global subscriber count for a feed URL |
| GET | `/xrpc/io.sunred.relay.getArticleShareCount` | Global share count for an article URL |
| GET | `/xrpc/io.sunred.relay.searchDIDs` | Cross-instance handle search |
| GET | `/xrpc/io.sunred.relay.resolveHandle` | DID + PDS URL for a handle |
| GET (WS) | `/xrpc/io.sunred.relay.subscribeEvents` | WebSocket event stream for instances |

`announceUser` input: `{did, pdsUrl, instanceUrl, handle}` (`did`, `pdsUrl`,
`instanceUrl` required; `handle` optional). Returns `{tracked: true, new: bool}`
— `new` tells the instance whether to wait for a `backfillComplete` event.

`subscribeEvents` (WebSocket) query params: `instanceUrl`, `cursor` (int64).
To avoid races, it registers for live events **before** replaying: a 256-event
buffer holds events emitted during replay. If `cursor > 0`, `relay_events` with
`seq > cursor` are replayed in batches of 200; then the live loop reads from the
channel, skips events with `seq <= cursor` (already replayed), and sends each
event. A `{"$type":"#ping"}` keepalive is sent after 30s of idle.

## Configuration

The relay is configured entirely through environment variables (loaded via
`godotenv` from `.env` if present). See `.env.example` for a ready-to-copy
template.

| Variable | Default | Description |
|---|---|---|
| `RELAY_HTTP_ADDR` | `:9090` | HTTP listen address |
| `RELAY_DATABASE_URL` | `postgres://sunred:sunred@localhost:5433/sunred_relay?sslmode=disable` | Postgres DSN (required non-empty) |
| `RELAY_LOG_FORMAT` | `pretty` | Log format (`pretty` or `json`) |
| `RELAY_LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `RELAY_FANOUT_WORKERS` | `50` | Loaded but not currently applied — fanout spawns one goroutine per tracked DID |
| `RELAY_RECONNECT_DELAY` | `5s` | Backoff before reconnecting a failed PDS subscription |
| `RELAY_EVENT_RETENTION` | `168h` (7 days) | How long to keep `relay_events` rows for cursor replay |

## Database schema

Migrations live in `internal/migrations/` and are embedded into the binary
(`embed.FS`). `migrations.Run` is a simple, idempotent, transactional runner:
it creates a `schema_migrations` bookkeeping table, then applies each
`*.sql` file in filename order inside its own transaction, recording each as
applied.

| Table | Purpose | Uniqueness |
|---|---|---|
| `instances` | Known Sunred instance URLs that have announced users | `url` |
| `tracked_dids` | DIDs being subscribed to, with per-DID cursor + state (`active`/`error`/`paused`) | `did` |
| `observed_follows` | Global follow graph; stores the record key so deletes decrement cleanly | `(follower_did, followee_did, rkey)` |
| `observed_shares` | One row per share record observed | `(did, rkey)` |
| `observed_subscriptions` | Global feed subscription observations | `(did, rkey)` |
| `relay_events` | Append-only outbound event log (`seq` PK) for cursor-based replay | `seq` |
| `schema_migrations` | Migration bookkeeping | `filename` |

Migration `002_handle_index.sql` adds an index on `tracked_dids.handle` to
support `searchDIDs` / `resolveHandle`.

## Data model

Events written to `relay_events` and sent to subscribers carry the following
payloads (JSON):

- **follow** / **unfollow** — `follower_did`, `followee_did`, `rkey`
- **share** / **unshare** — `articleUrl`, `feedUrl`, `title`, `description`,
  `feedTitle`, `feedSiteUrl`, `author`, `sharedAt`, `publishedAt` (optional)
- **feedSubscription** / **feedUnsubscription** — `feedUrl`, `siteUrl`, `title`,
  `createdAt`
- **backfillComplete** — `did`
- **#ping** — keepalive frame, no payload

## Protocol summary

- **Inbound (relay ← instance):** HTTP XRPC `io.sunred.relay.*`
  (`announceUser` POST; `getFollowerCount`, `getShareCount`,
  `getFeedSubscriptionCount`, `getFeedSubscriberCount`,
  `getArticleShareCount`, `searchDIDs`, `resolveHandle` GET) plus the
  WebSocket `io.sunred.relay.subscribeEvents` (cursor-based replay + live
  fanout).
- **Outbound (relay → PDS):** WebSocket
  `com.atproto.sync.subscribeRepos?wantedDids=<did>&cursor=<seq>` (live
  firehose); HTTP `com.atproto.repo.getRecord` (live record fetch on
  create/update) and `com.atproto.repo.listRecords?limit=100` (backfill
  pagination).

## Running

### Local development

```sh
make db-up      # start local Postgres on :5433
make run        # build + run the relay (migrations run on boot)
```

Copy `.env.example` to `.env` and adjust values as needed.

### Migrate only

```sh
make migrate    # run migrations and exit (equivalent to ./sunred-relay --migrate)
```

### Docker

The Dockerfile is a multi-stage build producing a fully static, stripped binary
in an Alpine 3.20 image with `ca-certificates` and `tzdata`, running as a
non-root user `sunred` (uid 10001). It exposes port `9090` and runs
`/app/sunred-relay` as its entrypoint. Migrations run on every boot.

```sh
docker build -t sunred-relay -f go/relay/Dockerfile go/relay
docker run --env-file go/relay/.env.example -p 9090:9090 sunred-relay
```

### Tests

```sh
make test       # go test ./... -count=1
```

`internal/fanout` tests run against an in-memory stub store and `httptest`
mock PDS, so they need no database. `internal/store` tests are integration
tests against a real Postgres (env `RELAY_DATABASE_URL`, or fallback
`postgres://sunred:sunred@localhost:5432/sunred_relay_test?sslmode=disable`);
they skip if the database is unreachable. The standard local dev database runs
on port `5433`, so a separate `5432` instance is needed to run the store tests.

## Operational notes

- **Graceful shutdown:** `SIGINT`/`SIGTERM` triggers a 10s HTTP shutdown drain.
- **Retention:** `relay_events` is purged hourly; rows older than
  `RELAY_EVENT_RETENTION` (default 7 days) are deleted. Cursor replay only
  works within the retention window — instances reconnecting with an older
  cursor will not receive those events.
- **Reconnect:** failed PDS subscriptions back off `RELAY_RECONNECT_DELAY`
  (default `5s`) and retry indefinitely, marking `tracked_dids.status='error'`.
- **PDS transport caveat:** the firehose is consumed as JSON via the `Accept`
  header; CBOR decoding is not implemented. The source notes that production
  deployments should add CBOR decoding.
