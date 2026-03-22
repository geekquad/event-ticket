package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"ticket/internal/config"
)

// loadDotEnv loads the first .env found walking up from the working directory
// (so `go run .` from cmd/server still finds repo-root .env).
func loadDotEnv() {
	dir, err := os.Getwd()
	if err != nil {
		slog.Warn("getwd", "error", err)
		return
	}
	for {
		p := filepath.Join(dir, ".env")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if err := godotenv.Load(p); err != nil {
				slog.Warn("load .env", "path", p, "error", err)
			} else {
				slog.Info("loaded .env", "path", p)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// In Docker / most platforms, env is injected; a .env file is optional.
			if _, err := os.Stat("/.dockerenv"); err != nil {
				slog.Info("no .env file on disk; using environment variables and defaults")
			}
			return
		}
		dir = parent
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	loadDotEnv()
	cfg := config.Load()

	container, err := NewContainer(cfg)
	if err != nil {
		slog.Error("failed to initialize", "error", err)
		os.Exit(1)
	}
	defer container.Close()

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           container.Router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down server", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
