# obs

Module: `github.com/hinha/auth-sdks/go/obs`

Optional Gigapipe ingest for **metrics**, **traces**, and **profiles**.  
Logs stay in [`go/stdlog`](../stdlog) (`WrapLoki` → `POST /loki/api/v1/push`).

Does **not** import `stdlog` or the Auth SDK client.

## Install

```bash
go get github.com/hinha/auth-sdks/go/obs@v0.2.0
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
`InstallGlobal` (off by default) calls `otel.SetTracerProvider`.

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

`DisableDefaultCollectors` drops the Go runtime, process, and build-info
collectors as an ingest-cost escape hatch. It does **not** drop `target_info`,
which is the service identity rather than a runtime collector.

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
