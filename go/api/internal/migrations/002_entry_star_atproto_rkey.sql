-- Add atproto_rkey column to entry_state for tracking the PDS record key
-- of io.sunred.entry.star records. Used to delete the star record from the
-- user's PDS when they unstar an entry.
ALTER TABLE entry_state ADD COLUMN IF NOT EXISTS atproto_rkey TEXT;
