-- Enforce handle uniqueness across tracked_dids.
--
-- A handle should resolve to at most one DID. Previously handle had no
-- uniqueness constraint, so stale rows from DID rotations could leave two
-- rows sharing the same handle, which surfaced as duplicate search results.
--
-- This migration cleans up existing duplicates (keeping the most recently
-- announced row) and adds a partial unique index so the invariant is
-- enforced going forward.

-- For every handle that appears on more than one row, clear the handle on
-- all but the most recently announced row.
UPDATE tracked_dids t
SET handle = ''
WHERE t.handle <> ''
  AND t.id <> (
    SELECT id
    FROM tracked_dids t2
    WHERE t2.handle = t.handle
    ORDER BY announced_at DESC, id DESC
    LIMIT 1
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_tracked_dids_handle_unique
  ON tracked_dids (handle)
  WHERE handle <> '';
