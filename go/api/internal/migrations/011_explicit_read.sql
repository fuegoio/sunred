-- History should only contain articles the user actually read: an explicit
-- per-article action (opening an entry, marking one entry read), not the bulk
-- "mark all as read" sweeps. entry_read_status now records whether the
-- current 'read' row was written by an explicit action. Bulk operations
-- (mark all, mark feed, subscribe backlog) insert rows with explicit = false;
-- history filters them out. Marking an entry unread resets the flag, so a
-- later bulk mark does not resurrect the article in history.

ALTER TABLE entry_read_status
  ADD COLUMN IF NOT EXISTS explicit BOOLEAN NOT NULL DEFAULT false;

-- Existing 'read' rows predate the flag and were meaningful reads (the bulk
-- paths existed, but there is no way to tell them apart retroactively);
-- keep the current history intact and only affect future bulk marks.
UPDATE entry_read_status SET explicit = true WHERE status = 'read';

-- Narrow the history index to the rows the history query now lists.
DROP INDEX IF EXISTS idx_entry_read_status_history;
CREATE INDEX idx_entry_read_status_history
  ON entry_read_status (user_id, changed_at DESC)
  WHERE status = 'read' AND explicit;
