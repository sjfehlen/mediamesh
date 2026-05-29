package transfers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/catalog"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/peers"
	"github.com/sjfehlen/mediamesh/internal/requests"
)

// ProgressBroadcaster is implemented by the WebSocket hub in the api package.
// Using an interface here keeps the transfers package free of api imports.
type ProgressBroadcaster interface {
	Broadcast(evt interface{})
}

// ProgressEvent carries real-time progress data for a single transfer.
type ProgressEvent struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal *int64 `json:"bytes_total,omitempty"`
	Status     string `json:"status,omitempty"`
}

// Transfer maps a transfers table row.
type Transfer struct {
	ID          string     `json:"id"`
	RequestID   string     `json:"request_id"`
	PeerID      string     `json:"peer_id"`
	ItemID      string     `json:"item_id"`
	Status      string     `json:"status"`
	BytesTotal  *int64     `json:"bytes_total,omitempty"`
	BytesDone   int64      `json:"bytes_done"`
	Error       *string    `json:"error,omitempty"`
	QueuedAt    time.Time  `json:"queued_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// EventDispatcher fires webhook events. Matches webhooks.EventDispatcher.
type EventDispatcher interface {
	Fire(ctx context.Context, event string, data any)
}

// Engine manages the transfer lifecycle.
type Engine struct {
	db         *sql.DB
	peers      *peers.Manager
	cfg        *config.Config
	audit      *audit.Log
	dispatcher EventDispatcher

	mu      sync.Mutex
	active  int
	maxConc int

	hub ProgressBroadcaster
}

// NewEngine creates a new transfer Engine.
func NewEngine(db *sql.DB, p *peers.Manager, cfg *config.Config, a *audit.Log, d EventDispatcher) *Engine {
	return &Engine{
		db:         db,
		peers:      p,
		cfg:        cfg,
		audit:      a,
		dispatcher: d,
		maxConc:    2,
	}
}

// SetHub wires a ProgressBroadcaster (the WebSocket hub) into the engine.
// Call this before Start.
func (e *Engine) SetHub(h ProgressBroadcaster) {
	e.hub = h
}

// broadcast sends a ProgressEvent if a hub is registered.
func (e *Engine) broadcast(evt ProgressEvent) {
	if e.hub != nil {
		e.hub.Broadcast(evt)
	}
}

// Enqueue creates a new queued transfer.
func (e *Engine) Enqueue(ctx context.Context, requestID, peerID, itemID string) (*Transfer, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := e.db.ExecContext(ctx,
		`INSERT INTO transfers (id, request_id, peer_id, item_id, status, queued_at)
		 VALUES (?, ?, ?, ?, 'queued', ?)`,
		id, requestID, peerID, itemID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("transfers.Engine.Enqueue: %w", err)
	}
	_ = e.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "transfer.queued",
		TargetType: "transfer",
		TargetID:   id,
	})
	return e.get(ctx, id)
}

// Start runs the background transfer worker.
func (e *Engine) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.processQueue(ctx)
			}
		}
	}()
}

func (e *Engine) processQueue(ctx context.Context) {
	e.mu.Lock()
	slots := e.maxConc - e.active
	e.mu.Unlock()

	if slots <= 0 {
		return
	}

	rows, err := e.db.QueryContext(ctx,
		`SELECT id FROM transfers WHERE status = 'queued' ORDER BY queued_at LIMIT ?`, slots)
	if err != nil {
		slog.Error("query queued transfers", "err", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		e.mu.Lock()
		e.active++
		e.mu.Unlock()

		go func(tid string) {
			defer func() {
				e.mu.Lock()
				e.active--
				e.mu.Unlock()
			}()
			t, err := e.get(ctx, tid)
			if err != nil {
				slog.Error("get transfer", "id", tid, "err", err)
				return
			}
			if err := e.executeTransfer(ctx, t); err != nil {
				slog.Error("transfer failed", "id", tid, "err", err)
				e.markFailed(ctx, tid, err.Error())
			}
		}(id)
	}
}

func (e *Engine) executeTransfer(ctx context.Context, t *Transfer) error {
	// Mark started.
	now := time.Now().UTC()
	_, _ = e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'active', started_at = ? WHERE id = ?`, now, t.ID)
	_ = e.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "transfer.started",
		TargetType: "transfer",
		TargetID:   t.ID,
	})
	e.broadcast(ProgressEvent{
		Type:       "transfer.status",
		TransferID: t.ID,
		Status:     "active",
	})

	// Fetch the item.
	item, err := catalog.GetByID(ctx, e.db, t.ItemID)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: get item: %w", err)
	}

	// Duplicate check.
	isDup, reason, err := requests.CheckDuplicate(ctx, e.db, item)
	if err != nil {
		slog.Warn("duplicate check error", "err", err)
	}
	if isDup {
		return fmt.Errorf("duplicate_detected: %s", reason)
	}

	// Get peer.
	peerList, err := e.peers.List(ctx)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: list peers: %w", err)
	}
	var sourcePeer *peers.Peer
	for _, p := range peerList {
		if p.ID == t.PeerID {
			sourcePeer = p
			break
		}
	}
	if sourcePeer == nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: peer not found: %s", t.PeerID)
	}

	// Determine destination path.
	destPath, err := e.destinationPath(item)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: destination path: %w", err)
	}

	// Path traversal check.
	if err := e.validateDestPath(destPath); err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: path validation: %w", err)
	}

	// Fetch from peer.
	fileURL := sourcePeer.Endpoint + "/api/peer/files/" + t.ItemID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: build request: %w", err)
	}

	// Resumability: check if .tmp exists.
	tmpPath := destPath + ".tmp"
	var offset int64
	if fi, err := os.Stat(tmpPath); err == nil {
		offset = fi.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	if err := e.peers.SignRequest(req); err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: sign request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: fetch file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("transfers.Engine.executeTransfer: peer returned %d", resp.StatusCode)
	}

	// Store bytes_total.
	var bytesTotal *int64
	if cl := resp.ContentLength; cl > 0 {
		total := cl + offset
		bytesTotal = &total
		_, _ = e.db.ExecContext(ctx,
			`UPDATE transfers SET bytes_total = ? WHERE id = ?`, total, t.ID)
	}

	// Ensure destination directory exists.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: mkdir: %w", err)
	}

	// Open tmp file.
	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: open tmp file: %w", err)
	}

	// Stream with progress updates.
	_, err = e.streamWithProgress(ctx, f, resp.Body, t.ID, offset, bytesTotal)
	f.Close()
	if err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: stream: %w", err)
	}

	// Rename on completion.
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("transfers.Engine.executeTransfer: rename: %w", err)
	}

	// Mark complete.
	completedAt := time.Now().UTC()
	_, _ = e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'complete', completed_at = ? WHERE id = ?`,
		completedAt, t.ID)
	_ = e.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "transfer.complete",
		TargetType: "transfer",
		TargetID:   t.ID,
	})
	e.broadcast(ProgressEvent{
		Type:       "transfer.status",
		TransferID: t.ID,
		Status:     "complete",
	})
	if e.dispatcher != nil {
		e.dispatcher.Fire(ctx, "transfer.complete", map[string]any{
			"transfer_id": t.ID,
			"item_id":     t.ItemID,
			"peer_id":     t.PeerID,
		})
	}

	// Rescan.
	_ = e.RescanLocal(ctx, string(item.MediaType))
	return nil
}

func (e *Engine) streamWithProgress(ctx context.Context, dst io.Writer, src io.Reader, transferID string, initial int64, bytesTotal *int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)

			select {
			case <-ticker.C:
				done := initial + total
				_, _ = e.db.ExecContext(ctx,
					`UPDATE transfers SET bytes_done = ? WHERE id = ?`, done, transferID)
				e.broadcast(ProgressEvent{
					Type:       "transfer.progress",
					TransferID: transferID,
					BytesDone:  done,
					BytesTotal: bytesTotal,
				})
			default:
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (e *Engine) destinationPath(item *catalog.Item) (string, error) {
	var base string
	switch item.MediaType {
	case catalog.Movie:
		base = "/media/movies"
	case catalog.TVShow, catalog.TVSeason, catalog.TVEpisode:
		base = "/media/tv"
	case catalog.Audiobook:
		base = "/media/audiobooks"
	case catalog.Ebook:
		base = "/media/ebooks"
	default:
		base = "/media/other"
	}
	return filepath.Join(base, item.RelativePath), nil
}

func (e *Engine) validateDestPath(destPath string) error {
	roots := []string{"/media/movies", "/media/tv", "/media/audiobooks", "/media/kids-audiobooks", "/media/ebooks", "/media/kids-ebooks", "/media/other"}
	clean := filepath.Clean(destPath)
	for _, root := range roots {
		if strings.HasPrefix(clean, root+"/") || clean == root {
			return nil
		}
	}
	return fmt.Errorf("transfers.validateDestPath: destination path %q is outside media roots", destPath)
}

// RescanLocal triggers a library refresh in Jellyfin or ABS.
func (e *Engine) RescanLocal(ctx context.Context, mediaType string) error {
	switch mediaType {
	case string(catalog.Movie), string(catalog.TVShow), string(catalog.TVSeason), string(catalog.TVEpisode):
		if e.cfg.JellyfinURL == "" {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			e.cfg.JellyfinURL+"/Library/Refresh", nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Emby-Token", e.cfg.JellyfinAPIKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	case string(catalog.Audiobook), string(catalog.Ebook):
		if e.cfg.AbsURL == "" {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			e.cfg.AbsURL+"/api/libraries/main/scan", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+e.cfg.AbsAPIKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	return nil
}

func (e *Engine) markFailed(ctx context.Context, id, errMsg string) {
	_, _ = e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'failed', error = ? WHERE id = ?`, errMsg, id)
	_ = e.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "transfer.failed",
		TargetType: "transfer",
		TargetID:   id,
		Detail:     errMsg,
	})
	e.broadcast(ProgressEvent{
		Type:       "transfer.status",
		TransferID: id,
		Status:     "failed",
	})
	if e.dispatcher != nil {
		e.dispatcher.Fire(ctx, "transfer.failed", map[string]any{
			"transfer_id": id,
			"error":       errMsg,
		})
	}
}

