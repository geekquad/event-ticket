package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerPort     string
	DatabaseURL    string
	RedisURL       string
	ReservationTTL time.Duration
}

func Load() Config {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	ttlMinutes := 10
	if v := os.Getenv("RESERVATION_TTL_MINUTES"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			ttlMinutes = parsed
		}
	}

	return Config{
		ServerPort:     port,
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisURL:       os.Getenv("REDIS_URL"),
		ReservationTTL: time.Duration(ttlMinutes) * time.Minute,
	}
}
