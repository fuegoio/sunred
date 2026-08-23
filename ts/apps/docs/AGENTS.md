<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

## Documentation conventions

This directory is the docs site for Sunred. Content lives in
`content/docs/` as MDX files, split into three sections.

### Sections

**`content/docs/product/`** — usage documentation for end users of Sunred.
Covers what things are and how to use them: subscribing to feeds, reading
entries, using the web UI, the CLI, authentication. Audience: anyone
using a running Sunred instance (mainly the cloud ones).

- No environment variables, no Docker, no server config.
- No internal pipeline details (adaptive polling algorithm, sanitization
  allowlists, request logging). Those belong in `architecture.mdx` only if they
  directly affect user-visible behavior, but even then keep it brief.
- If a section is about running or configuring the server, it does not belong here.

**`content/docs/self-hosting/`** — everything needed to deploy and operate
Sunred: Docker Compose setup, all environment variables, reverse proxy config,
database backups, log format, scheduler tuning. Audience: people running their
own instance.

- This is the only place environment variables should appear.
- Operational details (log format, cleanup frequency, worker pool size) live here,
  not in the product docs.

**`content/docs/openapi/`** — generated API reference from `go/api/openapi.json`.
Do not write prose here; the content is rendered from the OpenAPI spec.

### What does not belong anywhere in the docs site

- Internal implementation details: Go package layout, database schema, SQL queries.
- Test instructions or internal tooling.

### Tone

- Clear, task-oriented, skimmable.
- Imperative mood, short sentences.
- Show the user what to do and what to expect.
