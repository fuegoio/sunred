# Website app

The website (`ts/apps/website/`) is an Astro app deployed to Cloudflare
Workers. It is the front door for sunred.app: it serves the marketing pages,
the blog, and the docs, and proxies every other path to the Next.js app
(see `src/middleware.ts` and `src/pages/[...path].ts`).

## Layout

- Marketing and blog pages: `src/pages/`, content in `src/content/blog/`.
- Docs: MDX content in `src/content/docs/`, rendered with Fumadocs via React
  islands (`src/components/docs/`). Sections: `product/` (served at `/docs`),
  `self-hosting/` (`/docs/self-hosting`), `openapi/` (`/docs/api-reference`).
- The OpenAPI spec lives in `openapi/openapi.json`, copied there by
  `make gen-openapi` from `go/api/`. Never edit it by hand.
- Deployment: `pnpm deploy` (builds and runs `wrangler deploy`). Worker
  bindings and the `APP_URL` var live in `wrangler.jsonc`.

## Docs content conventions

Content lives in `src/content/docs/` as MDX files, split into three sections.

### Sections

**`src/content/docs/product/`** — usage documentation for end users of Sunred.
Covers what things are and how to use them: subscribing to feeds, reading
entries, using the web UI, the CLI, authentication. Audience: anyone
using a running Sunred instance (mainly the cloud ones).

- No environment variables, no Docker, no server config.
- No internal pipeline details (adaptive polling algorithm, sanitization
  allowlists, request logging). Those belong in `architecture.mdx` only if they
  directly affect user-visible behavior, but even then keep it brief.
- If a section is about running or configuring the server, it does not belong here.

**`src/content/docs/self-hosting/`** — everything needed to deploy and operate
Sunred: Docker Compose setup, all environment variables, reverse proxy config,
database backups, log format, scheduler tuning. Audience: people running their
own instance.

- This is the only place environment variables should appear.
- Operational details (log format, cleanup frequency, worker pool size) live here,
  not in the product docs.

**`src/content/docs/openapi/`** — generated API reference from `go/api/openapi.json`.
Do not write prose here; the content is rendered from the OpenAPI spec.

### What does not belong anywhere in the docs

- Internal implementation details: Go package layout, database schema, SQL queries.
- Test instructions or internal tooling.

## Writing style

The reference for tone is Linear's documentation (e.g.
https://linear.app/docs/editing-issues). Read it if you can. The goal is prose
that sounds like a sharp colleague explaining the product, not a manual.
Concrete rules:

### Structure

- **Titles are plain verb phrases** — "Edit issues", not "Issue Editing
  Capabilities". The frontmatter `description` is a short fragment
  ("Making changes to an issue."), never a marketing line.
- **Headings are actions or questions the reader has** — "Move an issue to
  another team", "Subscribe to a feed" — not nouns ("Feeds", "Entries").
  An index page may use a noun title; everything else should name the task.
- **Lead with why, then how.** One or two sentences of context before the
  mechanics: when would you do this, what happens as a result. Linear's
  pattern: _"When work needs to be passed over to another team, issues can be
  moved…"_ — the action is framed by the situation, not the other way around.
- **Short sections.** A heading every 2–4 short paragraphs. If a section runs
  long, it's covering two topics.

### Voice

- **Second person, present tense.** "Click the star icon. The entry moves to
  Starred." Not "the user can click", not "the star icon will be clicked".
- **Write like you speak, then trim.** Contractions are fine ("you don't need to",
  "it's there when you want it"). Short sentences. Vary their length —
  a run of same-length sentences reads like a robot.
- **An honest aside beats a hedge.** Linear writes _"unfortunately, this
  doesn't work for old issue titles"_ — a plain admission in the flow of a
  sentence. When something is awkward or limited, just say so, briefly,
  in the same voice as the rest of the page. Don't wrap every caveat in a
  warning callout.
- **No hype.** No "powerful", "seamless", "elegant", "robust", no exclamation
  marks. The product is interesting or it isn't; the prose shouldn't insist.
- **Confident but honest.** Say what the product does plainly, including
  where it stops. "The reader doesn't render the full article, because most
  feeds only carry a summary" — a limit stated with its reason reads as
  design, not weakness.

### Mechanics on the page

- **Prose first, lists as a last resort.** Short steps belong in sentences.
  Use bullets only when every item is genuinely parallel and a sentence
  would turn into mush. Prefer "the toolbar has everything you need to
  manage the feed" followed by the one action the reader actually came for.
- **Bold UI labels** (**Refresh**, **Settings → Tokens**), `code` for CLI
  commands, config values, and IDs. Keyboard shortcuts inline where the action
  is described, not in a separate table.
- **Tables only for reference data** — a compact grid the reader will scan or
  compare (formats, statuses, properties and their effects). Never use a table
  to write prose sideways. If a table's cells hold full sentences, it should
  be paragraphs.
- **Code blocks** show a real, runnable command with a comment line for the
  interesting part — not a stylized fragment with placeholders everywhere.
- **Link, don't repeat.** When a topic has its own page, mention it in one
  sentence and link. Restating the mechanism in three places is how docs rot.
