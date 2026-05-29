package peers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sjfehlen/mediamesh/internal/catalog"
)

// CatalogItem is a catalog entry sent between peers.
type CatalogItem struct {
	ID             string  `json:"id"`
	MediaType      string  `json:"media_type"`
	Title          string  `json:"title"`
	Year           *int    `json:"year,omitempty"`
	Series         *string `json:"series,omitempty"`
	SeasonNum      *int    `json:"season_num,omitempty"`
	EpisodeNum     *int    `json:"episode_num,omitempty"`
	RelativePath   string  `json:"relative_path"`
	FileSize       *int64  `json:"file_size,omitempty"`
	PosterURL      *string `json:"poster_url,omitempty"`
	Description    *string `json:"description,omitempty"`
	CatalogVersion int64   `json:"catalog_version"`
}

// CatalogPush is the envelope sent to a peer's /api/peer/catalog endpoint.
type CatalogPush struct {
	SenderVersion int64         `json:"sender_version"`
	IsDelta       bool          `json:"is_delta"`
	Items         []CatalogItem `json:"items"`
}

const fullSyncThreshold = 1000

// PushCatalog sends local catalog items to a peer, using delta sync when possible.
// Only items with catalog_version > the peer's last checkpoint are sent.
// Falls back to a full sync if the gap exceeds fullSyncThreshold.
func (m *Manager) PushCatalog(ctx context.Context, peer *Peer) error {
	localVersion, err := catalog.CurrentVersion(ctx, m.db)
	if err != nil {
		return fmt.Errorf("peers.PushCatalog: get local version: %w", err)
	}

	checkpoint, err := catalog.CheckpointVersion(ctx, m.db, peer.ID)
	if err != nil {
		return fmt.Errorf("peers.PushCatalog: get checkpoint: %w", err)
	}

	gap := localVersion - checkpoint
	isDelta := checkpoint > 0 && gap <= fullSyncThreshold

	var items []CatalogItem
	if isDelta {
		items, err = m.fetchLocalItemsSince(ctx, checkpoint)
	} else {
		items, err = m.fetchLocalItems(ctx)
	}
	if err != nil {
		return fmt.Errorf("peers.PushCatalog: fetch items: %w", err)
	}

	// Nothing new to push.
	if isDelta && len(items) == 0 {
		return nil
	}

	push := CatalogPush{
		SenderVersion: localVersion,
		IsDelta:       isDelta,
		Items:         items,
	}
	body, err := json.Marshal(push)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, peer.Endpoint+"/api/peer/catalog", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := m.SignRequest(req); err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("peers.PushCatalog: push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("peers.PushCatalog: status %d: %s", resp.StatusCode, b)
	}

	if err := m.updateCheckpoint(ctx, peer.ID, localVersion); err != nil {
		slog.Error("update catalog checkpoint", "peer", peer.ID, "err", err)
	}
	return nil
}

func (m *Manager) updateCheckpoint(ctx context.Context, peerID string, version int64) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO catalog_checkpoints (peer_id, version, synced_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(peer_id) DO UPDATE SET version = excluded.version, synced_at = excluded.synced_at`,
		peerID, version, time.Now().UTC())
	return err
}

// ReceiveCatalog upserts items from a peer into the local DB.
// push.IsDelta=true means only changed items are included; skip the removal sweep.
func (m *Manager) ReceiveCatalog(ctx context.Context, peerID string, push CatalogPush) error {
	now := time.Now().UTC()

	incomingIDs := make(map[string]bool, len(push.Items))
	for _, item := range push.Items {
		incomingIDs[item.ID] = true
	}

	for _, item := range push.Items {
		var exists bool
		_ = m.db.QueryRowContext(ctx, `SELECT 1 FROM library_items WHERE id = ?`, item.ID).Scan(&exists)
		if exists {
			_, err := m.db.ExecContext(ctx,
				`UPDATE library_items SET last_seen = ?, file_size = ?, catalog_version = ? WHERE id = ?`,
				now, item.FileSize, item.CatalogVersion, item.ID)
			if err != nil {
				slog.Error("update peer item", "err", err)
			}
			continue
		}

		_, err := m.db.ExecContext(ctx,
			`INSERT INTO library_items (id, peer_id, media_type, title, year, series, season_num, episode_num, relative_path, file_size, poster_url, description, last_seen, catalog_version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, peerID, item.MediaType, item.Title, item.Year, item.Series,
			item.SeasonNum, item.EpisodeNum, item.RelativePath, item.FileSize,
			item.PosterURL, item.Description, now, item.CatalogVersion,
		)
		if err != nil {
			slog.Error("insert peer item", "err", err)
		}
	}

	// For full syncs, mark items from this peer that weren't in the push as removed.
	if !push.IsDelta {
		rows, err := m.db.QueryContext(ctx, `SELECT id FROM library_items WHERE peer_id = ?`, peerID)
		if err != nil {
			return err
		}
		var toRemove []string
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			if !incomingIDs[id] {
				toRemove = append(toRemove, id)
			}
		}
		rows.Close()

		for _, id := range toRemove {
			_, _ = m.db.ExecContext(ctx,
				`UPDATE library_items SET last_seen = ? WHERE id = ?`,
				time.Time{}, id)
		}
	}

	// Record the sender's version as our checkpoint for this peer.
	if push.SenderVersion > 0 {
		if err := m.updateCheckpoint(ctx, peerID, push.SenderVersion); err != nil {
			slog.Error("update catalog checkpoint on receive", "peer", peerID, "err", err)
		}
	}

	return nil
}

// SyncAll pushes catalog to all active peers.
func (m *Manager) SyncAll(ctx context.Context) error {
	peers, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p.Status != "active" {
			continue
		}
		if err := m.PushCatalog(ctx, p); err != nil {
			slog.Error("push catalog to peer failed", "peer", p.ID, "err", err)
		}
	}
	return nil
}

func (m *Manager) fetchLocalItems(ctx context.Context) ([]CatalogItem, error) {
	return m.fetchLocalItemsSince(ctx, -1)
}

func (m *Manager) fetchLocalItemsSince(ctx context.Context, sinceVersion int64) ([]CatalogItem, error) {
	var rows *sql.Rows
	var err error
	if sinceVersion < 0 {
		rows, err = m.db.QueryContext(ctx,
			`SELECT id, media_type, title, year, series, season_num, episode_num, relative_path, file_size, poster_url, description, catalog_version
			 FROM library_items WHERE peer_id IS NULL`)
	} else {
		rows, err = m.db.QueryContext(ctx,
			`SELECT id, media_type, title, year, series, season_num, episode_num, relative_path, file_size, poster_url, description, catalog_version
			 FROM library_items WHERE peer_id IS NULL AND catalog_version > ?`, sinceVersion)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		if err := rows.Scan(&item.ID, &item.MediaType, &item.Title, &item.Year, &item.Series,
			&item.SeasonNum, &item.EpisodeNum, &item.RelativePath, &item.FileSize,
			&item.PosterURL, &item.Description, &item.CatalogVersion); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// StreamFile serves a local file to a peer.
func StreamFile(db *sql.DB, mediaRoots []string, itemID string, w http.ResponseWriter, r *http.Request) {
	var relPath, mediaType string
	err := db.QueryRowContext(r.Context(),
		`SELECT relative_path, media_type FROM library_items WHERE id = ? AND peer_id IS NULL`,
		itemID,
	).Scan(&relPath, &mediaType)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Find the file in the appropriate media root.
	var fullPath string
	for _, root := range mediaRoots {
		candidate := root + "/" + relPath
		fullPath = candidate
		break
	}

	http.ServeFile(w, r, fullPath)
}
