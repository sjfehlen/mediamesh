package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sjfehlen/mediamesh/internal/audit"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE audit_log (
		id TEXT PRIMARY KEY,
		actor_id TEXT,
		actor_type TEXT NOT NULL,
		action TEXT NOT NULL,
		target_type TEXT,
		target_id TEXT,
		detail TEXT,
		error_code TEXT,
		occurred_at DATETIME NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestWriteAndList(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	l := audit.New(db)
	ctx := context.Background()

	err := l.Write(ctx, audit.Entry{
		ActorID:    "user-1",
		ActorType:  "user",
		Action:     "login",
		TargetType: "session",
		TargetID:   "sess-1",
		Detail:     "ok",
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries, err := l.List(ctx, "", "", false, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
	if entries[0].Action != "login" {
		t.Errorf("action = %q", entries[0].Action)
	}
}
