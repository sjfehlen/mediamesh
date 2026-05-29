-- Add library_id FK with cascade delete so removing a library removes all its items.
-- SQLite doesn't support adding FK constraints via ALTER TABLE, so we recreate the table.
CREATE TABLE library_items_new (
  id               TEXT PRIMARY KEY,
  library_id       TEXT REFERENCES libraries(id) ON DELETE CASCADE,
  peer_id          TEXT,
  media_type       TEXT NOT NULL,
  title            TEXT NOT NULL,
  meta_title       TEXT,
  year             INTEGER,
  series           TEXT,
  season_num       INTEGER,
  episode_num      INTEGER,
  root_path        TEXT NOT NULL DEFAULT '',
  relative_path    TEXT NOT NULL,
  file_size        INTEGER,
  tmdb_id          INTEGER,
  ol_key           TEXT,
  poster_url       TEXT,
  description      TEXT,
  rating           REAL,
  genres           TEXT,
  last_seen        DATETIME NOT NULL,
  metadata_at      DATETIME,
  file_mtime       DATETIME,
  track_count      INTEGER,
  catalog_version  INTEGER,
  FOREIGN KEY (peer_id) REFERENCES peers(id)
);

INSERT INTO library_items_new
  SELECT id, NULL, peer_id, media_type, title, meta_title, year, series, season_num,
         episode_num, root_path, relative_path, file_size, tmdb_id, ol_key, poster_url,
         description, rating, genres, last_seen, metadata_at, file_mtime, track_count,
         catalog_version
  FROM library_items;

DROP TABLE library_items;
ALTER TABLE library_items_new RENAME TO library_items;
