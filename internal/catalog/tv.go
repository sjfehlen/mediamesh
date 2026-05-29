package catalog

import (
	"context"
	"database/sql"
	"fmt"
)

// TVSeriesSummary represents aggregated data for a single TV series.
type TVSeriesSummary struct {
	Series       string   `json:"series"`
	SeasonCount  int      `json:"season_count"`
	EpisodeCount int      `json:"episode_count"`
	PosterURL    *string  `json:"poster_url,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Rating       *float64 `json:"rating,omitempty"`
	// TODO: add peer availability tracking (which peers have full/partial/no episodes)
}

// TVSeasonSummary represents aggregated data for a single season of a TV series.
type TVSeasonSummary struct {
	Series       string  `json:"series"`
	SeasonNum    int     `json:"season_num"`
	EpisodeCount int     `json:"episode_count"`
	PosterURL    *string `json:"poster_url,omitempty"`
}

// GetTVSeries returns one summary entry per distinct TV series.
func GetTVSeries(ctx context.Context, db *sql.DB) ([]*TVSeriesSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT series,
		       COUNT(DISTINCT season_num) as season_count,
		       COUNT(*) as episode_count,
		       MAX(poster_url) as poster_url,
		       MAX(description) as description,
		       MAX(rating) as rating
		FROM library_items
		WHERE media_type = 'tvepisode' AND series IS NOT NULL
		GROUP BY series
		ORDER BY series
	`)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetTVSeries: %w", err)
	}
	defer rows.Close()

	var results []*TVSeriesSummary
	for rows.Next() {
		s := &TVSeriesSummary{}
		if err := rows.Scan(&s.Series, &s.SeasonCount, &s.EpisodeCount, &s.PosterURL, &s.Description, &s.Rating); err != nil {
			return nil, fmt.Errorf("catalog.GetTVSeries scan: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog.GetTVSeries rows: %w", err)
	}
	return results, nil
}

// GetTVSeasons returns one summary entry per season for the given series.
func GetTVSeasons(ctx context.Context, db *sql.DB, series string) ([]*TVSeasonSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT series, season_num,
		       COUNT(*) as episode_count,
		       MAX(poster_url) as poster_url
		FROM library_items
		WHERE media_type = 'tvepisode' AND series = ?
		GROUP BY series, season_num
		ORDER BY season_num
	`, series)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetTVSeasons: %w", err)
	}
	defer rows.Close()

	var results []*TVSeasonSummary
	for rows.Next() {
		s := &TVSeasonSummary{}
		if err := rows.Scan(&s.Series, &s.SeasonNum, &s.EpisodeCount, &s.PosterURL); err != nil {
			return nil, fmt.Errorf("catalog.GetTVSeasons scan: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog.GetTVSeasons rows: %w", err)
	}
	return results, nil
}

// GetTVEpisodes returns all episodes for the given series and season number.
func GetTVEpisodes(ctx context.Context, db *sql.DB, series string, seasonNum int) ([]*Item, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, peer_id, media_type, title, year, series, season_num, episode_num,
		       relative_path, file_size, track_count, tmdb_id, ol_key,
		       poster_url, description, rating, genres, last_seen, file_mtime, metadata_at
		FROM library_items
		WHERE media_type = 'tvepisode' AND series = ? AND season_num = ?
		ORDER BY episode_num
	`, series, seasonNum)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetTVEpisodes: %w", err)
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return nil, fmt.Errorf("catalog.GetTVEpisodes: %w", err)
	}
	return items, nil
}
