package redisstore

import (
	"fmt"

	"github.com/hinha/auth-sdks/go/ratelimit"
	"github.com/redis/go-redis/v9"
	"github.com/ulule/limiter/v3"
	ululeredis "github.com/ulule/limiter/v3/drivers/store/redis"
)

// Options configures the Redis-backed rate-limit store.
type Options struct {
	// Prefix is prepended to Redis keys (default: ulule DefaultPrefix "limiter").
	Prefix string
}

// NewStore builds a ratelimit.Store from a go-redis UniversalClient (single, cluster, or failover).
// The client must already be configured and reachable; NewStore preloads Lua scripts.
func NewStore(client redis.UniversalClient, opts ...Options) (ratelimit.Store, error) {
	if client == nil {
		return nil, fmt.Errorf("ratelimit/redis: client is nil")
	}
	opt := Options{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	storeOpts := limiter.StoreOptions{
		Prefix:          limiter.DefaultPrefix,
		CleanUpInterval: limiter.DefaultCleanUpInterval,
		MaxRetry:        limiter.DefaultMaxRetry,
	}
	if opt.Prefix != "" {
		storeOpts.Prefix = opt.Prefix
	}
	store, err := ululeredis.NewStoreWithOptions(client, storeOpts)
	if err != nil {
		return nil, fmt.Errorf("ratelimit/redis: %w", err)
	}
	return store, nil
}

// NewStoreFromAddr dials Redis with go-redis defaults and returns a store.
// Prefer NewStore when the service already owns a shared redis client.
func NewStoreFromAddr(addr string, opts ...Options) (ratelimit.Store, *redis.Client, error) {
	if addr == "" {
		return nil, nil, fmt.Errorf("ratelimit/redis: addr is required")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	store, err := NewStore(client, opts...)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return store, client, nil
}
