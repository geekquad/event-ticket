package ports

import (
	"context"
	"time"
)

type LockManager interface {
	Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key, value string) error
	GetOwner(ctx context.Context, key string) (string, error)
}
