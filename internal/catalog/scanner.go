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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/library"
)

// MediaType describes the kind of media an item represents.
type MediaType string

const (
	Movie     MediaType = "movie"
	TVShow    MediaType = "tvshow"
	TVSeason  MediaType = "tvseason"
	TVEpisode MediaType = "tvepisode"
	Audiobook MediaType = "audiobook"
	Ebook     MediaType = "ebook"
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
	TrackCount   *int       `json:"track_count,omitempty"`
	TmdbID       *int64     `json:"tmdb_id,omitempty"`
	OlKey        *string    `json:"ol_key,omitempty"`
	PosterURL    *string    `json:"poster_url,omitempty"`
	Description  *string    `json:"description,omitempty"`
	Rating       *float64   `json:"rating,omitempty"`
	Genres       *string    `json:"genres,omitempty"`
	LastSeen     time.Time  `json:"last_seen"`
	FileMtime    *time.Time `json:"file_mtime,omitempty"`
	MetadataAt   *time.Time `json:"metadata_at,omitempty"`
}

// TVEpisodeInfo holds parsed fields from a TV episode filename.
type TVEpisodeInfo struct {
	SeasonNum  int
	EpisodeNum int
	Title      string // may be empty
}

// reEpisode matches patterns like S01E02, S1E2, S01E02 - Title, etc.
var reEpisode = regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,2})(?:[Ee]\d{1,2})?(?:\s*[-–]\s*(.+))?`)

// ignoredDirs is a set of folder names to skip during walking.
var ignoredDirs = map[string]bool{
	"@eaDir":           true,
	"Extras":           true,
	"Featurettes":      true,
	"Behind The Scenes": true,
	"Interviews":       true,
	"Scenes":           true,
	"Shorts":           true,
	"Trailers":         true,
}

// ignoredFiles is a set of exact filenames to skip.
var ignoredFiles = map[string]bool{
	".DS_Store":    true,
	"Thumbs.db":    true,
	"feeder.json":  true,
}

// videoExts contains supported video file extensions.
var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
	".mov": true, ".ts": true, ".wmv": true, ".m2ts": true,
}

// audioExts contains supported audio file extensions.
var audioExts = map[string]bool{
	".mp3": true, ".m4b": true, ".flac": true, ".ogg": true,
	".aac": true, ".opus": true, ".wav": true,
}

// ebookExts contains supported ebook file extensions.
var ebookExts = map[string]bool{
	".epub": true, ".pdf": true, ".mobi": true, ".azw3": true,
	".cbz": true, ".cbr": true,
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
	if root.mediaType == Audiobook {
		return s.scanAudiobookRoot(ctx, root.path)
	}
	return s.scanFileRoot(ctx, root)
}

// scanFileRoot walks a root file-by-file (movies, TV, ebooks).
func (s *Scanner) scanFileRoot(ctx context.Context, root mediaRoot) (int, error) {
	var count int
	err := filepath.WalkDir(root.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipFile(d.Name()) {
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
		var fileMtime *time.Time
		if info != nil {
			sz := info.Size()
			fileSize = &sz
			mt := info.ModTime().UTC()
			fileMtime = &mt
		}

		// Skip if mtime unchanged.
		if s.mtimeUnchanged(ctx, "local:"+rel, fileMtime) {
			return nil
		}

		mt := inferMediaType(root.mediaType, rel)
		base := filepath.Base(rel)

		item := &Item{
			ID:           itemID("local:" + rel),
			MediaType:    mt,
			RelativePath: rel,
			FileSize:     fileSize,
			FileMtime:    fileMtime,
		}

		if mt == TVEpisode {
			ep := parseTVEpisode(base)
			if ep != nil {
				item.SeasonNum = &ep.SeasonNum
				item.EpisodeNum = &ep.EpisodeNum
				if ep.Title != "" {
					item.Title = ep.Title
				} else {
					item.Title = base
				}
			} else {
				item.Title, item.Year = parseTitleYear(base)
			}
			// Series name is the top-level folder under root.
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			series := parts[0]
			item.Series = &series
		} else {
			item.Title, item.Year = parseTitleYear(base)
		}

		if err := s.upsertItem(ctx, item); err != nil {
			slog.Error("upsert item failed", "path", rel, "err", err)
			return nil
		}
		count++
		return nil
	})
	return count, err
}

// scanAudiobookRoot treats each immediate subfolder as one audiobook.
func (s *Scanner) scanAudiobookRoot(ctx context.Context, rootPath string) (int, error) {
	entries, err := fs.ReadDir(newFS(rootPath), ".")
	if err != nil {
		return 0, fmt.Errorf("catalog.scanAudiobookRoot: %w", err)
	}

	var count int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if shouldSkipDir(entry.Name()) {
			continue
		}

		bookDir := filepath.Join(rootPath, entry.Name())
		totalSize, trackCount, latestMtime := audiobookDirStats(bookDir)

		rel := entry.Name()
		key := "local:audiobook:" + rel

		if s.mtimeUnchanged(ctx, key, latestMtime) {
			continue
		}

		item := &Item{
			ID:           itemID(key),
			MediaType:    Audiobook,
			Title:        entry.Name(),
			RelativePath: rel,
			FileSize:     &totalSize,
			TrackCount:   &trackCount,
			FileMtime:    latestMtime,
		}

		if err := s.upsertItem(ctx, item); err != nil {
			slog.Error("upsert audiobook failed", "path", rel, "err", err)
			continue
		}
		count++
	}
	return count, nil
}

// audiobookDirStats returns total size, track count, and latest mtime for audio files in a dir.
func audiobookDirStats(dirPath string) (totalSize int64, trackCount int, latestMtime *time.Time) {
	_ = filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !audioExts[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		totalSize += info.Size()
		trackCount++
		mt := info.ModTime().UTC()
		if latestMtime == nil || mt.After(*latestMtime) {
			latestMtime = &mt
		}
		return nil
	})
	return
}

// newFS returns an fs.FS rooted at the given path.
func newFS(root string) fs.FS {
	return realFS{root}
}

type realFS struct{ root string }

func (r realFS) Open(name string) (fs.File, error) {
	return openFSFile(r.root, name)
}

// mtimeUnchanged returns true if the stored mtime for the given key matches the provided mtime.
func (s *Scanner) mtimeUnchanged(ctx context.Context, key string, mtime *time.Time) bool {
	if mtime == nil {
		return false
	}
	id := itemID(key)
	var stored sql.NullTime
	_ = s.db.QueryRowContext(ctx, `SELECT file_mtime FROM library_items WHERE id = ?`, id).Scan(&stored)
	if !stored.Valid {
		return false
	}
	return stored.Time.UTC().Equal(mtime.UTC())
}

func (s *Scanner) upsertItem(ctx context.Context, item *Item) error {
	now := time.Now().UTC()
	item.LastSeen = now

	var exists bool
	_ = s.db.QueryRowContext(ctx, `SELECT 1 FROM library_items WHERE id = ?`, item.ID).Scan(&exists)

	if exists {
		_, err := s.db.ExecContext(ctx,
			`UPDATE library_items SET last_seen = ?, file_size = ?, file_mtime = ?, track_count = ?,
			  title = ?, series = ?, season_num = ?, episode_num = ?, media_type = ?
			 WHERE id = ?`,
			now, item.FileSize, item.FileMtime, item.TrackCount,
			item.Title, item.Series, item.SeasonNum, item.EpisodeNum, string(item.MediaType),
			item.ID)
		return err
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO library_items
		  (id, peer_id, media_type, title, year, series, season_num, episode_num,
		   relative_path, file_size, track_count, file_mtime, last_seen)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, string(item.MediaType), item.Title, item.Year,
		item.Series, item.SeasonNum, item.EpisodeNum,
		item.RelativePath, item.FileSize, item.TrackCount, item.FileMtime, now,
	)
	if err != nil {
		return fmt.Errorf("catalog.upsertItem: %w", err)
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "catalog.item_added",
		TargetType: "library_item",
		TargetID:   item.ID,
		Detail:     item.RelativePath,
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
		        relative_path, file_size, track_count, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, file_mtime, metadata_at
		 FROM library_items`)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetAll: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// GetByID returns a single library item.
