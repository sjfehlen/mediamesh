CREATE TABLE peers (
  id            TEXT PRIMARY KEY,
  display_name  TEXT NOT NULL,
  endpoint      TEXT NOT NULL,
  public_key    BLOB NOT NULL,
  status        TEXT NOT NULL DEFAULT 'active',
  added_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE library_items (
  id            TEXT PRIMARY KEY,
  peer_id       TEXT,
  media_type    TEXT NOT NULL,
  title         TEXT NOT NULL,
  year          INTEGER,
  series        TEXT,
  season_num    INTEGER,
  episode_num   INTEGER,
  relative_path TEXT NOT NULL,
  file_size     INTEGER,
  tmdb_id       INTEGER,
  ol_key        TEXT,
  poster_url    TEXT,
  description   TEXT,
  rating        REAL,
  genres        TEXT,
  last_seen     DATETIME NOT NULL,
  metadata_at   DATETIME,
  FOREIGN KEY (peer_id) REFERENCES peers(id)
);

CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  display_name  TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'member',
  auto_approve  INTEGER NOT NULL DEFAULT 0,
  can_request   INTEGER NOT NULL DEFAULT 1,
  quota_gb      INTEGER,
  libraries     TEXT,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  disabled_at   DATETIME
);

CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at  DATETIME NOT NULL,
  revoked_at  DATETIME,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE requests (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL,
  item_id      TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending',
  note         TEXT,
  reviewed_by  TEXT,
  review_note  TEXT,
  requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  reviewed_at  DATETIME,
  FOREIGN KEY (user_id) REFERENCES users(id),
  FOREIGN KEY (item_id) REFERENCES library_items(id)
);

CREATE TABLE transfers (
  id           TEXT PRIMARY KEY,
  request_id   TEXT NOT NULL,
  peer_id      TEXT NOT NULL,
  item_id      TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'queued',
  bytes_total  INTEGER,
  bytes_done   INTEGER NOT NULL DEFAULT 0,
  error        TEXT,
  queued_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at   DATETIME,
  completed_at DATETIME,
  FOREIGN KEY (request_id) REFERENCES requests(id),
  FOREIGN KEY (peer_id) REFERENCES peers(id)
);

CREATE TABLE audit_log (
  id           TEXT PRIMARY KEY,
  actor_id     TEXT,
  actor_type   TEXT NOT NULL,
  action       TEXT NOT NULL,
  target_type  TEXT,
  target_id    TEXT,
  detail       TEXT,
  occurred_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE invite_tokens (
  token        TEXT PRIMARY KEY,
  created_by   TEXT NOT NULL,
  token_type   TEXT NOT NULL,
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at   DATETIME NOT NULL,
  used_at      DATETIME,
  FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE INDEX idx_library_items_peer_id ON library_items(peer_id);
CREATE INDEX idx_library_items_media_type ON library_items(media_type);
CREATE INDEX idx_requests_user_id ON requests(user_id);
CREATE INDEX idx_requests_status ON requests(status);
CREATE INDEX idx_transfers_status ON transfers(status);
CREATE INDEX idx_audit_log_occurred_at ON audit_log(occurred_at);
CREATE INDEX idx_audit_log_actor_id ON audit_log(actor_id);
