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
)

// CatalogItem is a catalog entry sent between peers.
type CatalogItem struct {
	ID           string  `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	Year         *int    `json:"year,omitempty"`
	Series       *string `json:"series,omitempty"`
	SeasonNum    *int    `json:"season_num,omitempty"`
	EpisodeNum   *int    `json:"episode_num,omitempty"`
	RelativePath string  `json:"relative_path"`
	FileSize     *int64  `json:"file_size,omitempty"`
	PosterURL    *string `json:"poster_url,omitempty"`
	Description  *string `json:"description,omitempty"`
}

// PushCatalog sends all local items to a peer.
func (m *Manager) PushCatalog(ctx context.Context, peer *Peer) error {
	items, err := m.fetchLocalItems(ctx)
	if err != nil {
		return fmt.Errorf("fetch local items: %w", err)
	}

	body, err := json.Marshal(items)
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
		return fmt.Errorf("push catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push catalog status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ReceiveCatalog upserts items from a peer into the local DB.
func (m *Manager) ReceiveCatalog(ctx context.Context, peerID string, items []CatalogItem) error {
	now := time.Now().UTC()

	// Collect incoming IDs.
	incomingIDs := make(map[string]bool)
	for _, item := range items {
		incomingIDs[item.ID] = true
	}

	for _, item := range items {
		var exists bool
		_ = m.db.QueryRowContext(ctx, `SELECT 1 FROM library_items WHERE id = ?`, item.ID).Scan(&exists)
		if exists {
			_, err := m.db.ExecContext(ctx,
				`UPDATE library_items SET last_seen = ?, file_size = ? WHERE id = ?`,
				now, item.FileSize, item.ID)
			if err != nil {
				slog.Error("update peer item", "err", err)
			}
			continue
		}

		_, err := m.db.ExecContext(ctx,
			`INSERT INTO library_items (id, peer_id, media_type, title, year, series, season_num, episode_num, relative_path, file_size, poster_url, description, last_seen)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, peerID, item.MediaType, item.Title, item.Year, item.Series,
			item.SeasonNum, item.EpisodeNum, item.RelativePath, item.FileSize,
			item.PosterURL, item.Description, now,
		)
		if err != nil {
			slog.Error("insert peer item", "err", err)
		}
	}

	// Mark items from this peer that weren't in the push as removed (set last_seen old).
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
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, media_type, title, year, series, season_num, episode_num, relative_path, file_size, poster_url, description
		 FROM library_items WHERE peer_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		if err := rows.Scan(&item.ID, &item.MediaType, &item.Title, &item.Year, &item.Series,
			&item.SeasonNum, &item.EpisodeNum, &item.RelativePath, &item.FileSize,
			&item.PosterURL, &item.Description); err != nil {
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
