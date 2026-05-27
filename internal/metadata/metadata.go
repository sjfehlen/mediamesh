package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/sjfehlen/mediamesh/internal/catalog"
)

// Fetcher retrieves metadata from external APIs.
type Fetcher struct {
	tmdbKey    string
	tmdbClient *tmdb.Client
	httpClient *http.Client
}

// New creates a new Fetcher.
func New(tmdbKey string) *Fetcher {
	f := &Fetcher{
		tmdbKey:    tmdbKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	if tmdbKey != "" {
		c, err := tmdb.Init(tmdbKey)
		if err == nil {
			f.tmdbClient = c
		} else {
			slog.Warn("metadata: failed to init TMDB client", "err", err)
		}
	}
	return f
}

// EnrichItem fetches and stores metadata for a single item.
// If metadata is already fresh (< 7 days), it is skipped.
func (f *Fetcher) EnrichItem(ctx context.Context, db *sql.DB, item *catalog.Item) error {
	if item.MetadataAt != nil && time.Since(*item.MetadataAt) < 7*24*time.Hour {
		return nil
	}

	var err error
	switch item.MediaType {
	case catalog.Movie, catalog.TVShow, catalog.TVSeason, catalog.TVEpisode:
		err = f.enrichTMDB(ctx, db, item)
	case catalog.Audiobook, catalog.Ebook:
		err = f.enrichOpenLibrary(ctx, db, item)
	}

	if err != nil {
		slog.Error("metadata fetch failed", "item", item.ID, "err", err)
		return nil // never fail hard
	}
	return nil
}

// EnrichAll fetches metadata for all items that are missing or stale.
func (f *Fetcher) EnrichAll(ctx context.Context, db *sql.DB) error {
	items, err := catalog.GetStaleMetadata(ctx, db)
	if err != nil {
		return fmt.Errorf("metadata.Fetcher.EnrichAll: get stale metadata items: %w", err)
	}
	for _, item := range items {
		if err := f.EnrichItem(ctx, db, item); err != nil {
			slog.Error("enrich item failed", "id", item.ID, "err", err)
		}
	}
	return nil
}

// StartScheduled runs EnrichAll on the given interval.
func (f *Fetcher) StartScheduled(ctx context.Context, db *sql.DB, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := f.EnrichAll(ctx, db); err != nil {
					slog.Error("scheduled enrich failed", "err", err)
				}
			}
		}
	}()
}

func (f *Fetcher) enrichTMDB(ctx context.Context, db *sql.DB, item *catalog.Item) error {
	if f.tmdbClient == nil {
		return nil
	}

	var (
		tmdbID     int64
		posterURL  string
		overview   string
		rating     float64
		genresStr  string
	)

	urlOptions := make(map[string]string)
	if item.Year != nil {
		urlOptions["year"] = fmt.Sprintf("%d", *item.Year)
	}

	switch item.MediaType {
	case catalog.TVShow, catalog.TVSeason, catalog.TVEpisode:
		result, err := f.tmdbClient.GetSearchTVShow(item.Title, urlOptions)
		if err != nil {
			return fmt.Errorf("metadata.Fetcher.enrichTMDB: search TV: %w", err)
		}
		if result == nil || len(result.Results) == 0 {
			return nil
		}
		first := result.Results[0]
		tmdbID = int64(first.ID)
		if first.PosterPath != "" {
			posterURL = "https://image.tmdb.org/t/p/w500" + first.PosterPath
		}
		overview = first.Overview
		rating = float64(first.VoteAverage)
		ids := make([]int64, len(first.GenreIDs))
		for i, g := range first.GenreIDs {
			ids[i] = g
		}
		b, _ := json.Marshal(ids)
		genresStr = string(b)

	default: // Movie
		result, err := f.tmdbClient.GetSearchMovies(item.Title, urlOptions)
		if err != nil {
			return fmt.Errorf("metadata.Fetcher.enrichTMDB: search movie: %w", err)
		}
		if result == nil || len(result.Results) == 0 {
			return nil
		}
		first := result.Results[0]
		tmdbID = int64(first.ID)
		if first.PosterPath != "" {
			posterURL = "https://image.tmdb.org/t/p/w500" + first.PosterPath
		}
		overview = first.Overview
		rating = float64(first.VoteAverage)
		ids := make([]int64, len(first.GenreIDs))
		for i, g := range first.GenreIDs {
			ids[i] = g
		}
		b, _ := json.Marshal(ids)
		genresStr = string(b)
	}

	now := time.Now().UTC()
	_, err := db.ExecContext(ctx,
		`UPDATE library_items SET tmdb_id = ?, poster_url = ?, description = ?, rating = ?, genres = ?, metadata_at = ? WHERE id = ?`,
		tmdbID, posterURL, overview, rating, genresStr, now, item.ID,
	)
	return err
}

type olSearchResult struct {
	Docs []struct {
		Key           string `json:"key"`
		Title         string `json:"title"`
		CoverI        int    `json:"cover_i"`
		FirstSentence string `json:"first_sentence"`
	} `json:"docs"`
}

func (f *Fetcher) enrichOpenLibrary(ctx context.Context, db *sql.DB, item *catalog.Item) error {
	params := url.Values{}
	params.Set("title", item.Title)
	if item.Series != nil {
		params.Set("author", *item.Series)
	}

	apiURL := "https://openlibrary.org/search.json?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("metadata.Fetcher.enrichOpenLibrary: build request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metadata.Fetcher.enrichOpenLibrary: request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result olSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("metadata.Fetcher.enrichOpenLibrary: parse response: %w", err)
	}
	if len(result.Docs) == 0 {
		return nil
	}

	first := result.Docs[0]
	posterURL := ""
	if first.CoverI > 0 {
		posterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", first.CoverI)
	}

	now := time.Now().UTC()
	_, err = db.ExecContext(ctx,
		`UPDATE library_items SET ol_key = ?, poster_url = ?, description = ?, metadata_at = ? WHERE id = ?`,
		first.Key, posterURL, first.FirstSentence, now, item.ID,
	)
	return err
}
