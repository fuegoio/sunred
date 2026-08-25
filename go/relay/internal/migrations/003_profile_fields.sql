-- Cache the app.bsky.actor.profile fields for tracked DIDs so the relay can
-- serve display name, bio, and avatar/banner image URLs to instances without
-- each instance having to read the PDS record. avatar/banner are full public
-- getBlob URLs (pds + did + cid) resolved from the blob refs at backfill time.
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS bio        TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS avatar     TEXT NOT NULL DEFAULT '';
ALTER TABLE tracked_dids ADD COLUMN IF NOT EXISTS banner     TEXT NOT NULL DEFAULT '';
