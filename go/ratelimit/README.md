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

### Per-endpoint limits (dynamic `:param` paths)

Avoid exact `r.URL.Path == "..."` for routes with IDs — use `RouteTable` + `MatchPath`:

```go
table := ratelimit.RouteTable{
	// more specific first
	{Method: "GET", Pattern: "/api/v1/jobs/:id/events", Profile: "job_events"},
	{Method: "GET", Pattern: "/api/v1/jobs/:id", Profile: "job_detail"},
	{Method: "GET", Pattern: "/api/v1/search", Profile: "search"},
	{Method: "POST", Pattern: "/api/v1/tweets", Profile: "ingest"},
	{Pattern: "/api/v1/webhooks/*", Profile: "webhooks"}, // any method, rest of path
}

lim, err := ratelimit.New(ratelimit.Config{
	Enabled: true,
	Profiles: map[string]string{
		"default":    "60-M",
		"search":     "30-M",
		"ingest":     "10-M",
		"job_detail": "20-M",
		"job_events": "40-M",
		"webhooks":   "100-M",
	},
	DefaultProfile:   "default", // unmatched routes
	PickProfile:      table.Pick,
	CredentialHeader: "X-API-Key",
	SkipFunc:         ratelimit.SkipPrefixes("/health", "/internal", "/cms"),
})
```

Patterns: `:id` / `{id}` = one segment; trailing `/*` = one or more remaining segments.

### Basic

```go
import "github.com/hinha/auth-sdks/go/ratelimit"

lim, err := ratelimit.New(ratelimit.Config{
	Enabled: true,
	Profiles: map[string]string{
		"api":    "30-M",
		"ingest": "10-M",
	},
	DefaultProfile:   "api",
	CredentialHeader: "X-API-Key",
	SkipFunc:         ratelimit.SkipPrefixes("/health", "/v1/cms"),
	PickProfile:      table.Pick, // or a custom func
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
