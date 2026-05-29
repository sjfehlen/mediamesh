package catalog

import (
	"context"
	"database/sql"
	"fmt"
)

// BumpVersion increments the node's catalog_version and returns the new value.
// It should be called whenever a local library item is added or meaningfully updated.
func BumpVersion(ctx context.Context, db *sql.DB) (int64, error) {
	_, err := db.ExecContext(ctx,
		`UPDATE node_settings SET catalog_version = catalog_version + 1 WHERE id = 1`)
	if err != nil {
		return 0, fmt.Errorf("catalog.BumpVersion: %w", err)
	}
	var v int64
	if err := db.QueryRowContext(ctx, `SELECT catalog_version FROM node_settings WHERE id = 1`).Scan(&v); err != nil {
		return 0, fmt.Errorf("catalog.BumpVersion: read: %w", err)
	}
	return v, nil
}

// CurrentVersion returns the node's current catalog_version.
func CurrentVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var v int64
	err := db.QueryRowContext(ctx, `SELECT catalog_version FROM node_settings WHERE id = 1`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("catalog.CurrentVersion: %w", err)
	}
	return v, nil
}

// PeerVersion fetches the catalog_version a peer last reported (from the ping response).
// This is stored separately from checkpoints and returned by the peer's ping endpoint.
func CheckpointVersion(ctx context.Context, db *sql.DB, peerID string) (int64, error) {
	var v int64
	err := db.QueryRowContext(ctx,
		`SELECT version FROM catalog_checkpoints WHERE peer_id = ?`, peerID).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}
