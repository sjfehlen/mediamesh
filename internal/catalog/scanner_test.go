package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- parseTVEpisode tests ---

func TestParseTVEpisode_Standard(t *testing.T) {
	cases := []struct {
		filename   string
		wantSeason int
		wantEp     int
		wantTitle  string
	}{
		{"S01E02 - Pilot.mkv", 1, 2, "Pilot"},
		{"s03e12 - The One Where.mp4", 3, 12, "The One Where"},
		{"Show.S02E05.Title.Here.mkv", 2, 5, ""},
		{"S10E01.mkv", 10, 1, ""},
		{"S01E01 - Great Episode 1080p BluRay.mkv", 1, 1, "Great Episode"},
		{"S04E03 - Some Title - WEB-DL.mkv", 4, 3, "Some Title"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			ep := parseTVEpisode(tc.filename)
			if ep == nil {
				t.Fatalf("parseTVEpisode(%q) = nil, want result", tc.filename)
			}
			if ep.SeasonNum != tc.wantSeason {
				t.Errorf("SeasonNum = %d, want %d", ep.SeasonNum, tc.wantSeason)
			}
			if ep.EpisodeNum != tc.wantEp {
				t.Errorf("EpisodeNum = %d, want %d", ep.EpisodeNum, tc.wantEp)
			}
			if tc.wantTitle != "" && ep.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", ep.Title, tc.wantTitle)
			}
		})
	}
}

func TestParseTVEpisode_NoMatch(t *testing.T) {
	cases := []string{
		"The.Dark.Knight.2008.mkv",
		"Some Movie (2021).mp4",
		"",
	}
	for _, name := range cases {
		if ep := parseTVEpisode(name); ep != nil {
			t.Errorf("parseTVEpisode(%q) = %+v, want nil", name, ep)
		}
	}
}

// --- shouldSkipDir / shouldSkipFile tests ---

func TestShouldSkipDir(t *testing.T) {
	skip := []string{"@eaDir", "Extras", "Featurettes", "Behind The Scenes", "Trailers", "Shorts"}
	keep := []string{"Season 01", "Show Name", "Movies"}
	for _, d := range skip {
		if !shouldSkipDir(d) {
			t.Errorf("shouldSkipDir(%q) = false, want true", d)
		}
	}
	for _, d := range keep {
		if shouldSkipDir(d) {
			t.Errorf("shouldSkipDir(%q) = true, want false", d)
		}
	}
}

func TestShouldSkipFile(t *testing.T) {
	skip := []string{".DS_Store", "Thumbs.db", "feeder.json", "sample-video.mkv", "trailer-official.mp4"}
	keep := []string{"movie.mkv", "S01E01.mkv", "book.epub"}
	for _, f := range skip {
		if !shouldSkipFile(f) {
			t.Errorf("shouldSkipFile(%q) = false, want true", f)
		}
	}
	for _, f := range keep {
		if shouldSkipFile(f) {
			t.Errorf("shouldSkipFile(%q) = true, want false", f)
		}
	}
}

// --- isMediaFile tests ---

func TestIsMediaFile(t *testing.T) {
	cases := []struct {
		ext  string
		mt   MediaType
		want bool
	}{
		{".mkv", TVEpisode, true},
		{".m2ts", Movie, true},
		{".ts", TVEpisode, true},
		{".wmv", Movie, true},
		{".m4v", Movie, true},
		{".opus", Audiobook, true},
		{".aac", Audiobook, true},
		{".wav", Audiobook, true},
		{".cbz", Ebook, true},
		{".cbr", Ebook, true},
		{".txt", Movie, false},
		{".nfo", Ebook, false},
	}
	for _, tc := range cases {
		got := isMediaFile(tc.ext, tc.mt)
		if got != tc.want {
			t.Errorf("isMediaFile(%q, %v) = %v, want %v", tc.ext, tc.mt, got, tc.want)
		}
	}
}

// --- audiobookDirStats tests ---

func TestAudiobookDirStats(t *testing.T) {
	// Build a temp dir with audio files and one non-audio file.
	dir := t.TempDir()
	writeFile := func(name string, size int) {
		data := make([]byte, size)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("chapter01.mp3", 1000)
	writeFile("chapter02.m4b", 2000)
	writeFile("cover.jpg", 500) // should be ignored

	totalSize, trackCount, mtime := audiobookDirStats(dir)
	if totalSize != 3000 {
		t.Errorf("totalSize = %d, want 3000", totalSize)
	}
	if trackCount != 2 {
		t.Errorf("trackCount = %d, want 2", trackCount)
	}
	if mtime == nil {
		t.Error("mtime = nil, want non-nil")
	}
}

func TestAudiobookDirStats_Empty(t *testing.T) {
	dir := t.TempDir()
	totalSize, trackCount, mtime := audiobookDirStats(dir)
	if totalSize != 0 {
		t.Errorf("totalSize = %d, want 0", totalSize)
	}
	if trackCount != 0 {
		t.Errorf("trackCount = %d, want 0", trackCount)
	}
	if mtime != nil {
		t.Errorf("mtime = %v, want nil", mtime)
	}
}

// --- inferMediaType tests ---

func TestInferMediaType(t *testing.T) {
	cases := []struct {
		root MediaType
		rel  string
		want MediaType
	}{
		{TVShow, "Show Name", TVShow},
		{TVShow, filepath.Join("Show Name", "Season 01"), TVSeason},
		{TVShow, filepath.Join("Show Name", "Season 01", "S01E01.mkv"), TVEpisode},
		{Movie, "The Dark Knight (2008).mkv", Movie},
		{Ebook, "book.epub", Ebook},
	}
	for _, tc := range cases {
		got := inferMediaType(tc.root, tc.rel)
		if got != tc.want {
			t.Errorf("inferMediaType(%v, %q) = %v, want %v", tc.root, tc.rel, got, tc.want)
		}
	}
}

// --- parseTitleYear tests ---

func TestParseTitleYear(t *testing.T) {
	cases := []struct {
		input     string
		wantTitle string
		wantYear  *int
	}{
		{"The Dark Knight (2008).mkv", "The Dark Knight", intPtr(2008)},
		{"Movie.mkv", "Movie", nil},
		{"Bad Year (1700).mkv", "Bad Year (1700)", nil}, // out of range
	}
	for _, tc := range cases {
		title, year := parseTitleYear(tc.input)
		if title != tc.wantTitle {
			t.Errorf("parseTitleYear(%q) title = %q, want %q", tc.input, title, tc.wantTitle)
		}
		if tc.wantYear == nil && year != nil {
			t.Errorf("parseTitleYear(%q) year = %v, want nil", tc.input, year)
		}
		if tc.wantYear != nil {
			if year == nil {
				t.Errorf("parseTitleYear(%q) year = nil, want %d", tc.input, *tc.wantYear)
			} else if *year != *tc.wantYear {
				t.Errorf("parseTitleYear(%q) year = %d, want %d", tc.input, *year, *tc.wantYear)
			}
		}
	}
}

func intPtr(v int) *int { return &v }

// Ensure time package is used (for mtime comparisons in audiobookDirStats).
var _ = time.Now
