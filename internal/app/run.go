package app

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/containeroo/httpgrace/server"
	"github.com/containeroo/tinyflags"
	"github.com/gi8lino/screendeck/internal/config"
	"github.com/gi8lino/screendeck/internal/handler"
	"github.com/gi8lino/screendeck/internal/logging"
	"github.com/gi8lino/screendeck/internal/maintenance"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/gi8lino/screendeck/internal/routes"
	"github.com/gi8lino/screendeck/internal/store"
)

// Run wires the application dependencies and serves until shutdown.
func Run(ctx context.Context, appFS fs.FS, version, commit string, args []string, stdout, stderr io.Writer) error {
	cfg, err := config.Parse(args, version)
	if err != nil {
		if tinyflags.IsHelpRequested(err) || tinyflags.IsVersionRequested(err) {
			fmt.Fprint(stdout, err.Error()) // nolint:errcheck
			return nil
		}
		fmt.Fprintln(stderr, err) // nolint:errcheck
		return err
	}

	logger := logging.SetupLogger(cfg.LogFormat, cfg.Debug, stdout)
	setupLogger := logger.With("component", "setup")
	setupLogger.Info("starting ScreenDeck",
		"event", "app_starting",
		"version", version,
		"commit", commit,
	)
	if len(cfg.Overridden) > 0 {
		setupLogger.Info("CLI overrides",
			"event", "cli_overrides",
			"overrides", cfg.Overridden,
		)
	}

	database, err := store.Open(cfg.DatabasePath, cfg.AuthKeyPath)
	if err != nil {
		setupLogger.Error("application failed",
			"event", "app_failed",
			"stage", "open_database",
			"error", err,
		)
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close() // nolint:errcheck

	authManager, err := plex.NewAuthManager(
		ctx,
		database,
		logger.With("component", "plex"),
		"https://clients.plex.tv",
		cfg.PlexURLOverride,
		cfg.Experimental,
	)
	if err != nil {
		setupLogger.Error("application failed",
			"event", "app_failed",
			"stage", "configure_plex",
			"error", err,
		)
		return fmt.Errorf("configure Plex: %w", err)
	}

	roomService := room.NewService(database, authManager, cfg.RoomTTL, cfg.ExcludeLibraries)

	serverLogger := logger.With("component", "server")
	api := handler.New(version, commit, cfg.BaseURL, cfg.Experimental, roomService, authManager, serverLogger)

	router, err := routes.NewRouter(appFS, api, serverLogger, cfg.Debug)
	if err != nil {
		setupLogger.Error("application failed",
			"event", "app_failed",
			"stage", "create_router",
			"error", err,
		)
		return fmt.Errorf("configure router: %w", err)
	}

	ctx, stop := server.SignalContext(ctx)
	defer stop()

	go maintenance.RunRoomCleanup(ctx, database, cfg.RoomCleanupInterval, setupLogger)

	configured, serverName := authManager.Configured()
	serverLogger.Info("starting HTTP server",
		"event", "server_starting",
		"address", cfg.ListenAddress,
		"plex_configured", configured,
		"plex_server", serverName,
	)
	if err := server.Run(ctx, cfg.ListenAddress, router, serverLogger); err != nil {
		setupLogger.Error("application failed",
			"event", "app_failed",
			"stage", "run_server",
			"error", err,
		)
		return fmt.Errorf("run server: %w", err)
	}
	return nil
}
