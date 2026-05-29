package main

//go:embed ../../migrations
var migrationsDir embed.FS

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sjfehlen/mediamesh/internal/api"
	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/auth"
	"github.com/sjfehlen/mediamesh/internal/catalog"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/db"
	"github.com/sjfehlen/mediamesh/internal/identity"
	"github.com/sjfehlen/mediamesh/internal/library"
	"github.com/sjfehlen/mediamesh/internal/metadata"
	"github.com/sjfehlen/mediamesh/internal/peers"
	"github.com/sjfehlen/mediamesh/internal/requests"
	"github.com/sjfehlen/mediamesh/internal/transfers"
	"github.com/sjfehlen/mediamesh/internal/users"
	"github.com/sjfehlen/mediamesh/internal/webhooks"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	slog.Info("mediamesh starting", "node", cfg.NodeName, "url", cfg.PublicURL)

	// 1. Open DB (runs migrations).
	// database is *sql.DB (used by sqlc-generated code and most packages).
	// _ is *sqlx.DB (available for ad-hoc queries; not wired here yet but
	//   packages may accept it when convenient).
	migrationsFS, err := fs.Sub(migrationsDir, "migrations")
	if err != nil {
		slog.Error("failed to get migrations FS", "err", err)
		os.Exit(1)
	}
	database, _, err := db.Open(cfg.DataDir, migrationsFS)
	if err != nil {
		return err
	}
	defer database.Close()

	// 2. Load identity.
	id, err := identity.Load(cfg.DataDir)
	if err != nil {
		return err
	}

	// 3. Seed default libraries for any standard mount paths that exist.
	if err := library.SeedDefaults(ctx, database); err != nil {
		return fmt.Errorf("seed libraries: %w", err)
	}

	// 4. Init audit log.
	auditLog := audit.New(database)

	// 4. Init users store.
	userStore := users.NewStore(database, auditLog)

	// 5. Init auth manager (including OIDC setup if configured).
	authMgr, err := auth.New(ctx, database, userStore, cfg)
	if err != nil {
		return err
	}

	// 6. Init catalog scanner, start scheduled scan (1 hour interval).
	scanner := catalog.NewScanner(database, cfg, auditLog)
	scanner.StartScheduled(ctx, time.Hour)

	// 7. Init metadata fetcher, start scheduled enrich (12 hour interval).
	metaFetcher := metadata.New(cfg.TMDBAPIKey)
	metaFetcher.StartScheduled(ctx, database, 12*time.Hour)

	// 8. Init peers manager.
	peerMgr := peers.New(database, id, cfg, auditLog)

	// 9. Start peer heartbeat.
	peerMgr.StartHeartbeat(ctx)

	// 9. Init webhook dispatcher.
	dispatcher := webhooks.New(database, cfg.NodeName)

	// 10. Init requests store.
	requestStore := requests.NewStore(database, auditLog, dispatcher)

	// 11. Init transfers engine, start background worker.
	transferEngine := transfers.NewEngine(database, peerMgr, cfg, auditLog, dispatcher)
	transferEngine.Start(ctx)

	// 12. Init API server.
	srv := api.New(cfg, database, id, userStore, authMgr, scanner, peerMgr, requestStore, transferEngine, auditLog, dispatcher)
	handler := srv.Handler()

	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	// 12. Start HTTP server.
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	// 13. Wait for shutdown signal.
	<-ctx.Done()
	slog.Info("shutting down")

	// Graceful shutdown with 30s timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown error", "err", err)
	}

	return nil
}
