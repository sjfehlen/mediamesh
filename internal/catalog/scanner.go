package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/library"
)

// MediaType describes the kind of media an item represents.
type MediaType string

const (
	Movie      MediaType = "movie"
	TVShow     MediaType = "tvshow"
	TVSeason   MediaType = "tvseason"
	TVEpisode  MediaType = "tvepisode"
	Audiobook  MediaType = "audiobook"
	Ebook      MediaType = "ebook"
)

// Item maps a library_items row.
type Item struct {
	ID           string     `json:"id"`
	PeerID       *string    `json:"peer_id,omitempty"`
	MediaType    MediaType  `json:"media_type"`
	Title        string     `json:"title"`
	Year         *int       `json:"year,omitempty"`
	Series       *string    `json:"series,omitempty"`
	SeasonNum    *int       `json:"season_num,omitempty"`
	EpisodeNum   *int       `json:"episode_num,omitempty"`
	RelativePath string     `json:"relative_path"`
	FileSize     *int64     `json:"file_size,omitempty"`
	TmdbID       *int64     `json:"tmdb_id,omitempty"`
	OlKey        *string    `json:"ol_key,omitempty"`
	PosterURL    *string    `json:"poster_url,omitempty"`
	Description  *string    `json:"description,omitempty"`
	Rating       *float64   `json:"rating,omitempty"`
	Genres       *string    `json:"genres,omitempty"`
	LastSeen     time.Time  `json:"last_seen"`
	MetadataAt   *time.Time `json:"metadata_at,omitempty"`
}

// Scanner walks media roots and upserts library items.
type Scanner struct {
	db    *sql.DB
	cfg   *config.Config
	audit *audit.Log
}

// NewScanner creates a new Scanner.
func NewScanner(db *sql.DB, cfg *config.Config, a *audit.Log) *Scanner {
	return &Scanner{db: db, cfg: cfg, audit: a}
}

type mediaRoot struct {
	path      string
	mediaType MediaType
}

// mediaRoots reads enabled libraries from the DB.
func (s *Scanner) mediaRoots(ctx context.Context) []mediaRoot {
	libs, err := library.ListEnabled(ctx, s.db)
	if err != nil {
		slog.Error("scanner: list libraries", "err", err)
		return nil
	}
	roots := make([]mediaRoot, 0, len(libs))
	for _, lib := range libs {
		mt := mediaTypeFromString(lib.MediaType)
		roots = append(roots, mediaRoot{path: lib.Path, mediaType: mt})
	}
	return roots
}

func mediaTypeFromString(s string) MediaType {
	switch s {
	case "movie":
		return Movie
	case "tv":
		return TVShow
	case "audiobook":
		return Audiobook
	case "ebook":
		return Ebook
	default:
		return Movie
	}
}

// ScanAll walks all media roots and upserts items.
func (s *Scanner) ScanAll(ctx context.Context) error {
	slog.Info("catalog scan starting")
	roots := s.mediaRoots(ctx)
	var total int
	for _, root := range roots {
		n, err := s.scanRoot(ctx, root)
		if err != nil {
			slog.Error("scan root failed", "root", root.path, "err", err)
			continue
		}
		total += n
	}
	slog.Info("catalog scan complete", "items", total)
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType: "system",
		Action:    "catalog.scan",
		Detail:    fmt.Sprintf("found %d items", total),
	})
	return nil
}

func (s *Scanner) scanRoot(ctx context.Context, root mediaRoot) (int, error) {
	var count int
	err := filepath.WalkDir(root.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !isMediaFile(ext, root.mediaType) {
			return nil
		}

		rel, err := filepath.Rel(root.path, path)
		if err != nil {
			return nil
		}

		info, _ := d.Info()
		var fileSize *int64
		if info != nil {
			sz := info.Size()
			fileSize = &sz
		}

		mt := inferMediaType(root.mediaType, rel)
		title, year := parseTitleYear(filepath.Base(rel))

		id := itemID("local:" + rel)

		if err := s.upsertItem(ctx, id, mt, title, year, rel, fileSize); err != nil {
			slog.Error("upsert item failed", "path", rel, "err", err)
			return nil
		}
		count++
		return nil
	})
	return count, err
}

