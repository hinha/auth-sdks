# stdlog

Module: `github.com/hinha/auth-sdks/go/stdlog`

Service-grade structured logging for memoo / task-hub / x-engine.  
**Not** the Auth SDK client Strategy (`github.com/hinha/auth-sdks/go/logging`).

## Install

```bash
go get github.com/hinha/auth-sdks/go/stdlog@latest
```

Does **not** pull `ratelimit` or the main Auth SDK module.

## Backends

| Factory | Backend |
|---|---|
| `NewZap(cfg)` | [uber/zap](https://github.com/uber-go/zap) (primary) |
| `NewZerolog(cfg)` | [rs/zerolog](https://github.com/rs/zerolog) |
| `NewSlog(cfg)` | `log/slog` |

```go
log, err := stdlog.NewZap(stdlog.Config{
	Service: "x-engine", // required
	Level:   "info",     // debug|info|warn|error
	Format:  "json",     // json|console (pretty → console)
})
log.Named("worker").Info("started", stdlog.String("id", "1"))
```

## Access log (strict fields)

Message is always `"http request completed"`. Level: `>=500` error, `>=400` warn, else info.

Canonical keys only (unknown keys dropped by the access-log builder):  
`service`, `component`, `request_id`, `method`, `path`, `route`, `query`, `status`, `duration_ms`, `response_size`, `remote_addr`, `user_agent`, plus optional body/header/user fields.

```go
http.Handle("/", stdlog.Middleware(log, stdlog.AccessLogConfig{
	LogRequestHeaders: true,
})(handler))
```

Echo adapter: [`stdlog/echo`](./echo).

## Test

```bash
go test ./...
```
