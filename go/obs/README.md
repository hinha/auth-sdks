# obs

Module: `github.com/hinha/auth-sdks/go/obs`

Optional Gigapipe ingest for **metrics**, **traces**, and **profiles**.  
Logs stay in [`go/stdlog`](../stdlog) (`WrapLoki` → `POST /loki/api/v1/push`).

Does **not** import `stdlog` or the Auth SDK client.

## Install

```bash
go get github.com/hinha/auth-sdks/go/obs@v0.3.0
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

	// Optional, completes the OTel service identity.
	ServiceNamespace: "payments",
	ServiceVersion:   "v1.4.2", // falls back to the version stamped in the binary

	// Optional. Default Statfs path is cwd ("."). Set this to a PVC / data dir
	// when that volume is the disk that matters.
	// FilesystemPaths: []string{"/var/lib/app"},
})
defer n.Shutdown(context.Background())

// Nothing has to be registered for the service to be visible: everything in
// "Metrics emitted without any registration" is remote-written as soon as New
// returns, and pushed again immediately rather than after the first interval.
n.Registerer().MustRegister(myOwnCounter)

ctx, span := n.Tracer("http").Start(ctx, "GET /v1/reports")
defer span.End()
```

Disable one signal with `DisableMetrics`, `DisableTraces`, or `DisableProfiles`.  
`InstallGlobal` (off by default) calls `otel.SetTracerProvider` and sets a W3C
`TraceContext` + `Baggage` text-map propagator.

## Client hops (no Redis / GORM / Echo in this module)

Apps already construct Echo/Gin, go-redis, and GORM. They wrap those clients
here so Grafana can show `incoming HTTP → DB → Redis → outbound HTTP` as one
histogram family plus Tempo child spans. This module does **not** import
`go-redis`, GORM, Echo, Gin, pgx, otelsql, redisotel, or otelhttp.

```go
err := h.Observe(ctx, obs.Hop{
    Component: "redis",
    Operation: "GET",      // command name, never the key
    Peer:      "cache",    // logical dest, never a URL with query
}, func(ctx context.Context) error {
    return rdb.Get(ctx, key).Err()
})
```

Split Before/After hooks (GORM plugin, go-redis `Hook`) use `Start` / `End`.
`End` is idempotent. Nil `Handle` and empty-URL handles do not panic; `fn` still runs.

| Metric | Type | Labels |
|---|---|---|
| `client_request_duration_seconds` | histogram | `component`, `operation`, `peer`, `status` |

Identity labels (`service`, `env`, …) are attached at remote-write time, not on
the collector. `status` is an HTTP code (`200`, `500`, …) for HTTP hops and
`ok` / `error` otherwise. Error **messages**, Redis keys, full SQL, and raw
URLs with ids/query strings are forbidden as label values.

Bounds (empty → `unknown`, then truncate): `component` 32, `operation` 128,
`peer` 64, `status` 16 runes. Distinct hop label tuples are capped at 1024;
further combinations record on `unknown` so a high-cardinality `Operation`
cannot grow `HistogramVec` without bound.

Stdlib HTTP (build the middleware / transport **once** at startup):

```go
e.Use(echo.WrapMiddleware(h.HTTPMiddleware(obs.WithRoute(func(r *http.Request) string {
    if p, ok := r.Context().Value(echoRouteKey).(string); ok && p != "" {
        return p
    }
    return "unmatched"
}))))

http.DefaultTransport = h.WrapTransport(http.DefaultTransport, obs.WithPeer("upstream"))
```

Default inbound operation is `METHOD unmatched`, never `URL.Path`. Outbound
operation is the method only unless `WithPathTemplate` is set. Wrapping an
already-wrapped transport double-counts duration — wrap once.

### App recipes (copy into the service that already has the client library)

**go-redis v9** — implement `redis.Hook` in the app; never put the key in `Operation`:

```go
type obsRedisHook struct{ h *obs.Handle }

func (o obsRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (o obsRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        return o.h.Observe(ctx, obs.Hop{
            Component: "redis",
            Operation: cmd.FullName(),
            Peer:      "cache",
        }, func(ctx context.Context) error { return next(ctx, cmd) })
    }
}
func (o obsRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
    return next
}

rdb.AddHook(obsRedisHook{h: h})
```

**GORM** — register callbacks in the app and pair `Start`/`End`:

