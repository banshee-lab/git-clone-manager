package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitsync/internal/config"
	"gitsync/internal/events"
	"gitsync/internal/gitx"
	"gitsync/internal/history"
	"gitsync/internal/logstore"
	"gitsync/internal/respond"
	"gitsync/internal/store/bolt"
	"gitsync/internal/syncer"
	"gitsync/internal/webui"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to the YAML configuration file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	level := new(slog.LevelVar)
	switch cfg.Log.Level {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	var handler slog.Handler
	if cfg.Log.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	logger.Info("loaded configuration", "config", cfg.Redacted())
	if _, err := gitx.MakeHome(cfg.Storage.DataDir); err != nil {
		return err
	}

	logs, err := logstore.New(cfg.Storage.LogsDir)
	if err != nil {
		return err
	}
	db, err := bolt.Open(cfg.Storage.DataDir + "/gitsync.db")
	if err != nil {
		return err
	}
	defer db.Close()
	cleaner := history.New(db.Activities(), db.Repositories(), logs, cfg.History.MaxItems)
	hub := events.NewHub()
	executor, err := syncer.NewExecutor(cfg.Storage.DataDir, time.Duration(cfg.Sync.GitTimeout), time.Duration(cfg.Sync.PostCommandTimeout), cfg.Sync.EnvPassthrough)
	if err != nil {
		return err
	}
	manager, err := syncer.NewManager(db.Repositories(), db.Activities(), logs, executor, cleaner, nil, hub, cfg.Sync.MaxConcurrent)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout))
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			logger.Error("sync manager shutdown failed", "error", err)
		}
	}()
	if err := manager.Recover(context.Background()); err != nil {
		return fmt.Errorf("recover sync activities: %w", err)
	}
	if err := cleaner.Startup(context.Background()); err != nil {
		return fmt.Errorf("startup history cleanup: %w", err)
	}

	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           newMux(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server", "address", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout))
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return <-serverErr
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/", webui.Handler())
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
