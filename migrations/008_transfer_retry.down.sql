-- SQLite does not support DROP COLUMN cleanly; recreate table without retry columns.
CREATE TABLE transfers_backup AS SELECT id, request_id, peer_id, item_id, status, bytes_total, bytes_done, error, queued_at, started_at, completed_at FROM transfers;
DROP TABLE transfers;
ALTER TABLE transfers_backup RENAME TO transfers;
