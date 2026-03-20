package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"ticket/internal/adapters/postgres"
	redisadapter "ticket/internal/adapters/redis"
	"ticket/internal/application"
	"ticket/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found", "error", err)
	}

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
	ticketRepo := postgres.NewTicketRepo(db)
	bookingRepo := postgres.NewBookingRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	userRepo := postgres.NewUserRepo(db)
	transactor := postgres.NewTransactor(db)
	lockManager := redisadapter.NewLockManager(redisClient)

	// Services
	eventService := application.NewEventService(eventRepo, ticketRepo)
	bookingService := application.NewBookingService(bookingRepo, ticketRepo, auditRepo, lockManager, transactor, cfg.ReservationTTL)
	userService := application.NewUserService(userRepo)

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
