# Rust Gigapipe Observability Library and oc-cc Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Rust counterpart of `go/obs` and `go/stdlog` under `rust/` (metrics, traces, profiles, and Loki logs to Gigapipe), and integrate it into `oc-cc` so the proxy reports its own metrics, requests, upstream calls, and logs.

**Architecture:** One Cargo workspace in `rust/` holds two library crates. `auth-sdks-obs` (lib `obs`) covers the Gigapipe metrics, traces, and profiles endpoints. `auth-sdks-stdlog` (lib `stdlog`) covers the Loki push sink, and it does not depend on `obs`, as `go/stdlog` does not depend on `go/obs`. Consumers pin each crate to its own release tag (`rust/obs/vX.Y.Z`, `rust/stdlog/vX.Y.Z`). `oc-cc` wires both in at startup behind one config block. Every path is fail-open, and an empty URL makes the feature a no-op.

**Tech Stack:** Rust 1.96 (edition 2024), `prometheus` 0.14, `prost` 0.14, `snap` 1, `reqwest` 0.12 (blocking and async), `opentelemetry` 0.33 with `opentelemetry-otlp` 0.33 (http-proto), `pyroscope` 0.5 with `pyroscope_pprofrs` 0.2, `tower` 0.5, `procfs` 0.18 and `nix` 0.31 on Linux and Unix, `cargo-llvm-cov` for coverage.

## Global Constraints

- Gigapipe endpoints, from gigapipe.com/docs/api: metrics `POST /api/v1/prom/remote/write`, traces `POST /v1/traces`, profiles `POST /ingest`, logs `POST /loki/api/v1/push`.
- Empty URL is a no-op. Every push, export, and upload is fail-open: a failure is reported once per condition and never panics or blocks the caller.
- Coverage gate: at least 80% lines per crate, the same floor as `.github/scripts/go-test-cover.sh` (`MIN_COVER=80`).
- Lints: `cargo fmt --all -- --check` and `cargo clippy --workspace --all-targets --locked -- -D warnings`.
- Tests live in-module (`#[cfg(test)] mod tests`) and use an in-process capture server, not a live Gigapipe.
- The Rust toolchain is pinned to 1.96.0 by `rust/rust-toolchain.toml`.

## File Structure

- Create: `rust/Cargo.toml`, `rust/rust-toolchain.toml`, `rust/Cargo.lock`. Workspace root with `members = ["obs", "stdlog"]`.
- Create: `rust/obs/Cargo.toml`, `rust/obs/README.md`, `rust/obs/src/{lib,config,identity,warn,prompb,remote_write,metrics,collectors,hop,http,traces,profiles,handle,test_server}.rs`. Each file has one responsibility: config and identity are separate, the remote-write pipeline is split from the gather and conversion logic, and hop instrumentation is separate from its transports (`http.rs`, `traces.rs`, `profiles.rs`).
- Create: `rust/stdlog/Cargo.toml`, `rust/stdlog/README.md`, `rust/stdlog/src/{lib,config,record,kind,redact,loki,test_server}.rs`.
- Create: `.github/workflows/rust-unit-tests.yml`.
- Modify: `README.md` (language table row for Rust), `.gitignore` (`rust/target/`).
- Create (oc-cc, separate repository): `src/observability/mod.rs`. Modify `Cargo.toml`, `src/config/config.rs`, `src/logging/mod.rs`, `src/provider/base.rs`, `src/server/mod.rs`, `src/cli/serve.rs`, `src/lib.rs`.

## Interfaces

- `obs::Config` (fields `url`, `username`, `password`, `service`, `env`, `service_namespace`, `service_version`, `timeout`, `metrics_interval`, `disable_metrics|traces|profiles|default_collectors`, `filesystem_paths`, `install_global`, `on_warn`).
- `obs::Handle::new(Config) -> Handle`, `registry()`, `tracer()`, `identity()`, `gather()`, `metrics_text()`, `shutdown(&self)`.
- `obs::observe<T,E,Fut>(&Handle, Hop, Fut)`, `obs::observe_send(&Handle, peer, method, Fut)`, `obs::execute(&Handle, peer, &Client, Request)`, `obs::ServerLayer::new(Arc<Handle>).with_route(..)`.
- `stdlog::LokiConfig` (fields `url`, `username`, `password`, `env`, `service`, `timeout`, `batch_size`, `flush_interval`, `max_buffer`, `secret_values`, `on_warn`), `stdlog::LokiSink::new(LokiConfig) -> Arc<LokiSink>`, `shutdown(&self)`, and `impl LogExporter` on `LokiSink`.

## Tasks

### Task 1: Verify dependencies and the Pyroscope endpoint

**Files:** `rust/obs/Cargo.toml`

