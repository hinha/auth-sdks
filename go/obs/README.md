# obs

Module: `github.com/hinha/auth-sdks/go/obs`

Optional Gigapipe ingest for **metrics**, **traces**, and **profiles**.  
Logs stay in [`go/stdlog`](../stdlog) (`WrapLoki` → `POST /loki/api/v1/push`).

Does **not** import `stdlog` or the Auth SDK client.

## Install

```bash
go get github.com/hinha/auth-sdks/go/obs@v0.1.0
```

## Gigapipe ingest (this module)

Official API: [gigapipe.com/docs/api](https://gigapipe.com/docs/api).

| Signal | Endpoint | Notes |
|---|---|---|
| Metrics | `POST /api/v1/prom/remote/write` | Prometheus remote-write 1.0 (snappy protobuf). Query APIs are Grafana/View. |
| Traces | `POST /v1/traces` | OTLP HTTP. Zipkin / `/tempo/api/push` not used. |
| Profiles | `POST /ingest` | `grafana/pyroscope-go` against the Gigapipe origin. |

Empty `URL` is a no-op (local registry + noop tracer). Fail-open: 401/403 and network errors do not panic.

Env (client, not server `QRYN_*`): `GIGAPIPE_URL`, `GIGAPIPE_USERNAME`, `GIGAPIPE_PASSWORD`.

```go
n, err := obs.New(obs.Config{
	URL:      os.Getenv("GIGAPIPE_URL"),
	Username: os.Getenv("GIGAPIPE_USERNAME"),
	Password: os.Getenv("GIGAPIPE_PASSWORD"),
	Service:  "money-tracker",
	Env:      os.Getenv("APP_ENV"),
})
defer n.Shutdown(context.Background())

httpDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_request_duration_seconds",
	Help: "HTTP request duration",
}, []string{"method", "route"})
n.Registerer().MustRegister(httpDuration)

ctx, span := n.Tracer("http").Start(ctx, "GET /v1/reports")
defer span.End()
```

Disable one signal with `DisableMetrics`, `DisableTraces`, or `DisableProfiles`.  
`InstallGlobal` (off by default) calls `otel.SetTracerProvider`.

Do not put passwords, tokens, or API keys in metric labels, span attributes, or
profile tags. Label names `password`, `token`, `authorization`, and `api_key`
are dropped on remote-write.

## Loki (not this module)

`stdlog.WrapLoki` is the log ingest path. Loki query/tail is operator-side.

## Test

```bash
go test ./...
```
