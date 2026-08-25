-- Cache avatar and banner image URLs for the app.bsky.actor.profile record.
--
-- The profile record stores blob refs (CIDs) for avatar/banner; the API
-- resolves these to public PDS getBlob URLs at backfill time and caches the
-- full URL here so the web can render <img src> directly without a round
-- trip. Empty string = no image set on the Bluesky profile.
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS banner TEXT NOT NULL DEFAULT '';
