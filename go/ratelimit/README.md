# ratelimit

Module: `github.com/hinha/auth-sdks/go/ratelimit`

Flexible multi-profile HTTP rate limiting (based on [ulule/limiter](https://github.com/ulule/limiter)).  
Standalone module — installing this does **not** pull `stdlog` or the main Auth SDK.

## Install

```bash
go get github.com/hinha/auth-sdks/go/ratelimit@latest
```

Default store is **in-memory**. For Redis:

```bash
go get github.com/hinha/auth-sdks/go/ratelimit/redis@latest
```

Echo middleware: [`ratelimit/echo`](./echo).

## Usage

```go
import "github.com/hinha/auth-sdks/go/ratelimit"

lim, err := ratelimit.New(ratelimit.Config{
	Enabled: true,
	Profiles: map[string]string{
		"api":    "30-M", // 30 per minute (ulule format)
		"ingest": "10-M",
	},
	DefaultProfile:   "api",
	CredentialHeader: "X-API-Key", // else client IP
	SkipFunc:         ratelimit.SkipPrefixes("/health", "/v1/cms"),
	PickProfile: func(r *http.Request) string {
		if r.URL.Path == "/api/v1/tweets" && r.Method == http.MethodPost {
			return "ingest"
		}
		return "api"
	},
	// Store: nil → memory; or redis store from ratelimit/redis
	// FailOpen: ratelimit.BoolPtr(true), // default true on store errors
})

http.Handle("/", ratelimit.Middleware(lim, nil)(handler))
```

On allow/deny, sets `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.  
On 429, also `Retry-After`.

## Related modules

| Module | Purpose |
|---|---|
| [`ratelimit/redis`](./redis) | Redis-backed store (`go-redis`) |
| [`ratelimit/echo`](./echo) | Echo middleware |

## Test

```bash
go test ./...
```