// List returns all transfers.
func (e *Engine) List(ctx context.Context) ([]*Transfer, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, request_id, peer_id, item_id, status, bytes_total, bytes_done, error, queued_at, started_at, completed_at
		 FROM transfers ORDER BY queued_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("transfers.Engine.List: %w", err)
	}
	defer rows.Close()

	var list []*Transfer
	for rows.Next() {
		t := &Transfer{}
		if err := rows.Scan(&t.ID, &t.RequestID, &t.PeerID, &t.ItemID, &t.Status,
			&t.BytesTotal, &t.BytesDone, &t.Error,
			&t.QueuedAt, &t.StartedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// Pause pauses an active transfer.
func (e *Engine) Pause(ctx context.Context, id string) error {
	_, err := e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'paused' WHERE id = ? AND status = 'active'`, id)
	if err != nil {
		return fmt.Errorf("transfers.Engine.Pause: %w", err)
	}
	e.broadcast(ProgressEvent{Type: "transfer.status", TransferID: id, Status: "paused"})
	return nil
}

// Resume resumes a paused transfer (moves it back to queued).
func (e *Engine) Resume(ctx context.Context, id string) error {
	_, err := e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'queued' WHERE id = ? AND status = 'paused'`, id)
	if err != nil {
		return fmt.Errorf("transfers.Engine.Resume: %w", err)
	}
	e.broadcast(ProgressEvent{Type: "transfer.status", TransferID: id, Status: "queued"})
	return nil
}

// Retry resets a failed transfer back to queued.
func (e *Engine) Retry(ctx context.Context, id string) error {
	res, err := e.db.ExecContext(ctx,
		`UPDATE transfers SET status = 'queued', error = NULL WHERE id = ? AND status = 'failed'`, id)
	if err != nil {
		return fmt.Errorf("transfers.Engine.Retry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("transfers.Engine.Retry: transfer not found or not in failed state")
	}
	_ = e.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "transfer.retry",
		TargetType: "transfer",
		TargetID:   id,
	})
	e.broadcast(ProgressEvent{Type: "transfer.status", TransferID: id, Status: "queued"})
	return nil
}

