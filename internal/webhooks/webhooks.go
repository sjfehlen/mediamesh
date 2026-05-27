package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Event types — subscribe to any combination in a webhook's events list.
const (
	EventRequestSubmitted  = "request.submitted"
	EventRequestApproved   = "request.approved"
	EventRequestRejected   = "request.rejected"
	EventTransferStarted   = "transfer.started"
	EventTransferComplete  = "transfer.complete"
	EventTransferFailed    = "transfer.failed"
	EventNewMediaAvailable = "media.available" // fired when a peer has media you don't
	EventPeerConnected     = "peer.connected"
	EventPeerRevoked       = "peer.revoked"
)

// Payload is the envelope sent to every webhook URL.
type Payload struct {
	ID        string          `json:"id"`        // delivery UUID
	Event     string          `json:"event"`
	NodeName  string          `json:"node_name"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Webhook is a registered webhook subscription.
type Webhook struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// Secret is never returned after creation.
}

// Dispatcher fires webhook deliveries for events.
type Dispatcher struct {
	db       *sql.DB
	nodeName string
	client   *http.Client
}

// New creates a Dispatcher.
func New(db *sql.DB, nodeName string) *Dispatcher {
	return &Dispatcher{
		db:       db,
		nodeName: nodeName,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Fire dispatches an event to all matching enabled webhooks.
// It is non-blocking — deliveries are attempted in background goroutines.
func (d *Dispatcher) Fire(ctx context.Context, event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		slog.Error("webhooks.Fire: marshal data", "event", event, "err", err)
		return
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, url, secret FROM webhooks WHERE enabled = 1 AND json_each.value = ?
         -- use json_each to match event in the events JSON array
         AND EXISTS (SELECT 1 FROM json_each(events) WHERE value = ?)`,
		event, event,
	)
	if err != nil {
		slog.Error("webhooks.Fire: query", "err", err)
		return
	}
	defer rows.Close()

	type target struct{ id, url, secret string }
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.url, &t.secret); err == nil {
			targets = append(targets, t)
		}
	}

	payload := Payload{
		ID:        uuid.New().String(),
		Event:     event,
		NodeName:  d.nodeName,
		Timestamp: time.Now().UTC(),
		Data:      raw,
	}

	for _, t := range targets {
		go d.deliver(t.id, t.url, t.secret, payload)
	}
}

func (d *Dispatcher) deliver(webhookID, url, secret string, payload Payload) {
	body, _ := json.Marshal(payload)
	sig := sign(secret, body)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		d.recordDelivery(webhookID, payload.Event, string(body), 0, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MediaMesh-Event", payload.Event)
	req.Header.Set("X-MediaMesh-Signature", "sha256="+sig)
	req.Header.Set("X-MediaMesh-Delivery", payload.ID)

	resp, err := d.client.Do(req)
	if err != nil {
		d.recordDelivery(webhookID, payload.Event, string(body), 0, err.Error())
		return
	}
	defer resp.Body.Close()
	d.recordDelivery(webhookID, payload.Event, string(body), resp.StatusCode, "")
}

func (d *Dispatcher) recordDelivery(webhookID, event, payload string, statusCode int, errMsg string) {
	var statusVal any = statusCode
	if statusCode == 0 {
		statusVal = nil
	}
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := d.db.Exec(
		`INSERT INTO webhook_deliveries (id, webhook_id, event, payload, status_code, error)
         VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), webhookID, event, payload, statusVal, errVal,
	)
	if err != nil {
		slog.Error("webhooks: record delivery", "err", err)
	}
}

// sign returns HMAC-SHA256 hex of body using secret.
// The receiver can verify with the same computation.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- Management ---

// Create registers a new webhook and returns the raw secret (shown once).
func Create(ctx context.Context, db *sql.DB, name, url, createdBy string, events []string) (wh *Webhook, rawSecret string, err error) {
	id := uuid.New().String()
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return nil, "", fmt.Errorf("webhooks.Create: generate secret: %w", err)
	}
	rawSecret = hex.EncodeToString(secretBytes)
	eventsJSON, _ := json.Marshal(events)

	_, err = db.ExecContext(ctx,
		`INSERT INTO webhooks (id, name, url, secret, events, enabled, created_by)
         VALUES (?, ?, ?, ?, ?, 1, ?)`,
		id, name, url, rawSecret, string(eventsJSON), createdBy,
	)
	if err != nil {
		return nil, "", fmt.Errorf("webhooks.Create: %w", err)
	}
	return &Webhook{
		ID:        id,
		Name:      name,
		URL:       url,
		Events:    events,
		Enabled:   true,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}, rawSecret, nil
}

// List returns all webhooks (secret omitted).
func List(ctx context.Context, db *sql.DB) ([]*Webhook, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, url, events, enabled, created_by, created_at FROM webhooks ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("webhooks.List: %w", err)
	}
	defer rows.Close()

	var out []*Webhook
	for rows.Next() {
		var wh Webhook
		var eventsJSON string
		var enabled int
		if err := rows.Scan(&wh.ID, &wh.Name, &wh.URL, &eventsJSON, &enabled, &wh.CreatedBy, &wh.CreatedAt); err != nil {
			return nil, fmt.Errorf("webhooks.List: scan: %w", err)
		}
		_ = json.Unmarshal([]byte(eventsJSON), &wh.Events)
		wh.Enabled = enabled == 1
		out = append(out, &wh)
	}
	return out, nil
}

// SetEnabled enables or disables a webhook.
func SetEnabled(ctx context.Context, db *sql.DB, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := db.ExecContext(ctx, `UPDATE webhooks SET enabled = ? WHERE id = ?`, v, id)
	return err
}

// Delete removes a webhook and its delivery history.
func Delete(ctx context.Context, db *sql.DB, id string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE webhook_id = ?`, id); err != nil {
		return fmt.Errorf("webhooks.Delete deliveries: %w", err)
	}
	_, err := db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// Deliveries returns recent delivery attempts for a webhook.
func Deliveries(ctx context.Context, db *sql.DB, webhookID string, limit int) ([]map[string]any, error) {
	if limit == 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, event, status_code, error, attempted_at
         FROM webhook_deliveries WHERE webhook_id = ?
         ORDER BY attempted_at DESC LIMIT ?`,
		webhookID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("webhooks.Deliveries: %w", err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var id, event string
		var statusCode sql.NullInt64
		var errMsg sql.NullString
		var attemptedAt time.Time
		if err := rows.Scan(&id, &event, &statusCode, &errMsg, &attemptedAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id":           id,
			"event":        event,
			"status_code":  statusCode.Int64,
			"error":        errMsg.String,
			"attempted_at": attemptedAt,
		})
	}
	return out, nil
}
