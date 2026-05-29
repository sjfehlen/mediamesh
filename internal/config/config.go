package config

import (
	"errors"
	"os"
	"strconv"
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

	OIDCIssuer       string // OIDC_ISSUER
	OIDCClientID     string // OIDC_CLIENT_ID
	OIDCClientSecret string // OIDC_CLIENT_SECRET

	MaxConcurrentTransfers int // MAX_CONCURRENT_TRANSFERS (default 2)
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

		OIDCIssuer:       env("OIDC_ISSUER", ""),
		OIDCClientID:     env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("OIDC_CLIENT_SECRET", ""),

		MaxConcurrentTransfers: envInt("MAX_CONCURRENT_TRANSFERS", 2),
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

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
