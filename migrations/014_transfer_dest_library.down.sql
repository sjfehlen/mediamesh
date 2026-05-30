CREATE TABLE transfers_new AS SELECT id, request_id, peer_id, item_id, status, bytes_total,
  bytes_done, error, retry_count, next_retry_at, queued_at, started_at, completed_at FROM transfers;
DROP TABLE transfers;
ALTER TABLE transfers_new RENAME TO transfers;
