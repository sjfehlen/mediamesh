-- SQLite does not support DROP COLUMN in older versions; recreate table without the columns.
CREATE TABLE library_items_backup AS SELECT
  id, peer_id, media_type, title, year, series, season_num, episode_num,
  relative_path, file_size, tmdb_id, ol_key, poster_url, description,
  rating, genres, last_seen, metadata_at
FROM library_items;

DROP TABLE library_items;

ALTER TABLE library_items_backup RENAME TO library_items;