```go
ctx, t := h.Start(tx.Statement.Context, obs.Hop{
    Component: "db",
    Operation: "SELECT users", // table + verb, never the interpolated SQL
    Peer:      "postgres",
})
tx.Statement.Context = ctx
// after_query:
t.End(tx.Error)
```

**Echo / Gin** — `echo.WrapMiddleware(h.HTTPMiddleware(...))` (or Gin equivalent).
Store the framework route template on context in the app if `r.Pattern` is empty.

Do not put passwords, tokens, or API keys in metric labels, span attributes, or
profile tags. Label names `password`, `token`, `authorization`, and `api_key`
are dropped on remote-write.

## Metrics emitted without any registration

Remote-write pushes **immediately on start** and then every `MetricsInterval`
(15s default), so a service shows up in Grafana without waiting for traffic, and
a worker that never serves HTTP is no longer invisible.

| Metric | Type | Labels |
|---|---|---|
| `target_info` | gauge = 1 | identity labels + `service_version` |
| `go_*` | mixed | client_golang defaults |
| `go_build_info` | gauge | `path`, `version`, `checksum` |
| `process_*` | mixed | client_golang defaults |
| `process_io_*` | counter | Linux `/proc/self/io` only; omitted elsewhere |
| `process_filesystem_avail_bytes` | gauge | `path` — Statfs `Bavail * Bsize` (Linux/macOS) |
| `process_filesystem_size_bytes` | gauge | `path` — Statfs `Blocks * Bsize` (Linux/macOS) |

`process_io_read_bytes_total` / `process_io_write_bytes_total` are kernel
`read_bytes` / `write_bytes` (storage). `process_io_rchar_bytes_total` /
`process_io_wchar_bytes_total` include page cache. Grafana: `rate(...[5m])`,
not a latency histogram.

Filesystem gauges default to cwd (`path="."`). They are **not** host mount
tables (`node_filesystem_*`). Used fraction:

`1 - process_filesystem_avail_bytes / process_filesystem_size_bytes`

Uptime is `time() - process_start_time_seconds`, and `target_info == 1` is the
presence signal — so the per-service `*_process_up` gauge that used to be needed
to appear in Grafana is no longer required.

Metric metadata (HELP, type, unit) travels with every push, so Grafana Cloud
shows descriptions and formats units instead of bare numbers.

### Identity labels

Every series carries the service identity. The legacy pair is kept and the OTel
semconv pair is added alongside it, so a dashboard filtering either spelling
matches.

| Label | Source |
|---|---|
| `service` | `Config.Service` (omitted when empty) |
| `service_name` | `Config.Service`, or `auth-sdks` when empty; always identical to `service` when that is set |
| `env` | `Config.Env` (omitted when empty) |
| `deployment_environment` | `Config.Env` |
| `deployment_environment_name` | `Config.Env` — semconv v1.37.0 renamed the attribute, and an OTel Collector normalises it to this spelling |
| `service_namespace` | `Config.ServiceNamespace` |
| `service_version` | `target_info` **only** — it changes on every deploy, so putting it on every series would churn the entire series set per release |

The same identity is applied to profile tags, so metrics and profiles cannot
disagree about which service they belong to.

### Your own collectors always win

Defaults live in a private registry that is merged at gather time with the
application registry **first**. On any name clash the application's series wins
and the default is dropped, so double-emission cannot happen. That means:

- `Registerer().MustRegister(collectors.NewGoCollector())` keeps working.
- A Go collector registered with different options keeps its families, and the
  defaults fill the gaps.
- If you added a `*_process_up` gauge to work around the old behaviour, you can
  delete it — but nothing breaks if you leave it.

`DisableDefaultCollectors` drops the Go runtime, process, process I/O,
filesystem, and build-info collectors as an ingest-cost escape hatch. It does
**not** drop `target_info`, which is the service identity rather than a runtime
collector.

### Inspect what is actually being sent

```go
mux.Handle("/metrics", n.Handler())
```

`Handler()` serves the same merged registry that is remote-written — the fastest
way to answer "what is this process sending?" when a dashboard panel is empty.

### Failure reporting

Push failures are fail-open and reported **once per distinct condition**, not
once per process. A one-shot warning would be wrong here: the push now runs at
startup, when DNS may not be ready, so the first failure would consume the only
warning and silence every later, real one. A tracer that fails to construct is
also fail-open and no longer takes metrics and profiles down with it.

## Loki (not this module)

`stdlog.WrapLoki` is the log ingest path. Loki query/tail is operator-side.

## Test

```bash
go test ./...
```
