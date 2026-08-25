-- The entry_read_status.status column now defaults to 'unread'. An explicit
-- INSERT that omits the status (none currently do) will land as 'unread'
-- rather than 'read'. Application semantics are unchanged: a row's absence
-- still means the article is read (COALESCE(rs.status, 'read')); new feed
-- articles become unread via explicit rows the feed processor writes for
-- each subscriber when a new entry is materialized. No data backfill is
-- needed — existing rows keep their stored status.
ALTER TABLE entry_read_status ALTER COLUMN status SET DEFAULT 'unread';
