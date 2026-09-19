# stdlog

Module: `github.com/hinha/auth-sdks/go/stdlog`

Service-grade structured logging for memoo / task-hub / x-engine.  
**Not** the Auth SDK client Strategy (`github.com/hinha/auth-sdks/go/logging`).

## Install

```bash
go get github.com/hinha/auth-sdks/go/stdlog@latest
```

Does **not** pull `ratelimit`. `LoggingAdapter` requires the main Auth SDK module (`github.com/hinha/auth-sdks/go`) so one logger can capture SDK audit events.

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

## Audit (closed schema)

Message is always `"audit event"`. `LogAudit` drops unknown keys. Machine `sa_*`
ids are prefix-redacted. Never attach passwords or tokens.

Loki stream `kind`: `audit` | `access` | `app` | `sdk`.

```go
stdlog.LogAudit(log, stdlog.AuditEvent{
	EventType: stdlog.EventAuthAuthorizeAction,
	Decision:  stdlog.DecisionDeny,
	ActorType: stdlog.ActorUser,
	Action:    "reports:export",
})
```

## Gigapipe Loki sink (optional)

Empty `URL` is a no-op (stdout only). Pushes JSON to `{URL}/loki/api/v1/push`
with HTTP Basic. Fail-open: buffer drop-oldest; 401/403 does not panic.

Env (client, not server `QRYN_*`): `GIGAPIPE_URL`, `GIGAPIPE_USERNAME`,
`GIGAPIPE_PASSWORD`.

```go
log, err = stdlog.WrapLoki(log, stdlog.LokiConfig{
	URL:      os.Getenv("GIGAPIPE_URL"),
	Username: os.Getenv("GIGAPIPE_USERNAME"),
	Password: os.Getenv("GIGAPIPE_PASSWORD"),
	Env:      "prod",
	Service:  "money-tracker",
})
defer log.Sync()

client, err := authsdk.New(base, "money-tracker",
	authsdk.Credentials(key),
	authsdk.WithLogger(stdlog.LoggingAdapter(log)),
)
```

Operator LogQL (View / `GET /loki/api/v1/query_range`):

- `{kind="audit"}`
- `{kind="audit"} | json | decision="deny"`
- `{kind="audit",event_type="auth.login_failed"}`
- `{kind="access"} | json | status >= 500`

```bash
curl -sS -u "$GIGAPIPE_USERNAME:$GIGAPIPE_PASSWORD" -G \
  "$GIGAPIPE_URL/loki/api/v1/query_range" \
  --data-urlencode 'query={kind="audit"}'
```

## Test

```bash
go test ./...
```
