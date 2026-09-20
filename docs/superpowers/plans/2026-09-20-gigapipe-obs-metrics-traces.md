# auth-sdks Gigapipe Prometheus Tempo Pyroscope

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship metrics, traces, and profiles from services to Gigapipe ingest, next to existing Loki `WrapLoki`.

**Architecture:** New nested module `github.com/hinha/auth-sdks/go/obs`. Loki query stays operator-side. No Influx/Elastic/Datadog/Zipkin.

**Tech Stack:** Go 1.25.7, prometheus/client_golang, OTLP HTTP (`otlptracehttp`), grafana/pyroscope-go, snappy protobuf remote-write.

**Spec:** [gigapipe.com/docs/api](https://gigapipe.com/docs/api)

## Global Constraints

- Empty `GIGAPIPE_URL` / `Config.URL` is a no-op.
- HTTP Basic; `User-Agent: auth-sdks-obs/1`.
- Fail-open: never panic on 401/403 or transport errors.
- Do not import `stdlog` or `github.com/hinha/auth-sdks/go`.
- Coverage ≥80% via `.github/scripts/go-test-cover.sh`.
- Branch: `feat/gigapipe-obs-metrics-traces` from `origin/main`.
- After PR CI green: merge, then tag `go/v0.2.2` and `go/obs/v0.1.0`.
