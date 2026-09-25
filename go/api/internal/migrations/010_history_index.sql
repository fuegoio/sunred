-- Index backing the read-history listing: entry_read_status rows with an
-- explicit 'read' status, ordered by changed_at (the time of the most recent
-- read). Partial because only 'read' rows are listed; 'unread' and 'removed'
-- rows are excluded by the history query.

CREATE INDEX IF NOT EXISTS idx_entry_read_status_history
  ON entry_read_status (user_id, changed_at DESC)
  WHERE status = 'read';
