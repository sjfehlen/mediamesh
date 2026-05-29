-- SQLite doesn't support DROP COLUMN natively before 3.35
-- Recreate the table without the new columns
CREATE TABLE peers_backup AS SELECT id, display_name, endpoint, public_key, status, added_at FROM peers;
DROP TABLE peers;
ALTER TABLE peers_backup RENAME TO peers;