- [x] **Step 1:** Look up the current release of each crate with `cargo info`, and read the API from the cached sources.
- [x] **Step 2:** Confirm the Pyroscope push path. `pyroscope` 2.1.1 posts to `/push.v1.PusherService/Push`, which Gigapipe does not expose. `pyroscope` 0.5.8 posts to `{url}/ingest`, which matches the documentation. Pin `pyroscope = "0.5"` and `pyroscope_pprofrs = "0.2"`.

### Task 2: Scaffold the workspace and CI

**Files:** `rust/Cargo.toml`, `rust/rust-toolchain.toml`, `.github/workflows/rust-unit-tests.yml`, `.gitignore`, `README.md`

- [x] **Step 1:** Create the workspace with resolver 2, edition 2024, and `publish = false` on every crate, so nothing reaches crates.io by accident.
- [x] **Step 2:** Add the workflow. It runs fmt and clippy in one job and a coverage gate per crate in a matrix.
- [x] **Step 3:** Ignore `rust/target/`, and add the Rust row to the root language table.

### Task 3: Implement `stdlog` (Loki sink)

**Files:** `rust/stdlog/src/*.rs`

- [x] **Step 1:** Write the record and kind model: `LogRecord`, `Level`, `LogExporter`, and `kind_from` (matching `kindFrom`).
- [x] **Step 2:** Write redaction (`skip_secret_key`, `redact_text`), with the same key list as `skipSecretKey`.
- [x] **Step 3:** Write `LokiSink`. One worker thread owns the blocking client. `export` only appends to a bounded buffer. A batch never exceeds `batch_size`, and the buffer drops the oldest record past `max_buffer`.
- [x] **Step 4:** Test against the capture server. Cover the body shape, Basic auth, batching, the buffer cap, 401 and 403 reported once, transport errors and timeouts, shutdown flush, the drop-time flush, and the empty URL.

### Task 4: Implement `obs` (metrics, traces, profiles)

**Files:** `rust/obs/src/*.rs`

- [x] **Step 1:** Config and identity. `Identity::resolve` keeps `service_name` equal to `service` when set, and `service_version` goes only on `target_info`.
- [x] **Step 2:** Remote write. Hand-written prost messages (no `protoc`), snappy compression, and a worker that pushes on start, on each interval, and once more on stop.
- [x] **Step 3:** Collectors and merge. The application registry wins over default families. Process metrics come from the prometheus collector on Linux, `process_io_*` from `/proc/self/io`, and `process_filesystem_*` from statvfs.
- [x] **Step 4:** Hop API. Labels are bounded, distinct series are capped at 1024, and the span carries `peer.service`, `db.system`, and HTTP attributes as in `hop_semconv.go`.
- [x] **Step 5:** Traces. OTLP/HTTP to `/v1/traces` with batch export and Basic auth. A zero timeout falls back to the default, since reqwest treats zero as an immediate timeout.
- [x] **Step 6:** Profiles. Pyroscope 0.5 with the pprof backend at 100 Hz, using the origin URL, because the agent appends `/ingest` itself.
- [x] **Step 7:** Test everything. 84 tests pass, including the in-process capture-server tests for each transport.

### Task 5: Integrate into oc-cc

**Repository:** `oc-cc`, branch `feat/rust-obs-integration`

- [x] **Step 1:** Add the `observability` config block. It is Rust-only, skipped when unset, and filled from `GIGAPIPE_*` where the file is empty.
- [x] **Step 2:** Add `observability::start` and `shutdown`. Start runs before the first startup log line, and shutdown runs last.
- [x] **Step 3:** Bridge the proxy counters into `oc_cc_*` families on each gather, from one `get_snapshot` call.
- [x] **Step 4:** Tee log records to Loki through `logging::set_record_hook`.
- [x] **Step 5:** Wrap the router with `ServerLayer`, and instrument `send_capped` with `observe_send`.

### Task 6: Verify, release, and document

- [x] **Step 1:** Run the full workspace under `--locked`: fmt, clippy, and tests.
- [ ] **Step 2:** Confirm CI is green on the library PR.
- [ ] **Step 3:** Cut tags `rust/obs/v0.1.0` and `rust/stdlog/v0.1.0` on the merged commit, and switch oc-cc from the local path dependency to the tags.
- [ ] **Step 4:** Run `oc-cc` CI on its PR, and save this plan to Notion.

## Deviations from the first plan

- `Handle::shutdown` is synchronous, not `async`. Shutdown is a blocking join on the worker threads, so the crate needs no async runtime.
- The log hook sits in `logging::log`, which is the single entry point that every macro uses. It is not a field on `Sink`, so a replacement `install`ed sink is still teed.
- The upstream hop's `peer` is the upstream host, not the provider name. `send_capped` has no provider context, and Go's own default peer for an unnamed upstream is `host:port`.
- The inbound span ends when the response headers arrive, not when a stream finishes. Streaming responses therefore record time to first byte, not total stream time.
- Rust has no `go_*` runtime metrics. That is an intentional divergence.
- Coverage is measured differently from Go. Rust counts lines including in-module tests, while Go counts statements with tests excluded. Both clear the 80% gate.
