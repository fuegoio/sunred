-- Split entry_state into two URL-keyed tables: entry_stars and
-- entry_read_status. Both are keyed by (user_id, article_url) instead of
-- (user_id, entry_id), so star/read state can exist before the feed is
-- materialized locally. entry_id is a nullable cache column, repopulated
-- when the entry is fetched.

CREATE TABLE entry_stars (
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
  atproto_rkey  TEXT,
  starred_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, article_url)
);
CREATE INDEX idx_entry_stars_entry_id ON entry_stars (entry_id);
CREATE INDEX idx_entry_stars_rkey     ON entry_stars (user_id, atproto_rkey);

CREATE TABLE entry_read_status (
  user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  article_url TEXT NOT NULL,
  entry_id   BIGINT REFERENCES entries (id) ON DELETE SET NULL,
  status     VARCHAR(50) NOT NULL DEFAULT 'read',
  changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, article_url)
);
CREATE INDEX idx_entry_read_status_entry_id    ON entry_read_status (entry_id);
CREATE INDEX idx_entry_read_status_user_status ON entry_read_status (user_id, status);

-- Backfill stars from entry_state (only rows where starred = true).
-- DISTINCT ON deduplicates by (user_id, article_url) in case the same
-- article URL appears in multiple feeds (multiple entry_id values).
INSERT INTO entry_stars (user_id, article_url, entry_id, atproto_rkey, starred_at)
SELECT DISTINCT ON (es.user_id, e.url)
       es.user_id, e.url, es.entry_id, es.atproto_rkey, es.changed_at
FROM entry_state es
JOIN entries e ON e.id = es.entry_id
WHERE es.starred = true
ORDER BY es.user_id, e.url, es.changed_at DESC;

-- Backfill read status from entry_state (only rows where status <> 'unread').
INSERT INTO entry_read_status (user_id, article_url, entry_id, status, changed_at)
SELECT DISTINCT ON (es.user_id, e.url)
       es.user_id, e.url, es.entry_id, es.status, es.changed_at
FROM entry_state es
JOIN entries e ON e.id = es.entry_id
WHERE es.status <> 'unread'
ORDER BY es.user_id, e.url, es.changed_at DESC;

DROP TABLE entry_state;
