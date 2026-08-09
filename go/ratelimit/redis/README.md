# ratelimit/redis

Module: `github.com/hinha/auth-sdks/go/ratelimit/redis`

Optional Redis store for [`ratelimit`](../). Separate module so memory-only users do not depend on `go-redis`.

## Install

```bash
go get github.com/hinha/auth-sdks/go/ratelimit/redis@latest
```

## Usage

Prefer sharing an existing client:

```go
import (
	"github.com/hinha/auth-sdks/go/ratelimit"
	redisstore "github.com/hinha/auth-sdks/go/ratelimit/redis"
	"github.com/redis/go-redis/v9"
)

client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
store, err := redisstore.NewStore(client, redisstore.Options{Prefix: "x-engine-rl"})

lim, err := ratelimit.New(ratelimit.Config{
	Enabled:  true,
	Profiles: map[string]string{"default": "30-M"},
	Store:    store,
})
```

Or dial by address (caller must `Close` the returned client):

```go
store, client, err := redisstore.NewStoreFromAddr("127.0.0.1:6379", redisstore.Options{Prefix: "svc-rl"})
defer client.Close()
```

`NewStore` accepts `redis.UniversalClient` (single, cluster, failover). Lua scripts are preloaded via ulule's Redis driver.

## Test

Uses [miniredis](https://github.com/alicebob/miniredis) (no real Redis required):

```bash
go test ./...
```
