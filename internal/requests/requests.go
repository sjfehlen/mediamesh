package requests

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sjfehlen/mediamesh/internal/audit"
)

// Request maps a requests table row.
type Request struct {
	ID          string
	UserID      string
	ItemID      string
	Status      string
	Note        *string
	ReviewedBy  *string
	ReviewNote  *string
	RequestedAt time.Time
	ReviewedAt  *time.Time
}

// EventDispatcher fires webhook events. Matches webhooks.EventDispatcher.
type EventDispatcher interface {
	Fire(ctx context.Context, event string, data any)
}

// Store manages media requests.
type Store struct {
	db         *sql.DB
	audit      *audit.Log
	dispatcher EventDispatcher
}

// NewStore creates a new request Store.
func NewStore(db *sql.DB, a *audit.Log, d EventDispatcher) *Store {
	return &Store{db: db, audit: a, dispatcher: d}
}

// Submit creates a new request.
func (s *Store) Submit(ctx context.Context, userID, itemID, note string) (*Request, error) {
	// Check user can_request.
	var canRequest bool
	var quotaGB *int64
	err := s.db.QueryRowContext(ctx,
		`SELECT can_request, quota_gb FROM users WHERE id = ?`, userID,
	).Scan(&canRequest, &quotaGB)
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if !canRequest {
		return nil, fmt.Errorf("user not allowed to request")
	}

	// Quota check: sum bytes_total of completed transfers in last 30 days.
	if quotaGB != nil {
		var used int64
		_ = s.db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(t.bytes_total), 0)
			 FROM transfers t
			 JOIN requests r ON t.request_id = r.id
			 WHERE r.user_id = ? AND t.status = 'complete' AND t.completed_at > ?`,
			userID, time.Now().UTC().Add(-30*24*time.Hour),
		).Scan(&used)
		limitBytes := *quotaGB * 1024 * 1024 * 1024
		if used >= limitBytes {
			return nil, fmt.Errorf("quota exceeded")
		}
	}

	// Duplicate request check.
	var dupCount int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM requests WHERE user_id = ? AND item_id = ? AND status IN ('pending', 'approved')`,
		userID, itemID,
	).Scan(&dupCount)
	if dupCount > 0 {
		return nil, fmt.Errorf("duplicate request already in queue")
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	var notePtr interface{}
	if note != "" {
		notePtr = note
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO requests (id, user_id, item_id, status, note, requested_at)
		 VALUES (?, ?, ?, 'pending', ?, ?)`,
		id, userID, itemID, notePtr, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert request: %w", err)
	}

	_ = s.audit.Write(ctx, audit.Entry{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "request.submit",
		TargetType: "request",
		TargetID:   id,
		Detail:     itemID,
	})

	req, err := s.get(ctx, id)
	if err == nil && s.dispatcher != nil {
		s.dispatcher.Fire(ctx, "request.submitted", req)
	}
	return req, err
}

// Approve approves a pending request.
func (s *Store) Approve(ctx context.Context, requestID, reviewerID, note string) (*Request, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE requests SET status = 'approved', reviewed_by = ?, review_note = ?, reviewed_at = ? WHERE id = ?`,
		reviewerID, nullIfEmpty(note), now, requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("approve request: %w", err)
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorID:    reviewerID,
		ActorType:  "user",
		Action:     "request.approve",
		TargetType: "request",
		TargetID:   requestID,
	})
	req, err := s.get(ctx, requestID)
	if err == nil && s.dispatcher != nil {
		s.dispatcher.Fire(ctx, "request.approved", req)
	}
	return req, err
}

// Reject rejects a request.
func (s *Store) Reject(ctx context.Context, requestID, reviewerID, note string) (*Request, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE requests SET status = 'rejected', reviewed_by = ?, review_note = ?, reviewed_at = ? WHERE id = ?`,
		reviewerID, nullIfEmpty(note), now, requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("reject request: %w", err)
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorID:    reviewerID,
		ActorType:  "user",
		Action:     "request.reject",
		TargetType: "request",
		TargetID:   requestID,
	})
	req, err := s.get(ctx, requestID)
	if err == nil && s.dispatcher != nil {
		s.dispatcher.Fire(ctx, "request.rejected", req)
	}
	return req, err
}

// List returns requests. Admin sees all; user sees their own.
func (s *Store) List(ctx context.Context, userID string, adminView bool) ([]*Request, error) {
	query := `SELECT id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at
	          FROM requests`
	args := []interface{}{}
	if !adminView {
		query += " WHERE user_id = ?"
		args = append(args, userID)
	}
	query += " ORDER BY requested_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query requests: %w", err)
	}
	defer rows.Close()

	var reqs []*Request
	for rows.Next() {
		r := &Request{}
		if err := rows.Scan(&r.ID, &r.UserID, &r.ItemID, &r.Status, &r.Note,
			&r.ReviewedBy, &r.ReviewNote, &r.RequestedAt, &r.ReviewedAt); err != nil {
			return nil, fmt.Errorf("scan request: %w", err)
		}
		reqs = append(reqs, r)
	}
	return reqs, rows.Err()
}

func (s *Store) get(ctx context.Context, id string) (*Request, error) {
	r := &Request{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, item_id, status, note, reviewed_by, review_note, requested_at, reviewed_at
		 FROM requests WHERE id = ?`, id,
	).Scan(&r.ID, &r.UserID, &r.ItemID, &r.Status, &r.Note,
		&r.ReviewedBy, &r.ReviewNote, &r.RequestedAt, &r.ReviewedAt)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	return r, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
