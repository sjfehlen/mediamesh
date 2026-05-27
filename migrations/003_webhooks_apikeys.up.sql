CREATE TABLE api_keys (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  key_hash    TEXT NOT NULL UNIQUE,  -- sha256 of the raw key; raw key shown once on creation
  created_by  TEXT NOT NULL,
  scopes      TEXT NOT NULL,         -- JSON array: ["library:read","requests:write","transfers:read"]
  last_used   DATETIME,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  revoked_at  DATETIME,
  FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE TABLE webhooks (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  url         TEXT NOT NULL,
  secret      TEXT NOT NULL,         -- HMAC-SHA256 signing secret; shown once on creation
  events      TEXT NOT NULL,         -- JSON array of subscribed event types
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_by  TEXT NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE TABLE webhook_deliveries (
  id           TEXT PRIMARY KEY,
  webhook_id   TEXT NOT NULL,
  event        TEXT NOT NULL,
  payload      TEXT NOT NULL,        -- JSON payload sent
  status_code  INTEGER,              -- HTTP response code; NULL = not yet attempted or in-flight
  error        TEXT,
  attempted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (webhook_id) REFERENCES webhooks(id)
);

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_webhook_deliveries_webhook_id ON webhook_deliveries(webhook_id);
CREATE INDEX idx_webhook_deliveries_event ON webhook_deliveries(event);
