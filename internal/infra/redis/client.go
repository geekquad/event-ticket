package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/redis/go-redis/v9"
)

func Connect(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "EOF") {
			return nil, fmt.Errorf("ping redis at %s: %w (for TLS servers use rediss:// in REDIS_URL, not redis://)", opts.Addr, err)
		}
		return nil, fmt.Errorf("ping redis at %s: %w", opts.Addr, err)
	}
	return client, nil
}
