CREATE TABLE libraries (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,   -- absolute path inside the container
  media_type TEXT NOT NULL,   -- movie | tv | audiobook | ebook
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_libraries_enabled ON libraries(enabled);
