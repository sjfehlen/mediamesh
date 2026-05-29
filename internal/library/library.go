package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// MediaType values for library classification.
const (
	TypeMovie     = "movie"
	TypeTV        = "tv"
	TypeAudiobook = "audiobook"
	TypeEbook     = "ebook"
)

// Library is a configured media folder.
type Library struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	MediaType string    `json:"media_type"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultLibraries are seeded on first boot if the standard mount paths exist.
var DefaultLibraries = []Library{
	{Name: "Movies", Path: "/media/movies", MediaType: TypeMovie},
	{Name: "TV Shows", Path: "/media/tv", MediaType: TypeTV},
	{Name: "Audiobooks", Path: "/media/audiobooks", MediaType: TypeAudiobook},
	{Name: "Kids Audiobooks", Path: "/media/kids-audiobooks", MediaType: TypeAudiobook},
	{Name: "Ebooks", Path: "/media/ebooks", MediaType: TypeEbook},
	{Name: "Kids Ebooks", Path: "/media/kids-ebooks", MediaType: TypeEbook},
}

// SeedDefaults inserts default libraries for any standard mount paths that
// exist on disk and aren't already configured. Safe to call on every boot.
func SeedDefaults(ctx context.Context, db *sql.DB) error {
	for _, def := range DefaultLibraries {
		if _, err := os.Stat(def.Path); os.IsNotExist(err) {
			continue
		}
		var count int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM libraries WHERE path = ?`, def.Path).Scan(&count)
		if count > 0 {
			continue
		}
		if _, err := Create(ctx, db, def.Name, def.Path, def.MediaType); err != nil {
			return fmt.Errorf("library.SeedDefaults: %w", err)
		}
	}
	return nil
}

// Create adds a new library.
func Create(ctx context.Context, db *sql.DB, name, path, mediaType string) (*Library, error) {
	id := uuid.New().String()
	_, err := db.ExecContext(ctx,
		`INSERT INTO libraries (id, name, path, media_type) VALUES (?, ?, ?, ?)`,
		id, name, path, mediaType,
	)
	if err != nil {
		return nil, fmt.Errorf("library.Create: %w", err)
	}
	return Get(ctx, db, id)
}

// List returns all libraries.
func List(ctx context.Context, db *sql.DB) ([]*Library, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, path, media_type, enabled, created_at FROM libraries ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("library.List: %w", err)
	}
	defer rows.Close()

	var out []*Library
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// ListEnabled returns only enabled libraries.
func ListEnabled(ctx context.Context, db *sql.DB) ([]*Library, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, path, media_type, enabled, created_at FROM libraries WHERE enabled = 1 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("library.ListEnabled: %w", err)
	}
	defer rows.Close()

	var out []*Library
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// Get returns a single library by ID.
func Get(ctx context.Context, db *sql.DB, id string) (*Library, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, name, path, media_type, enabled, created_at FROM libraries WHERE id = ?`, id,
	)
	return scanLibrary(row)
}

// Update changes a library's name, path, and media type.
func Update(ctx context.Context, db *sql.DB, id, name, path, mediaType string) (*Library, error) {
	_, err := db.ExecContext(ctx,
		`UPDATE libraries SET name = ?, path = ?, media_type = ? WHERE id = ?`,
		name, path, mediaType, id,
	)
	if err != nil {
		return nil, fmt.Errorf("library.Update: %w", err)
	}
	return Get(ctx, db, id)
}

// SetEnabled enables or disables a library.
func SetEnabled(ctx context.Context, db *sql.DB, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := db.ExecContext(ctx, `UPDATE libraries SET enabled = ? WHERE id = ?`, v, id)
	return err
}

// Delete removes a library and all its scanned items.
func Delete(ctx context.Context, db *sql.DB, id string) error {
	lib, err := Get(ctx, db, id)
	if err != nil {
		return err
	}
	// Remove catalog items that came from this library path.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM library_items WHERE peer_id IS NULL AND relative_path LIKE ?`,
		lib.Path+"%",
	); err != nil {
		return fmt.Errorf("library.Delete items: %w", err)
	}
	_, err = db.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(...any) error
}

func scanLibrary(row scanner) (*Library, error) {
	var l Library
	var enabled int
	err := row.Scan(&l.ID, &l.Name, &l.Path, &l.MediaType, &enabled, &l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("library.scan: %w", err)
	}
	l.Enabled = enabled == 1
	return &l, nil
}
