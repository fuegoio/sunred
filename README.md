<p align="center">
  <img src="assets/logo.svg" alt="Sunred logo" width="120" />
</p>

# Sunred

A self-hosted RSS reader with ATProto-based federation: instances can link
user identities and share activity (followers, reposts, feed subscriptions)
through a relay, while articles and profiles stay on your own server.

## Repository structure

```
sunred/
├── go/                      # Go (API server, relay, SDK, CLI/TUI)
│   ├── go.work              # Go workspace — links all modules
│   ├── api/                 # API server (huma, PostgreSQL)
│   ├── relay/               # federation relay (ATProto firehose, PostgreSQL)
│   ├── sdk/                 # Go client generated from OpenAPI (oapi-codegen)
│   └── cli/                 # CLI + TUI (cobra, bubbletea)
├── ts/                      # TypeScript (web, docs, website, shared packages)
│   ├── apps/
│   │   ├── web/             # Next.js frontend
│   │   ├── website/         # Astro marketing site (one-pager, blog)
│   │   └── docs/            # Fumadocs documentation site
│   └── packages/
│       ├── api-client/      # TS client generated from OpenAPI (openapi-ts)
│       ├── ui/              # Shared UI components
│       ├── eslint-config/
│       └── typescript-config/
├── Makefile                 # Root orchestration (make gen, make dev, ...)
└── .github/workflows/       # CI
```

## Quick start

### Prerequisites

- Go 1.25+
- Node.js 20+ with pnpm 10+
- PostgreSQL 16+ (or Docker for local dev)

### API server

```bash
cd go/api
make db-up          # start PostgreSQL via docker compose
make migrate        # run database migrations
make run            # start the API server on :8080
```

### CLI

```bash
cd go/cli
make build
./sunred config set base_url http://localhost:8080
./sunred config set token <your-api-token>
./sunred feeds list
./sunred-tui      # interactive TUI
```

### Relay (optional)

The relay federates activity across independent instances by subscribing to
announced users' PDS repo streams and fanning events back out over WebSocket:

```bash
cd go/relay
make db-up          # start PostgreSQL on :5433 via docker compose
make migrate        # run relay database migrations
make run            # start the relay
```

### Web frontend

```bash
cd ts
pnpm install
pnpm dev
```

The frontend ships a first-run onboarding tour, one-click subscribe from any
article to its source feed, and optional Umami telemetry.

### Code generation

Both the Go SDK and TS client are generated from the OpenAPI spec:

```bash
make gen             # from repo root — regenerates spec + both clients
```

## License

[MIT](LICENSE)
