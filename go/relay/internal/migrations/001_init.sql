-- Sunred relay initial schema.
--
-- Single consolidated migration representing the final schema after the
-- iterative 001..002 migrations.
--
-- The relay tracks a set of DIDs across federated Sunred instances. For each
-- tracked DID it maintains a persistent WebSocket subscription to the DID's
-- PDS repo stream and aggregates io.sunred.* record events into global counts.

-- Known Sunred instances that have announced users to this relay.
CREATE TABLE instances (
  id         SERIAL PRIMARY KEY,
  url        TEXT NOT NULL UNIQUE,
  first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every DID the relay is tracking, keyed by the DID string.
CREATE TABLE tracked_dids (
  id            BIGSERIAL PRIMARY KEY,
  did           TEXT NOT NULL UNIQUE,
  pds_url       TEXT NOT NULL,
  handle        TEXT NOT NULL DEFAULT '',
  instance_id   INTEGER REFERENCES instances (id) ON DELETE SET NULL,
  cursor_seq    BIGINT NOT NULL DEFAULT 0,
  status        VARCHAR(32) NOT NULL DEFAULT 'active',
  error_msg     TEXT NOT NULL DEFAULT '',
  announced_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_event_at TIMESTAMPTZ
);
CREATE INDEX idx_tracked_dids_pds_url ON tracked_dids (pds_url);
CREATE INDEX idx_tracked_dids_status  ON tracked_dids (status);
CREATE INDEX idx_tracked_dids_handle  ON tracked_dids (handle);
-- A handle resolves to at most one DID; enforce uniqueness for non-empty handles.
CREATE UNIQUE INDEX idx_tracked_dids_handle_unique ON tracked_dids (handle) WHERE handle <> '';

-- Global follower counts: for each (follower_did, followee_did) pair seen
-- across any tracked PDS repo, we store the follow record key so we can
-- decrement the count cleanly on delete.
CREATE TABLE observed_follows (
  id           BIGSERIAL PRIMARY KEY,
  follower_did TEXT NOT NULL,
  followee_did TEXT NOT NULL,
  rkey         TEXT NOT NULL,
  pds_url      TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (follower_did, followee_did, rkey)
);
CREATE INDEX idx_observed_follows_followee ON observed_follows (followee_did);
CREATE INDEX idx_observed_follows_follower ON observed_follows (follower_did);

-- Global share observations: one row per (did, rkey) share record seen.
CREATE TABLE observed_shares (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  article_url TEXT NOT NULL DEFAULT '',
  feed_url    TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL DEFAULT '',
  pds_url     TEXT NOT NULL,
  shared_at   TIMESTAMPTZ,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE INDEX idx_observed_shares_did ON observed_shares (did);

-- Global feed subscription observations.
CREATE TABLE observed_subscriptions (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  feed_url    TEXT NOT NULL,
  pds_url     TEXT NOT NULL,
  created_at  TIMESTAMPTZ,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE INDEX idx_observed_subs_feed_url ON observed_subscriptions (feed_url);
CREATE INDEX idx_observed_subs_did      ON observed_subscriptions (did);

-- Global star observations: one row per (did, rkey) star record seen.
CREATE TABLE observed_stars (
  id          BIGSERIAL PRIMARY KEY,
  did         TEXT NOT NULL,
  rkey        TEXT NOT NULL,
  article_url TEXT NOT NULL DEFAULT '',
  pds_url     TEXT NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (did, rkey)
);
CREATE INDEX idx_observed_stars_did ON observed_stars (did);

-- Outbound event log for subscribeEvents WebSocket fanout to instances.
-- Instances read from this log when they reconnect with a cursor.
CREATE TABLE relay_events (
  seq        BIGSERIAL PRIMARY KEY,
  event_type VARCHAR(32) NOT NULL,
  did        TEXT NOT NULL,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_relay_events_created_at ON relay_events (created_at DESC);
CREATE INDEX idx_relay_events_did        ON relay_events (did);
