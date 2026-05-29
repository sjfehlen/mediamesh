package db_test

import (
	"testing"
	"testing/fstest"

	"github.com/sjfehlen/mediamesh/internal/db"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()

	// Build a minimal in-memory FS with a no-op migration so the test
	// has no dependency on the real migrations directory.
	migrationsFS := fstest.MapFS{
		"1_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS _test (id INTEGER PRIMARY KEY);")},
		"1_init.down.sql": {Data: []byte("DROP TABLE IF EXISTS _test;")},
	}

	database, dbx, err := db.Open(dir, migrationsFS)
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
