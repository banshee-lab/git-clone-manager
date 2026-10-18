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

	"gitsync/internal/api"
	"gitsync/internal/auth"
	"gitsync/internal/config"
	"gitsync/internal/events"
	"gitsync/internal/gitx"
	"gitsync/internal/history"
	"gitsync/internal/logstore"
	"gitsync/internal/respond"
	"gitsync/internal/store"
	"gitsync/internal/store/bolt"
	"gitsync/internal/syncer"
	"gitsync/internal/webhook"
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
	authService, err := auth.New(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("initialize mandatory OIDC authentication: %w", err)
	}
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
	appHandler, err := newMux(cfg, authService, db.Repositories(), db.Activities(), manager, hub, logs, cleaner)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           appHandler,
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

func newMux(cfg config.Config, authService *auth.Service, repos store.Repositories, activities store.Activities, manager api.Manager, hub *events.Hub, logs logstore.Store, cleaner interface {
	DeleteRepository(context.Context, string) error
}) (http.Handler, error) {
	mux := http.NewServeMux()
	apiService := api.New(cfg, repos, activities, manager, hub, logs, cleaner)
	webhookHandler := webhook.NewHandler(cfg.Webhooks.Secret, repos, manager, slog.Default())
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /auth/login", authService.Login)
	mux.HandleFunc("GET /auth/callback", authService.Callback)
	mux.HandleFunc("POST /auth/logout", authService.Logout)
	mux.Handle("GET /api/me", authService.RequireSession(http.HandlerFunc(auth.Me)))
	mux.Handle("GET /api/meta", authService.RequireSession(http.HandlerFunc(apiService.Meta)))
	mux.Handle("GET /api/dashboard", authService.RequireSession(http.HandlerFunc(apiService.Dashboard)))
	mux.Handle("GET /api/repositories", authService.RequireSession(http.HandlerFunc(apiService.ListRepositories)))
	mux.Handle("POST /api/repositories", authService.RequireSession(http.HandlerFunc(apiService.PostRepository)))
	mux.Handle("GET /api/repositories/", authService.RequireSession(http.HandlerFunc(apiService.GetRepository)))
	mux.Handle("PUT /api/repositories/", authService.RequireSession(http.HandlerFunc(apiService.UpdateRepository)))
	mux.Handle("DELETE /api/repositories/", authService.RequireSession(http.HandlerFunc(apiService.DeleteRepository)))
	mux.Handle("POST /api/repositories/{id}/sync", authService.RequireSession(http.HandlerFunc(apiService.SyncRepository)))
	mux.Handle("POST /api/sync", authService.RequireSession(http.HandlerFunc(apiService.SyncAll)))
	mux.Handle("GET /api/repositories/{id}/activities", authService.RequireSession(http.HandlerFunc(apiService.RepositoryActivities)))
	mux.Handle("GET /api/activities/{id}", authService.RequireSession(http.HandlerFunc(apiService.GetActivity)))
	mux.Handle("GET /api/activities/{id}/log", authService.RequireSession(http.HandlerFunc(apiService.ActivityLog)))
	mux.Handle("GET /api/events", authService.RequireSession(http.HandlerFunc(apiService.Events)))
	mux.Handle("GET /api/activities/{id}/stream", authService.RequireSession(http.HandlerFunc(apiService.ActivityStream)))
	mux.Handle("POST /hooks/{repository_id}", webhookHandler)
	mux.Handle("/", webui.Handler())
	protection, err := auth.NewCSRFProtection(cfg.Server.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure cross-origin protection: %w", err)
	}
	return protection.Handler(mux), nil
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
