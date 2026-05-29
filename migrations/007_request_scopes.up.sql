-- Add request scope columns and remove item_id FK constraint
-- (item_id may be empty string '' when scope != 'item').
-- SQLite requires table recreation to drop a FK constraint.

CREATE TABLE requests_new (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL,
  item_id       TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'pending',
  note          TEXT,
  reviewed_by   TEXT,
  review_note   TEXT,
  requested_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  reviewed_at   DATETIME,
  request_scope TEXT NOT NULL DEFAULT 'item',
  series_name   TEXT,
  req_season_num INTEGER,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

INSERT INTO requests_new
  (id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at,
   request_scope, series_name, req_season_num)
SELECT
  id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at,
  'item', NULL, NULL
FROM requests;

DROP TABLE requests;
ALTER TABLE requests_new RENAME TO requests;

CREATE INDEX idx_requests_user_id ON requests(user_id);
CREATE INDEX idx_requests_status ON requests(status);
