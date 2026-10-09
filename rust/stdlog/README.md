# stdlog

Crate: `auth-sdks-stdlog` (library name `stdlog`). Padanan Rust untuk
[`go/stdlog`](../../go/stdlog/README.md), in particular `WrapLoki`.

Gigapipe Loki log sink. Records are pushed as JSON to `POST {url}/loki/api/v1/push` with HTTP
Basic auth, which matches the endpoint in [gigapipe.com/docs/api](https://gigapipe.com/docs/api).

## Install

```toml
[dependencies]
auth-sdks-stdlog = { git = "https://github.com/hinha/auth-sdks", tag = "rust/stdlog/v0.1.0" }
```

## Use

```rust
use std::time::Duration;
use stdlog::{Level, LogExporter, LogRecord, LokiConfig, LokiSink};

let sink = LokiSink::new(LokiConfig {
    url: "https://gigapipe.example".into(),
    username: "user".into(),
    password: "pass".into(),
    env: "prod".into(),
    service: "my-service".into(),
    flush_interval: Duration::from_secs(5),
    secret_values: vec!["sk-live-…".into()], // scrubbed from every line
    ..LokiConfig::default()
});

sink.export(&LogRecord::new(Level::Info, "started").with_field("port", 5456));

// Flush the buffered tail before exit. Dropping the sink does the same.
sink.shutdown();
```

`LokiSink` is a `LogExporter` and is shared as `Arc<LokiSink>`. `export` never blocks on the
network. One worker thread owns the blocking HTTP client and batches the pushes.

## Behavior (matches `go/stdlog/loki.go`)

- An empty `url` disables the sink. Records are dropped, and no thread starts.
- Batches go out on `batch_size` (default 50), on `flush_interval` (off by default), and on
  `shutdown`. A batch never exceeds `batch_size`.
- The buffer holds `max_buffer` records (default 1000). Beyond that the oldest is dropped.
- Stream labels: `kind`, `level`, `env`, `service`. Audit records add `event_type` and `decision`.
  Labels stay low-cardinality, so IDs are never labels.
- `kind` comes from an explicit `kind` field, then from the message (`audit event`,
  `http request completed`, `http_request_*`), and defaults to `app`.
- Secret keys are dropped (`password`, `token`, `authorization`, `api_key`, …). Configured secret
  values are replaced with `[REDACTED]` inside string fields.
- Fail-open: a transport error or a rejected push never reaches the caller. A 401/403 or a
  transport error is reported once per sink through `on_warn` (stderr by default).

## Test

```bash
cd rust
cargo test -p auth-sdks-stdlog --locked
```
