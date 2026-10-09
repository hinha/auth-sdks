# obs

Crate: `auth-sdks-obs` (library name `obs`). Padanan Rust untuk [`go/obs`](../../go/obs/README.md).

Gigapipe metrics, traces, and profiles for Rust services. The same endpoints serve all three
signals, and logs live in the sibling crate [`stdlog`](../stdlog/README.md), as `go/stdlog` does
for Go.

| Signal | Endpoint | Notes |
|---|---|---|
| Metrics | `POST /api/v1/prom/remote/write` | Snappy protobuf remote write 1.0, HTTP Basic |
| Traces | `POST /v1/traces` | OTLP/HTTP, protobuf, batch export |
| Profiles | `POST /ingest` | Pyroscope push API, pprof CPU profile (`pyroscope` 0.5) |

Endpoints are from [gigapipe.com/docs/api](https://gigapipe.com/docs/api).

## Install

Git dependency, pinned to the crate's release tag (the same pattern as the Go `go/obs/v…` tags):

```toml
[dependencies]
auth-sdks-obs = { git = "https://github.com/hinha/auth-sdks", tag = "rust/obs/v0.1.0" }
```

Requires Rust 1.96 (see `rust/rust-toolchain.toml`).

## Use

```rust
use std::sync::Arc;
use obs::{Config, Handle, Hop, ServerLayer};

let handle = Arc::new(Handle::new(Config {
    url: std::env::var("GIGAPIPE_URL").unwrap_or_default(),
    username: std::env::var("GIGAPIPE_USERNAME").unwrap_or_default(),
    password: std::env::var("GIGAPIPE_PASSWORD").unwrap_or_default(),
    service: "my-service".into(),
    env: std::env::var("APP_ENV").unwrap_or_default(),
    service_version: env!("CARGO_PKG_VERSION").into(),
    ..Config::default()
}));

// Inbound: one http_server hop per request (wrap an axum/tower service).
let app = axum::Router::new().layer(ServerLayer::new(Arc::clone(&handle)));

// Outbound: one http_client hop per upstream call, peer = downstream service name.
let response = obs::observe_send(&handle, "auth-service", "POST", client.post(url).send()).await?;

// Your own collectors take precedence over defaults with the same name.
handle.registry().register(Box::new(my_counter))?;

// Push the last metrics and flush spans and profiles before exit.
handle.shutdown();
```

An empty `url` makes the handle local-only. Nothing is sent, and the registry still works.

## Metrics emitted without any registration

| Metric | Type | Labels |
|---|---|---|
| `target_info` | gauge = 1 | identity labels + `service_version` |
| `process_*` | mixed | Linux only, from the prometheus process collector |
| `process_io_*` | counter | Linux only, from `/proc/self/io` |
| `process_filesystem_avail_bytes` / `_size_bytes` | gauge | `path` (statvfs, Unix) |
| `client_request_duration_seconds` | histogram | `component`, `operation`, `peer`, `status` |

Rust has no `go_*` runtime metrics. That is the one intentional divergence from `go/obs`.

Identity labels, the 1024-tuple cap on hop series, the label bounds (component 32, operation 128,
peer 64, status 16 characters), and the dropped secret labels (`password`, `token`,
`authorization`, `api_key`) match `go/obs`.

## Fail-open

Every push, export, and profile upload is best-effort. A failure is reported once per condition
through `Config::on_warn` (stderr by default) and never panics or blocks the caller.

## Test

```bash
cd rust
cargo test -p auth-sdks-obs --locked
cargo llvm-cov -p auth-sdks-obs --locked --summary-only
```

The tests run against an in-process capture server (`src/test_server.rs`), which plays the part of
Go's `httptest`. They never reach a live Gigapipe.
