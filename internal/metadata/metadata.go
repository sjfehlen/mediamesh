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
	"github.com/sjfehlen/mediamesh/internal/config"
)

// Fetcher retrieves metadata from external APIs.
type Fetcher struct {
	tmdbClient    *tmdb.Client
	httpClient    *http.Client
	jellyfinURL   string
	jellyfinKey   string
	absURL        string
	absKey        string
}

// New creates a new Fetcher with all configured sources.
func New(cfg *config.Config) *Fetcher {
	f := &Fetcher{
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		jellyfinURL: cfg.JellyfinURL,
		jellyfinKey: cfg.JellyfinAPIKey,
		absURL:      cfg.AbsURL,
		absKey:      cfg.AbsAPIKey,
	}
	if cfg.TMDBAPIKey != "" {
		c, err := tmdb.Init(cfg.TMDBAPIKey)
		if err == nil {
			f.tmdbClient = c
		} else {
			slog.Warn("metadata: failed to init TMDB client", "err", err)
		}
	}
	return f
}

// EnrichItem fetches and stores metadata for a single item.
// Priority: Jellyfin (movies/TV) → ABS (audiobooks) → TMDB → OpenLibrary.
// Skips items whose metadata was fetched within the last 7 days.
func (f *Fetcher) EnrichItem(ctx context.Context, db *sql.DB, item *catalog.Item) error {
	if item.MetadataAt != nil && time.Since(*item.MetadataAt) < 7*24*time.Hour {
		return nil
	}

	var err error
	switch item.MediaType {
	case catalog.Movie, catalog.TVShow, catalog.TVSeason, catalog.TVEpisode:
		if f.jellyfinURL != "" {
			if enriched, jellyErr := f.enrichJellyfin(ctx, db, item); jellyErr != nil {
				slog.Warn("jellyfin metadata failed, falling back to TMDB", "item", item.ID, "err", jellyErr)
			} else if enriched {
				return nil
			}
		}
		err = f.enrichTMDB(ctx, db, item)

	case catalog.Audiobook:
		if f.absURL != "" {
			if enriched, absErr := f.enrichABS(ctx, db, item); absErr != nil {
				slog.Warn("audiobookshelf metadata failed, falling back to OpenLibrary", "item", item.ID, "err", absErr)
			} else if enriched {
				return nil
			}
		}
		err = f.enrichOpenLibrary(ctx, db, item)

	case catalog.Ebook:
		err = f.enrichOpenLibrary(ctx, db, item)
	}

	if err != nil {
		slog.Error("metadata fetch failed", "item", item.ID, "err", err)
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

// StartScheduled runs EnrichAll immediately and then on the given interval.
func (f *Fetcher) StartScheduled(ctx context.Context, db *sql.DB, interval time.Duration) {
	go func() {
		if err := f.EnrichAll(ctx, db); err != nil {
			slog.Error("initial enrich failed", "err", err)
		}
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

// --- Jellyfin ---

type jellyfinItem struct {
	Name             string  `json:"Name"`
	OriginalTitle    string  `json:"OriginalTitle"`
	Overview         string  `json:"Overview"`
	CommunityRating  float64 `json:"CommunityRating"`
	ImageTags        map[string]string `json:"ImageTags"`
	BackdropImageTags []string         `json:"BackdropImageTags"`
	ProviderIDs      map[string]string `json:"ProviderIds"`
	Genres           []string `json:"Genres"`
	ProductionYear   int      `json:"ProductionYear"`
	ID               string   `json:"Id"`
}

type jellyfinSearchResult struct {
	Items            []jellyfinItem `json:"Items"`
	TotalRecordCount int            `json:"TotalRecordCount"`
}

func (f *Fetcher) enrichJellyfin(ctx context.Context, db *sql.DB, item *catalog.Item) (bool, error) {
	searchTitle := item.Title
	if item.Series != nil {
		searchTitle = *item.Series
	}

	params := url.Values{}
	params.Set("searchTerm", searchTitle)
	params.Set("IncludeItemTypes", jellyfinItemType(item.MediaType))
	params.Set("Recursive", "true")
	params.Set("Fields", "Overview,Genres,ProviderIds,ImageTags,CommunityRating")
	params.Set("Limit", "5")
	params.Set("api_key", f.jellyfinKey)

	apiURL := f.jellyfinURL + "/Items?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("jellyfin: build request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("jellyfin: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("jellyfin: status %d", resp.StatusCode)
	}

	var result jellyfinSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("jellyfin: decode: %w", err)
	}
	if len(result.Items) == 0 {
		return false, nil
	}

	jItem := result.Items[0]

	title := jItem.Name
	if jItem.OriginalTitle != "" {
		title = jItem.OriginalTitle
	}

	posterURL := ""
	if tag, ok := jItem.ImageTags["Primary"]; ok && jItem.ID != "" {
		posterURL = fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s&api_key=%s", f.jellyfinURL, jItem.ID, tag, f.jellyfinKey)
	}

	genresJSON, _ := json.Marshal(jItem.Genres)

	now := time.Now().UTC()
	_, err = db.ExecContext(ctx,
		`UPDATE library_items SET meta_title = ?, poster_url = ?, description = ?, rating = ?, genres = ?, metadata_at = ? WHERE id = ?`,
		title, posterURL, jItem.Overview, jItem.CommunityRating, string(genresJSON), now, item.ID,
	)
	return err == nil, err
}

func jellyfinItemType(mt catalog.MediaType) string {
	switch mt {
	case catalog.TVShow, catalog.TVSeason, catalog.TVEpisode:
		return "Series"
	default:
		return "Movie"
	}
}

// --- Audiobookshelf ---

type absLibraryItem struct {
	ID    string `json:"id"`
	Media struct {
		Metadata struct {
			Title       string   `json:"title"`
			AuthorName  string   `json:"authorName"`
			Description string   `json:"description"`
			Genres      []string `json:"genres"`
		} `json:"metadata"`
		CoverPath string `json:"coverPath"`
	} `json:"media"`
}

type absSearchResult struct {
	Book *absLibraryItem `json:"book"`
}

func (f *Fetcher) enrichABS(ctx context.Context, db *sql.DB, item *catalog.Item) (bool, error) {
	params := url.Values{}
	params.Set("q", item.Title)

	apiURL := f.absURL + "/api/search/books?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("abs: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.absKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("abs: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("abs: status %d", resp.StatusCode)
	}

	// ABS returns an array of { book: LibraryItem } results.
	var results []absSearchResult
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &results); err != nil {
		return false, fmt.Errorf("abs: decode: %w", err)
	}
	if len(results) == 0 || results[0].Book == nil {
		return false, nil
	}

	absItem := results[0].Book
	meta := absItem.Media.Metadata

	posterURL := ""
	if absItem.ID != "" {
		posterURL = fmt.Sprintf("%s/api/items/%s/cover?token=%s", f.absURL, absItem.ID, f.absKey)
	}

	genresJSON, _ := json.Marshal(meta.Genres)

	now := time.Now().UTC()
	_, err = db.ExecContext(ctx,
		`UPDATE library_items SET meta_title = ?, poster_url = ?, description = ?, genres = ?, metadata_at = ? WHERE id = ?`,
		meta.Title, posterURL, meta.Description, string(genresJSON), now, item.ID,
	)
	return err == nil, err
}

// --- TMDB ---

func (f *Fetcher) enrichTMDB(ctx context.Context, db *sql.DB, item *catalog.Item) error {
	if f.tmdbClient == nil {
		return nil
	}

	var (
		tmdbID    int64
		metaTitle string
		posterURL string
		overview  string
		rating    float64
		genresStr string
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
		metaTitle = first.Name
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
		metaTitle = first.Title
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
		`UPDATE library_items SET tmdb_id = ?, meta_title = ?, poster_url = ?, description = ?, rating = ?, genres = ?, metadata_at = ? WHERE id = ?`,
		tmdbID, metaTitle, posterURL, overview, rating, genresStr, now, item.ID,
	)
	return err
}

// --- OpenLibrary ---

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
		`UPDATE library_items SET ol_key = ?, meta_title = ?, poster_url = ?, description = ?, metadata_at = ? WHERE id = ?`,
		first.Key, first.Title, posterURL, first.FirstSentence, now, item.ID,
	)
	return err
}
