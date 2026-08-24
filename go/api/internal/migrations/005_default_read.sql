-- The default read state is now "read": the absence of an entry_read_status
-- row means the article is read. The table only stores deviations — 'unread'
-- and 'removed'. Delete the now-redundant 'read' rows so the table stays lean.
DELETE FROM entry_read_status WHERE status = 'read';
