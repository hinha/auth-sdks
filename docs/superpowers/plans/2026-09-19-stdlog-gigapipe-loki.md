# auth-sdks stdlog + audit → Gigapipe

> Implementation plan executed 2026-09-19. Notion: https://app.notion.com/p/3e08d63e407a819e985bdffed16127af

**Goal:** Ship operational logs and a closed audit trail from `auth-sdks` to Gigapipe OSS via Loki JSON push.

Official API: https://gigapipe.com/docs/api

## SDK writes

- `POST /loki/api/v1/push` (JSON `streams[].stream` + `values`). HTTP Basic. `User-Agent: auth-sdks-stdlog/1`.
- Client env: `GIGAPIPE_URL`, `GIGAPIPE_USERNAME`, `GIGAPIPE_PASSWORD` (not server `QRYN_*`).

## Operator reads (not called by SDK)

- `GET /loki/api/v1/query`, `query_range`, `label`, `labels`, `label/{name}/values`, `series`, `tail`
- `GET /ready`

## Out of this slice

OTLP, Tempo, Prometheus remote write, Influx, Elastic, Datadog, Pyroscope.

## Two audit channels

1. NATS JetStream `platform.entitlements.audit.v1.raised` — Auth Service contract (unchanged).
2. Loki `kind=audit` — operator View. Fail-open.

## Closed audit

Message always `"audit event"`. Labels: `service`, `env`, `kind`, `level`, plus `event_type`/`decision` for audit. No tokens, passwords, or full `sa_*` keys.

`event_type`: `auth.login`, `auth.login_failed`, `auth.refresh`, `auth.logout`, `auth.authorize_action`, `auth.authorize_endpoint`, `auth.api_key_verify`, `entitlement.fetched`, `entitlement.feature_checked`, `entitlement.quota_checked`.

## Wiring

```go
log, _ := stdlog.NewZap(stdlog.Config{Service: "money-tracker", Level: "info", Format: "json"})
log, _ = stdlog.WrapLoki(log, stdlog.LokiConfig{URL: os.Getenv("GIGAPIPE_URL"), Username: os.Getenv("GIGAPIPE_USERNAME"), Password: os.Getenv("GIGAPIPE_PASSWORD"), Env: os.Getenv("APP_ENV"), Service: "money-tracker"})
defer log.Sync()
client, _ := authsdk.New(base, "money-tracker", authsdk.Credentials(key), authsdk.WithLogger(stdlog.LoggingAdapter(log)))
```

LogQL: `{kind="audit"} | json | decision="deny"`
