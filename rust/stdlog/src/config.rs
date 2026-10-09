//! Configuration for the Loki sink.

use std::sync::Arc;
use std::time::Duration;

/// Receives fail-open warnings as `(key, detail)`. The key is `gigapipe loki push failed`, and the
/// detail is the HTTP status (`401 Unauthorized`) or `error` for a transport failure.
pub type WarnFn = Arc<dyn Fn(&str, &str) + Send + Sync>;

/// Default HTTP timeout per push (5 s).
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);
/// Records per push (50).
pub const DEFAULT_BATCH_SIZE: usize = 50;
/// Maximum buffered records before the oldest is dropped (1000).
pub const DEFAULT_MAX_BUFFER: usize = 1000;

/// Settings for [`crate::LokiSink`]. An empty `url` disables the sink.
#[derive(Clone, Default)]
pub struct LokiConfig {
    /// Gigapipe base URL. `/loki/api/v1/push` is appended unless already present.
    pub url: String,
    pub username: String,
    pub password: String,
    /// Written to the `env` label when non-empty.
    pub env: String,
    /// Written to the `service` label, unless a record carries a non-empty `service` field.
    pub service: String,
    /// Zero means [`DEFAULT_TIMEOUT`].
    pub timeout: Duration,
    /// Zero means [`DEFAULT_BATCH_SIZE`].
    pub batch_size: usize,
    /// Zero disables the periodic flush. Batches then leave on size or at shutdown.
    pub flush_interval: Duration,
    /// Zero means [`DEFAULT_MAX_BUFFER`].
    pub max_buffer: usize,
    /// Secret values (API keys, tokens) replaced inside every message and string field.
    pub secret_values: Vec<String>,
    /// Optional warning hook. When `None`, warnings go to stderr.
    pub on_warn: Option<WarnFn>,
}

impl LokiConfig {
    /// Applies the defaults for zero values.
    pub(crate) fn normalized(mut self) -> Self {
        if self.timeout.is_zero() {
            self.timeout = DEFAULT_TIMEOUT;
        }
        if self.batch_size == 0 {
            self.batch_size = DEFAULT_BATCH_SIZE;
        }
        if self.max_buffer == 0 {
            self.max_buffer = DEFAULT_MAX_BUFFER;
        }
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn zero_values_take_the_go_defaults() {
        let cfg = LokiConfig::default().normalized();
        assert_eq!(cfg.timeout, Duration::from_secs(5), "timeout default");
        assert_eq!(cfg.batch_size, 50, "batch size default");
        assert_eq!(cfg.max_buffer, 1000, "max buffer default");
        assert!(
            cfg.flush_interval.is_zero(),
            "flush interval stays off by default"
        );
    }

    #[test]
    fn explicit_values_survive_normalization() {
        let cfg = LokiConfig {
            timeout: Duration::from_millis(250),
            batch_size: 3,
            max_buffer: 7,
            ..LokiConfig::default()
        }
        .normalized();
        assert_eq!(
            cfg.timeout,
            Duration::from_millis(250),
            "explicit timeout kept"
        );
        assert_eq!(cfg.batch_size, 3, "explicit batch size kept");
        assert_eq!(cfg.max_buffer, 7, "explicit max buffer kept");
    }
}
