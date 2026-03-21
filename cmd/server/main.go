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

	"ticket/internal/adapters/postgres"
	redisadapter "ticket/internal/adapters/redis"
	"ticket/internal/config"
	"ticket/internal/service"
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
			slog.Warn("no .env file found (using defaults from env or config)")
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

	db, err := postgres.Connect(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("connected to database")

	redisClient, err := redisadapter.Connect(cfg.RedisURL)
	if err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	slog.Info("connected to redis")

	// Repositories
	eventRepo := postgres.NewEventRepo(db)
	bookingRepo := postgres.NewBookingRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	userRepo := postgres.NewUserRepo(db)
	transactor := postgres.NewTransactor(db)
	lockManager := redisadapter.NewLockManager(redisClient)

	// Services
	eventService := service.NewEventService(eventRepo)
	bookingService := service.NewBookingService(bookingRepo, eventRepo, auditRepo, lockManager, transactor, cfg.ReservationTTL)
	userService := service.NewUserService(userRepo)

	router := NewRouter(eventService, bookingService, userService)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           router,
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