func GetByID(ctx context.Context, db *sql.DB, id string) (*Item, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
		        relative_path, file_size, track_count, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, file_mtime, metadata_at
		 FROM library_items WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetByID: %w", err)
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
		        relative_path, file_size, track_count, tmdb_id, ol_key, poster_url, description,
		        rating, genres, last_seen, file_mtime, metadata_at
		 FROM library_items
		 WHERE metadata_at IS NULL OR metadata_at < ?`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetStaleMetadata: %w", err)
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
			&item.RelativePath, &item.FileSize, &item.TrackCount,
			&item.TmdbID, &item.OlKey,
			&item.PosterURL, &item.Description, &item.Rating, &item.Genres,
			&item.LastSeen, &item.FileMtime, &item.MetadataAt,
		); err != nil {
			return nil, fmt.Errorf("catalog.scanItems: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func itemID(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// isMediaFile reports whether ext is a valid media extension for the given type.
func isMediaFile(ext string, mt MediaType) bool {
	switch mt {
	case Movie, TVShow, TVSeason, TVEpisode:
		return videoExts[ext]
	case Audiobook:
		return audioExts[ext]
	case Ebook:
		return ebookExts[ext]
	}
	return false
}

// inferMediaType returns the specific media type based on path depth within a TV root.
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

// parseTVEpisode parses a filename for SxxExx patterns.
// Returns nil if no pattern found.
func parseTVEpisode(filename string) *TVEpisodeInfo {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	m := reEpisode.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	season, _ := strconv.Atoi(m[1])
	episode, _ := strconv.Atoi(m[2])
	title := strings.TrimSpace(m[3])
	// Strip trailing quality/codec tags from title (e.g. " 1080p", " BluRay")
	if idx := indexQualityTag(title); idx > 0 {
		title = strings.TrimRight(strings.TrimSpace(title[:idx]), "-–")
		title = strings.TrimSpace(title)
	}
	return &TVEpisodeInfo{
		SeasonNum:  season,
		EpisodeNum: episode,
		Title:      title,
	}
}

// qualityTags are common suffixes to strip from parsed episode titles.
var qualityTagRe = regexp.MustCompile(`(?i)\b(1080[pi]|720[pi]|4[kK]|2160[pi]|BluRay|BDRip|WEB-DL|WEBRip|HDTV|DVDRip|x264|x265|HEVC|H\.264|AAC|AC3|DTS|REMUX)\b`)

func indexQualityTag(s string) int {
	loc := qualityTagRe.FindStringIndex(s)
	if loc == nil {
		return -1
	}
	return loc[0]
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

// shouldSkipDir reports whether a directory name should be skipped entirely.
func shouldSkipDir(name string) bool {
	return ignoredDirs[name]
}

// shouldSkipFile reports whether a file should be skipped.
func shouldSkipFile(name string) bool {
	if ignoredFiles[name] {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "sample-") || strings.HasPrefix(lower, "trailer-")
}
