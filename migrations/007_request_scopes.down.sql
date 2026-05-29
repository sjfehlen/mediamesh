-- Restore original requests table with item_id FK and without scope columns.
CREATE TABLE requests_old (
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

INSERT INTO requests_old
  (id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at)
SELECT
  id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at
FROM requests
WHERE item_id != '';

DROP TABLE requests;
ALTER TABLE requests_old RENAME TO requests;

CREATE INDEX idx_requests_user_id ON requests(user_id);
CREATE INDEX idx_requests_status ON requests(status);
