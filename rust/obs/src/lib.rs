//! Gigapipe metrics, traces, and profiles for Rust services.
//!
//! The same endpoints serve all three signals:
//!
//! | Signal | Endpoint | Notes |
//! |---|---|---|
//! | Metrics | `POST /api/v1/prom/remote/write` | Snappy protobuf remote write 1.0, HTTP Basic |
//! | Traces | `POST /v1/traces` | OTLP/HTTP, protobuf, batch export |
//! | Profiles | `POST /ingest` | Pyroscope push API, pprof CPU profile |
//!
//! Every path is fail-open. An empty `Config::url` makes the handle local-only. A failed push or
//! export is reported once through `Config::on_warn` and never blocks or panics the caller.
//!
//! Logs live in the sibling crate `auth-sdks-stdlog`.

mod collectors;
mod config;
mod handle;
mod hop;
mod http;
mod identity;
mod metrics;
mod profiles;
mod prompb;
mod remote_write;
#[cfg(test)]
mod test_server;
mod traces;
mod warn;

pub use config::{
    Config, DEFAULT_FILESYSTEM_PATH, DEFAULT_METRICS_INTERVAL, DEFAULT_TIMEOUT,
    MAX_FILESYSTEM_PATHS, WarnFn,
};
pub use handle::Handle;
pub use hop::{
    COMPONENT_HTTP_CLIENT, COMPONENT_HTTP_SERVER, Hop, MAX_HOP_SERIES,
    METRIC_CLIENT_REQUEST_DURATION, UNKNOWN, bound_label, db_system_from_peer, http_status_label,
    observe, split_http_operation,
};
pub use http::{RouteFn, ServerLayer, ServerService, execute, observe_send};
pub use identity::{DEFAULT_SERVICE_NAME, Identity};
pub use metrics::{DROPPED_LABELS, Gatherer, write_request};
pub use profiles::{SAMPLE_RATE_HZ, origin_url};
pub use remote_write::{REMOTE_WRITE_PATH, encode, remote_write_url};
pub use traces::{TRACES_PATH, basic_auth_header, traces_url};
pub use warn::MAX_WARNED_KEYS;
