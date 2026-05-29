-- catalog_version tracks which version of the local catalog was current when each item was last modified.
ALTER TABLE library_items ADD COLUMN catalog_version INTEGER NOT NULL DEFAULT 0;

-- node_settings stores singleton node-level counters. One row always exists.
CREATE TABLE node_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    catalog_version INTEGER NOT NULL DEFAULT 0
);
INSERT INTO node_settings (id, catalog_version) VALUES (1, 0);

-- catalog_checkpoints records the last catalog_version successfully synced to each peer.
CREATE TABLE catalog_checkpoints (
    peer_id    TEXT    NOT NULL PRIMARY KEY,
    version    INTEGER NOT NULL DEFAULT 0,
    synced_at  DATETIME NOT NULL,
    FOREIGN KEY (peer_id) REFERENCES peers (id) ON DELETE CASCADE
);
