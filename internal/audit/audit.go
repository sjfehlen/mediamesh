package audit

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Entry describes a single audit event.
type Entry struct {
	ActorID    string
	ActorType  string
	Action     string
	TargetType string
	TargetID   string
	Detail     string
}

// Log is an append-only audit writer backed by SQLite.
type Log struct {
	db *sql.DB
}

// New creates a new audit Log backed by the given database.
func New(db *sql.DB) *Log {
	return &Log{db: db}
}

// Write inserts a new audit log entry. It never returns an error that
// should crash the caller — failures are logged and swallowed.
func (l *Log) Write(ctx context.Context, e Entry) error {
	id := uuid.New().String()
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, actor_id, actor_type, action, target_type, target_id, detail, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullIfEmpty(e.ActorID), e.ActorType, e.Action,
		nullIfEmpty(e.TargetType), nullIfEmpty(e.TargetID), nullIfEmpty(e.Detail),
		time.Now().UTC(),
	)
	if err != nil {
		slog.Error("audit write failed", "err", err, "action", e.Action)
		// swallow — never crash caller
	}
	return nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// AuditEntry is the full row returned from the audit log.
type AuditEntry struct {
	ID         string    `json:"id"`
	ActorID    string    `json:"actor_id"`
	ActorType  string    `json:"actor_type"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurred_at"`
}

// List returns audit entries with optional filters.
func (l *Log) List(ctx context.Context, actorID, action string, from, to time.Time, limit int) ([]*AuditEntry, error) {
	query := `SELECT id, COALESCE(actor_id,''), actor_type, action,
		COALESCE(target_type,''), COALESCE(target_id,''), COALESCE(detail,''), occurred_at
		FROM audit_log WHERE 1=1`
	args := []interface{}{}

	if actorID != "" {
		query += " AND actor_id = ?"
		args = append(args, actorID)
	}
	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}
	if !from.IsZero() {
		query += " AND occurred_at >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND occurred_at <= ?"
		args = append(args, to)
	}
	query += " ORDER BY occurred_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit_log: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorType, &e.Action,
			&e.TargetType, &e.TargetID, &e.Detail, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