func (s *Scanner) upsertItem(ctx context.Context, id string, mt MediaType, title string, year *int, relPath string, fileSize *int64) error {
	now := time.Now().UTC()
	var exists bool
	_ = s.db.QueryRowContext(ctx, `SELECT 1 FROM library_items WHERE id = ?`, id).Scan(&exists)

	if exists {
		_, err := s.db.ExecContext(ctx,
			`UPDATE library_items SET last_seen = ?, file_size = ? WHERE id = ?`,
			now, fileSize, id)
		return err
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO library_items (id, peer_id, media_type, title, year, relative_path, file_size, last_seen)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?)`,
		id, string(mt), title, year, relPath, fileSize, now,
	)
	if err != nil {
		return fmt.Errorf("insert library item: %w", err)
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "catalog.item_added",
		TargetType: "library_item",
		TargetID:   id,
		Detail:     relPath,
	})
	return nil
}

// StartScheduled runs ScanAll on the given interval until ctx is cancelled.
func (s *Scanner) StartScheduled(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.ScanAll(ctx); err != nil {
					slog.Error("scheduled scan failed", "err", err)
				}
			}
		}
	}()
}

// GetAll returns all library items.
func GetAll(ctx context.Context, db *sql.DB) ([]*Item, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
		        relative_path, file_size, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, metadata_at
		 FROM library_items`)
	if err != nil {
		return nil, fmt.Errorf("query library items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// GetByID returns a single library item.
func GetByID(ctx context.Context, db *sql.DB, id string) (*Item, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
		        relative_path, file_size, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, metadata_at
		 FROM library_items WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("item not found")
	}
	return items[0], nil
}

// GetStaleMetadata returns items with no or stale metadata.
func GetStaleMetadata(ctx context.Context, db *sql.DB) ([]*Item, error) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	rows, err := db.QueryContext(ctx,
		`SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
		        relative_path, file_size, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, metadata_at
		 FROM library_items
		 WHERE metadata_at IS NULL OR metadata_at < ?`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query stale metadata: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

func scanItems(rows *sql.Rows) ([]*Item, error) {
	var items []*Item
	for rows.Next() {
		item := &Item{}
		if err := rows.Scan(
			&item.ID, &item.PeerID, &item.MediaType, &item.Title, &item.Year,
			&item.Series, &item.SeasonNum, &item.EpisodeNum,
			&item.RelativePath, &item.FileSize, &item.TmdbID, &item.OlKey,
			&item.PosterURL, &item.Description, &item.Rating, &item.Genres,
			&item.LastSeen, &item.MetadataAt,
		); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func itemID(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func isMediaFile(ext string, mt MediaType) bool {
	switch mt {
	case Movie, TVShow, TVSeason, TVEpisode:
		return ext == ".mkv" || ext == ".mp4" || ext == ".avi" || ext == ".mov"
	case Audiobook:
		return ext == ".mp3" || ext == ".m4b" || ext == ".flac" || ext == ".ogg"
	case Ebook:
		return ext == ".epub" || ext == ".mobi" || ext == ".pdf" || ext == ".azw3"
	}
	return false
}

func inferMediaType(root MediaType, rel string) MediaType {
	if root != TVShow {
		return root
	}
	parts := strings.Split(rel, string(filepath.Separator))
	switch len(parts) {
	case 1:
		return TVShow
	case 2:
		return TVSeason
	default:
		return TVEpisode
	}
}

func parseTitleYear(filename string) (string, *int) {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	// Try to extract year in parens: "Title (2023)"
	if idx := strings.LastIndex(name, "("); idx != -1 {
		yearStr := strings.TrimRight(name[idx+1:], ")")
		var year int
		if _, err := fmt.Sscanf(yearStr, "%d", &year); err == nil && year > 1800 && year < 2100 {
			title := strings.TrimSpace(name[:idx])
			return title, &year
		}
	}
	return name, nil
}