// ListByRequestID returns all transfers for a given request.
func (e *Engine) ListByRequestID(ctx context.Context, requestID string) ([]*Transfer, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, request_id, peer_id, item_id, status, bytes_total, bytes_done, error, queued_at, started_at, completed_at
		 FROM transfers WHERE request_id = ? ORDER BY queued_at DESC`, requestID)
	if err != nil {
		return nil, fmt.Errorf("transfers.Engine.ListByRequestID: %w", err)
	}
	defer rows.Close()

	var list []*Transfer
	for rows.Next() {
		t := &Transfer{}
		if err := rows.Scan(&t.ID, &t.RequestID, &t.PeerID, &t.ItemID, &t.Status,
			&t.BytesTotal, &t.BytesDone, &t.Error,
			&t.QueuedAt, &t.StartedAt, &t.CompletedAt); err != nil {
			return nil, fmt.Errorf("transfers.Engine.ListByRequestID: scan: %w", err)
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (e *Engine) get(ctx context.Context, id string) (*Transfer, error) {
	t := &Transfer{}
	err := e.db.QueryRowContext(ctx,
		`SELECT id, request_id, peer_id, item_id, status, bytes_total, bytes_done, error, queued_at, started_at, completed_at
		 FROM transfers WHERE id = ?`, id,
	).Scan(&t.ID, &t.RequestID, &t.PeerID, &t.ItemID, &t.Status,
		&t.BytesTotal, &t.BytesDone, &t.Error,
		&t.QueuedAt, &t.StartedAt, &t.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("transfers.Engine.get: %w", err)
	}
	return t, nil
}
