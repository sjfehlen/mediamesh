package config

import (
	"errors"
	"os"
)

type Config struct {
	NodeName   string
	PublicURL  string
	DataDir    string
	ListenAddr string

	JellyfinURL    string
	JellyfinAPIKey string
	AbsURL         string
	AbsAPIKey      string
	TMDBAPIKey     string
}

func Load() (*Config, error) {
	cfg := &Config{
		NodeName:   env("NODE_NAME", ""),
		PublicURL:  env("PUBLIC_URL", ""),
		DataDir:    env("DATA_DIR", "/data"),
		ListenAddr: env("LISTEN_ADDR", ":8080"),

		JellyfinURL:    env("JELLYFIN_URL", ""),
		JellyfinAPIKey: env("JELLYFIN_API_KEY", ""),
		AbsURL:         env("ABS_URL", ""),
		AbsAPIKey:      env("ABS_API_KEY", ""),
		TMDBAPIKey:     env("TMDB_API_KEY", ""),
	}

	if cfg.NodeName == "" {
		return nil, errors.New("NODE_NAME is required")
	}
	if cfg.PublicURL == "" {
		return nil, errors.New("PUBLIC_URL is required")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
