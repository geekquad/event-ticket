package main

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"ticket/internal/adapters/postgres"
	redisadapter "ticket/internal/adapters/redis"
	"ticket/internal/config"
	"ticket/internal/service"
)

// Container holds all application dependencies.
type Container struct {
	DB     *sql.DB
	Router *gin.Engine
	redis  *redis.Client
}

// NewContainer initializes database, Redis, repositories, services, and router.
func NewContainer(cfg config.Config) (*Container, error) {
	db, err := postgres.Connect(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	redisClient, err := redisadapter.Connect(cfg.RedisURL)
	if err != nil {
		db.Close()
		return nil, err
	}

	eventRepo := postgres.NewEventRepo(db)
	bookingRepo := postgres.NewBookingRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	userRepo := postgres.NewUserRepo(db)
	transactor := postgres.NewTransactor(db)
	lockManager := redisadapter.NewLockManager(redisClient)

	eventService := service.NewEventService(eventRepo)
	bookingService := service.NewBookingService(bookingRepo, eventRepo, auditRepo, lockManager, transactor, cfg.ReservationTTL)
	userService := service.NewUserService(userRepo)

	router := NewRouter(eventService, bookingService, userService, resolveFrontendDir())

	return &Container{
		DB:     db,
		Router: router,
		redis:  redisClient,
	}, nil
}

// Close releases database and Redis connections.
func (c *Container) Close() {
	if c.redis != nil {
		_ = c.redis.Close()
	}
	if c.DB != nil {
		_ = c.DB.Close()
	}
}
