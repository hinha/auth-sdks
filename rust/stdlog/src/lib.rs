//! Gigapipe Loki log sink for Rust services.
//!
//! Padanan `go/stdlog/loki.go` (`WrapLoki`). Records are pushed as JSON to
//! `POST {url}/loki/api/v1/push` with HTTP Basic auth. The sink is fail-open: a transport error or
//! a rejected push never reaches the caller.
//!
//! Like `go/stdlog`, this crate does not depend on `auth-sdks-obs`. Metrics, traces, and profiles
//! live in `rust/obs`.

mod config;
mod kind;
mod loki;
mod record;
mod redact;
#[cfg(test)]
mod test_server;

pub use config::{DEFAULT_BATCH_SIZE, DEFAULT_MAX_BUFFER, DEFAULT_TIMEOUT, LokiConfig, WarnFn};
pub use kind::{
    ACCESS_LOG_MESSAGE, AUDIT_LOG_MESSAGE, FIELD_KIND, KIND_ACCESS, KIND_APP, KIND_AUDIT, KIND_SDK,
    kind_from,
};
pub use loki::{LokiSink, push_url};
pub use record::{Level, LogExporter, LogRecord};
pub use redact::{REDACTED, redact_text, skip_secret_key};
