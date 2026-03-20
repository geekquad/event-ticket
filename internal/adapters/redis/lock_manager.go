package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"ticket/internal/ports"
)

const luaRelease = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end`

type lockManager struct {
	client *redis.Client
}

func NewLockManager(client *redis.Client) ports.LockManager {
	return &lockManager{client: client}
}

func (l *lockManager) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	ok, err := l.client.SetNX(ctx, key, value, ttl).Result()
	return ok, err
}

func (l *lockManager) Release(ctx context.Context, key, value string) error {
	result, err := l.client.Eval(ctx, luaRelease, []string{key}, value).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	_ = result // 0 means already expired -- not an error
	return nil
}

func (l *lockManager) GetOwner(ctx context.Context, key string) (string, error) {
	val, err := l.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return val, err
}
