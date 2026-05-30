-- SQLite does not support DROP COLUMN before 3.35; rebuild without error_code.
CREATE TABLE audit_log_new AS SELECT id, actor_id, actor_type, action, target_type, target_id, detail, occurred_at FROM audit_log;
DROP TABLE audit_log;
ALTER TABLE audit_log_new RENAME TO audit_log;
