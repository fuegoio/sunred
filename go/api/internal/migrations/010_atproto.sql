-- AT Protocol integration.
--
-- Each user can connect their Sunred account to an AT Proto identity (DID).
-- The DID is the globally stable identifier; the PDS is where their repo lives.
-- When connected, Sunred writes io.sunred.* records to the PDS on every
-- relevant action (follow, share, feed subscription, profile update).
--
-- The atproto_rkey columns record the record key of the last written AT Proto
-- record so we can update or delete it in place rather than creating duplicates.

-- Extend user_profiles with AT Proto identity.
ALTER TABLE user_profiles
  ADD COLUMN IF NOT EXISTS did          TEXT,
  ADD COLUMN IF NOT EXISTS pds_url      TEXT,
  ADD COLUMN IF NOT EXISTS atproto_access_token   TEXT,
  ADD COLUMN IF NOT EXISTS atproto_refresh_token  TEXT,
  ADD COLUMN IF NOT EXISTS atproto_token_expires_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_profiles_did ON user_profiles (did) WHERE did IS NOT NULL;

-- Track which AT Proto follow record corresponds to each local follow row
-- so we can delete it cleanly on unfollow.
ALTER TABLE user_follows
  ADD COLUMN IF NOT EXISTS atproto_rkey TEXT;

-- Track the AT Proto record key for each shared article.
ALTER TABLE shared_articles
  ADD COLUMN IF NOT EXISTS atproto_rkey TEXT;

-- Track the AT Proto feed subscription record key for each feed row.
ALTER TABLE feeds
  ADD COLUMN IF NOT EXISTS atproto_rkey TEXT;

-- Sequence counter for relay events received by this instance.
-- Allows resuming relay WebSocket subscriptions from a known point.
CREATE TABLE IF NOT EXISTS atproto_relay_cursor (
  id          INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1), -- singleton row
  relay_url   TEXT NOT NULL DEFAULT '',
  cursor_seq  BIGINT NOT NULL DEFAULT 0,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO atproto_relay_cursor (id) VALUES (1) ON CONFLICT DO NOTHING;
