-- Sunred API initial schema.
--
-- Single consolidated migration representing the final schema after the
-- iterative 001..015 migrations. Not in production; existing data is dropped
-- rather than migrated.
--
-- Identity is ATProto DID-based: users log in via OAuth with their PDS. Feeds
-- are global (one row per feed_url); per-user state lives in subscriptions and
-- entry_state. Shared articles materialize as global entries so articles
-- shared by followed users appear in the entry stream.

-- --- Users ---

CREATE TABLE users (
  id                       SERIAL PRIMARY KEY,
  did                      TEXT,
  handle                   TEXT,
  display_name             TEXT NOT NULL DEFAULT '',
  bio                      TEXT NOT NULL DEFAULT '',
  pds_url                  TEXT,
  atproto_access_token     TEXT,
  atproto_refresh_token    TEXT,
  atproto_token_expires_at TIMESTAMPTZ,
  oauth_session_id         TEXT,
  pds_sync_status          TEXT NOT NULL DEFAULT 'idle',
  pds_synced_at            TIMESTAMPTZ,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_users_did    ON users (did)    WHERE did IS NOT NULL;
CREATE UNIQUE INDEX idx_users_handle ON users (handle) WHERE handle IS NOT NULL;

-- --- Folders (nested, per-user) ---

CREATE TABLE folders (
  id         SERIAL PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  parent_id  INTEGER REFERENCES folders (id) ON DELETE SET NULL,
  title      VARCHAR(255) NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_folders_user_id   ON folders (user_id);
CREATE INDEX idx_folders_parent_id ON folders (parent_id);

-- --- Global feeds ---

CREATE TABLE feeds (
  id                    SERIAL PRIMARY KEY,
  feed_url              TEXT NOT NULL,
  site_url              TEXT NOT NULL DEFAULT '',
  title                 VARCHAR(512) NOT NULL DEFAULT '',
  description           TEXT NOT NULL DEFAULT '',
  etag_header           TEXT NOT NULL DEFAULT '',
  last_modified_header  TEXT NOT NULL DEFAULT '',
  parsing_error         TEXT NOT NULL DEFAULT '',
  parsing_error_count   INTEGER NOT NULL DEFAULT 0,
  disabled              BOOLEAN NOT NULL DEFAULT false,
  scraper_rules         TEXT NOT NULL DEFAULT '',
  rewrite_rules         TEXT NOT NULL DEFAULT '',
  crawler               BOOLEAN NOT NULL DEFAULT false,
  next_check_at         TIMESTAMPTZ,
  last_fetch_at         TIMESTAMPTZ,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_feeds_feed_url      ON feeds (feed_url);
CREATE INDEX        idx_feeds_next_check_at ON feeds (next_check_at) WHERE disabled = false;

-- --- Per-user subscriptions to global feeds ---

CREATE TABLE subscriptions (
  id             SERIAL PRIMARY KEY,
  user_id        INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  feed_id        INTEGER NOT NULL REFERENCES feeds (id) ON DELETE CASCADE,
  folder_id      INTEGER REFERENCES folders (id) ON DELETE SET NULL,
  title_override VARCHAR(512),
  sort_order     INTEGER NOT NULL DEFAULT 0,
  atproto_rkey   TEXT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, feed_id)
);
CREATE INDEX idx_subscriptions_user_id   ON subscriptions (user_id);
CREATE INDEX idx_subscriptions_feed_id   ON subscriptions (feed_id);
CREATE INDEX idx_subscriptions_folder_id ON subscriptions (folder_id);

-- --- Global entries ---

CREATE TABLE entries (
  id            BIGSERIAL PRIMARY KEY,
  feed_id       INTEGER NOT NULL REFERENCES feeds (id) ON DELETE CASCADE,
  hash          VARCHAR(255) NOT NULL,
  title         TEXT NOT NULL DEFAULT '',
  url           TEXT NOT NULL DEFAULT '',
  comments_url  TEXT NOT NULL DEFAULT '',
  author        VARCHAR(255) NOT NULL DEFAULT '',
  content       TEXT NOT NULL DEFAULT '',
  description   TEXT NOT NULL DEFAULT '',
  published_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  tags          TEXT[] NOT NULL DEFAULT '{}',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  document      tsvector GENERATED ALWAYS AS (to_tsvector('english', coalesce(title, '') || ' ' || coalesce(content, ''))) STORED
);
CREATE UNIQUE INDEX idx_entries_feed_hash     ON entries (feed_id, hash);
CREATE INDEX        idx_entries_feed_id       ON entries (feed_id);
CREATE INDEX        idx_entries_published_at  ON entries (published_at DESC);
CREATE INDEX        idx_entries_document      ON entries USING gin (document);

-- --- Per-user entry state (read/starred/liked) ---

CREATE TABLE entry_state (
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  entry_id   BIGINT NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
  status     VARCHAR(50) NOT NULL DEFAULT 'unread',
  starred    BOOLEAN NOT NULL DEFAULT false,
  liked      BOOLEAN NOT NULL DEFAULT false,
  changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, entry_id)
);
CREATE INDEX idx_entry_state_user_status   ON entry_state (user_id, status);
CREATE INDEX idx_entry_state_user_starred ON entry_state (user_id, starred);

-- --- Enclosures & feed icons ---

CREATE TABLE enclosures (
  id        BIGSERIAL PRIMARY KEY,
  entry_id  BIGINT NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
  url       TEXT NOT NULL,
  mime_type VARCHAR(255) NOT NULL DEFAULT '',
  size      BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_enclosures_entry_id ON enclosures (entry_id);

CREATE TABLE feed_icons (
  feed_id INTEGER PRIMARY KEY REFERENCES feeds (id) ON DELETE CASCADE,
  data    BYTEA NOT NULL
);

-- --- API tokens (manual + device-flow) ---

CREATE TABLE api_tokens (
  id            SERIAL PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  label         VARCHAR(255) NOT NULL DEFAULT '',
  token_hash    VARCHAR(255) NOT NULL,
  origin        VARCHAR(32) NOT NULL DEFAULT 'manual',
  expires_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_api_tokens_token_hash ON api_tokens (token_hash);
CREATE INDEX        idx_api_tokens_user_id     ON api_tokens (user_id);

-- --- Device authorization grant (RFC 8628) ---

CREATE TABLE device_codes (
  id              BIGSERIAL PRIMARY KEY,
  device_code     TEXT NOT NULL,
  user_code       TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending',
  user_id         INTEGER REFERENCES users (id) ON DELETE CASCADE,
  token_id        INTEGER REFERENCES api_tokens (id) ON DELETE SET NULL,
  token_plaintext TEXT,
  interval_s      INTEGER NOT NULL DEFAULT 5,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_polled_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_device_codes_device_code      ON device_codes (device_code);
CREATE UNIQUE INDEX idx_device_codes_user_code        ON device_codes (user_code);
CREATE INDEX        idx_device_codes_status_expires   ON device_codes (status, expires_at);

-- --- Social: follows & shared articles ---

CREATE TABLE user_follows (
  id           BIGSERIAL PRIMARY KEY,
  follower_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  followee_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  atproto_rkey TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (follower_id, followee_id)
);
CREATE INDEX idx_user_follows_follower ON user_follows (follower_id);
CREATE INDEX idx_user_follows_followee ON user_follows (followee_id);

CREATE TABLE shared_articles (
  id            BIGSERIAL PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  article_url   TEXT NOT NULL,
  entry_id      BIGINT REFERENCES entries (id) ON DELETE SET NULL,
  title         TEXT NOT NULL DEFAULT '',
  description   TEXT NOT NULL DEFAULT '',
  feed_url      TEXT NOT NULL DEFAULT '',
  feed_title    VARCHAR(512) NOT NULL DEFAULT '',
  feed_site_url TEXT NOT NULL DEFAULT '',
  author        VARCHAR(255) NOT NULL DEFAULT '',
  published_at  TIMESTAMPTZ,
  shared_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  atproto_rkey  TEXT,
  UNIQUE (user_id, article_url)
);
CREATE INDEX idx_shared_articles_user_id   ON shared_articles (user_id);
CREATE INDEX idx_shared_articles_shared_at ON shared_articles (shared_at DESC);
CREATE INDEX idx_shared_articles_entry_id  ON shared_articles (entry_id);

-- --- ATProto relay cursor (singleton) ---

CREATE TABLE atproto_relay_cursor (
  id         INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  relay_url  TEXT NOT NULL DEFAULT '',
  cursor_seq BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO atproto_relay_cursor (id) VALUES (1) ON CONFLICT DO NOTHING;

-- --- OAuth client persistence (indigo) ---

CREATE TABLE oauth_auth_requests (
  state      TEXT PRIMARY KEY,
  data       JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_oauth_auth_requests_created_at ON oauth_auth_requests (created_at);

CREATE TABLE oauth_sessions (
  account_did TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  data        JSONB NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (account_did, session_id)
);

-- --- Web session cookie -> user_id ---

CREATE TABLE web_sessions (
  token      TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_web_sessions_user_id ON web_sessions (user_id);
