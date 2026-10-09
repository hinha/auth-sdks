//! Records handed to a [`LogExporter`]. Structured fields are kept as JSON values so the Loki
//! line carries them intact, instead of re-parsing formatted text.

use std::time::SystemTime;

use serde_json::Value;

/// Severity, rendered lowercase as Go's `loki.go` writes the `level` label.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Level {
    Debug,
    Info,
    Warn,
    Error,
}

impl Level {
    /// Lowercase name: `debug`, `info`, `warn`, `error`.
    pub fn as_str(self) -> &'static str {
        match self {
            Level::Debug => "debug",
            Level::Info => "info",
            Level::Warn => "warn",
            Level::Error => "error",
        }
    }
}

/// One log event.
#[derive(Debug, Clone, PartialEq)]
pub struct LogRecord {
    pub time: SystemTime,
    pub level: Level,
    pub msg: String,
    pub fields: Vec<(String, Value)>,
}

impl LogRecord {
    /// A record stamped with the current time and no fields.
    pub fn new(level: Level, msg: impl Into<String>) -> Self {
        Self {
            time: SystemTime::now(),
            level,
            msg: msg.into(),
            fields: Vec::new(),
        }
    }

    /// Appends one structured field.
    pub fn with_field(mut self, key: impl Into<String>, value: impl Into<Value>) -> Self {
        self.fields.push((key.into(), value.into()));
        self
    }
}

/// Receives records. Implementations must not block the caller on I/O.
pub trait LogExporter: Send + Sync {
    fn export(&self, record: &LogRecord);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn level_labels_are_lowercase() {
        assert_eq!(Level::Debug.as_str(), "debug", "debug label");
        assert_eq!(Level::Info.as_str(), "info", "info label");
        assert_eq!(Level::Warn.as_str(), "warn", "warn label");
        assert_eq!(Level::Error.as_str(), "error", "error label");
    }

    #[test]
    fn builder_keeps_fields_in_insertion_order() {
        let rec = LogRecord::new(Level::Info, "hello")
            .with_field("a", 1)
            .with_field("b", "two");
        assert_eq!(rec.msg, "hello", "message kept");
        assert_eq!(rec.fields.len(), 2, "two fields");
        assert_eq!(rec.fields[0].0, "a", "first field key");
        assert_eq!(rec.fields[1].0, "b", "second field key");
    }
}
