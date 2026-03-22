package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort             string
	DatabaseURL            string
	RedisURL               string
	ReservationTTL         time.Duration
	MaxSeatsPerReservation int
}

func Load() Config {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8085"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Matches docker-compose published port (avoids clashing with a local Postgres on 5432).
		databaseURL = "postgres://postgres:postgres@localhost:5433/ticketbooking?sslmode=disable"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	ttlMinutes := 10
	if v := os.Getenv("RESERVATION_TTL_MINUTES"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			ttlMinutes = parsed
		}
	}

	// Upper bound per reserve request (abuse guard). Not venue capacity — that still wins in DB.
	// Default 100 is a conservative product default; override with RESERVATION_MAX_SEATS.
	maxSeats := 100
	if v := os.Getenv("RESERVATION_MAX_SEATS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			maxSeats = parsed
		}
	}

	return Config{
		ServerPort:             port,
		DatabaseURL:            databaseURL,
		RedisURL:               redisURL,
		ReservationTTL:         time.Duration(ttlMinutes) * time.Minute,
		MaxSeatsPerReservation: maxSeats,
	}
}
