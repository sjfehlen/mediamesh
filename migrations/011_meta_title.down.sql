-- SQLite ALTER TABLE does not support DROP COLUMN before 3.35; rebuild the table.
CREATE TABLE library_items_new AS SELECT id, peer_id, media_type, title, year, series,
    season_num, episode_num, root_path, relative_path, file_size, tmdb_id, ol_key,
    poster_url, description, rating, genres, last_seen, metadata_at, file_mtime,
    track_count, catalog_version
FROM library_items;
DROP TABLE library_items;
ALTER TABLE library_items_new RENAME TO library_items;
