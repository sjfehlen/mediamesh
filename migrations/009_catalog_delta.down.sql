DROP TABLE IF EXISTS catalog_checkpoints;
DROP TABLE IF EXISTS node_settings;
-- SQLite: recreate library_items without catalog_version column.
CREATE TABLE library_items_backup AS
    SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
           relative_path, file_size, file_mtime, track_count, poster_url, description, last_seen
    FROM library_items;
DROP TABLE library_items;
ALTER TABLE library_items_backup RENAME TO library_items;
