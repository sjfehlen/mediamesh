package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
)

// Open opens (or creates) the SQLite database at dataDir/mediamesh.db,
// enables WAL mode and foreign keys, and runs all pending migrations from
// the provided FS (expected to contain *.sql files at the root).
// Returns both a *sql.DB (for sqlc-generated code) and a *sqlx.DB (for
// ad-hoc queries that benefit from sqlx convenience methods).
func init() {
	// Register a custom driver that enforces WAL mode and foreign keys on every
	// connection in the pool. The DSN pragmas only apply to the first connection;
	// subsequent pool connections need this hook.
	sql.Register("sqlite3_mediamesh", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			_, err := conn.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;", nil)
			return err
		},
	})
}

func Open(dataDir string, migrationsFS fs.FS) (*sql.DB, *sqlx.DB, error) {
	dsn := fmt.Sprintf("file:%s/mediamesh.db", dataDir)
	db, err := sql.Open("sqlite3_mediamesh", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("db.Open: open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, nil, fmt.Errorf("db.Open: ping sqlite: %w", err)
	}

	if err := runMigrations(db, migrationsFS); err != nil {
		return nil, nil, fmt.Errorf("db.Open: run migrations: %w", err)
	}

	slog.Info("database opened", "path", dataDir+"/mediamesh.db")

	// Wrap in sqlx for ad-hoc query convenience — shares the same underlying connection.
	dbx := sqlx.NewDb(db, "sqlite3")
	return db, dbx, nil
}

func runMigrations(db *sql.DB, migrationsFS fs.FS) error {
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return fmt.Errorf("db.runMigrations: create sqlite driver: %w", err)
	}

	src, err := iofs.New(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("db.runMigrations: create iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("db.runMigrations: create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("db.runMigrations: apply migrations: %w", err)
	}

	return nil
}
