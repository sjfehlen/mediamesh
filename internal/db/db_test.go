package db_test

import (
	"os"
	"testing"

	"github.com/sjfehlen/mediamesh/internal/db"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	// Migrations are resolved relative to the working directory;
	// this test is run from the package directory so we need to
	// point at the repo-root migrations folder.
	// Skip if not running from repo root (CI sets working dir correctly).
	if _, err := os.Stat("../../migrations"); os.IsNotExist(err) {
		t.Skip("migrations folder not found; skipping integration test")
	}

	database, dbx, err := db.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if err := database.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if dbx == nil {
		t.Fatal("expected non-nil sqlx.DB")
	}
}
