//! Configuration for [`crate::Handle`]. Field names and defaults mirror `go/obs` `Config`.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

/// Receives fail-open warnings as `(key, detail)`. Each key is reported once per handle.
pub type WarnFn = Arc<dyn Fn(&str, &str) + Send + Sync>;

/// Default HTTP timeout for pushes and OTLP export (`go/obs`: 5 s).
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);
/// Default remote-write interval (`go/obs`: 15 s).
pub const DEFAULT_METRICS_INTERVAL: Duration = Duration::from_secs(15);
/// Maximum distinct filesystem paths sampled (`go/obs`: 8).
pub const MAX_FILESYSTEM_PATHS: usize = 8;
/// Filesystem path sampled when none is configured (the process working directory).
pub const DEFAULT_FILESYSTEM_PATH: &str = ".";

/// Credentials and switches for Gigapipe ingest. An empty `url` makes the handle a no-op for
/// remote writes, traces, and profiles. The local registry still works.
#[derive(Clone, Default)]
pub struct Config {
    /// Gigapipe base URL, e.g. `https://gigapipe.example`.
    pub url: String,
    pub username: String,
    pub password: String,
    /// Written to the `service` label (omitted when empty). Defaults to `auth-sdks` for
    /// `service_name`.
    pub service: String,
    /// Written to the `env` label (omitted when empty).
    pub env: String,
    /// Written to the `service_namespace` label (omitted when empty).
    pub service_namespace: String,
    /// Written to the `service_version` label on `target_info` only. Rust has no build-info
    /// fallback, so set this from the caller (e.g. `env!("CARGO_PKG_VERSION")`).
    pub service_version: String,
    /// Zero means [`DEFAULT_TIMEOUT`].
    pub timeout: Duration,
    /// Zero means [`DEFAULT_METRICS_INTERVAL`].
    pub metrics_interval: Duration,
    pub disable_metrics: bool,
    pub disable_traces: bool,
    pub disable_profiles: bool,
    /// Drops the process and filesystem collectors. `target_info` is still emitted.
    pub disable_default_collectors: bool,
    /// Volumes sampled for `process_filesystem_*`. Empty means `["."]`, and at most eight unique
    /// cleaned paths are kept.
    pub filesystem_paths: Vec<String>,
    /// Installs the tracer provider and a W3C `tracecontext` + `baggage` propagator globally.
    pub install_global: bool,
    /// Optional warning hook. When `None`, warnings go to stderr.
    pub on_warn: Option<WarnFn>,
}

impl Config {
    /// Applies the Go defaults. Zero durations take their defaults, and filesystem paths are
    /// cleaned, de-duplicated, and capped.
    pub(crate) fn normalized(mut self) -> Self {
        if self.timeout.is_zero() {
            self.timeout = DEFAULT_TIMEOUT;
        }
        if self.metrics_interval.is_zero() {
            self.metrics_interval = DEFAULT_METRICS_INTERVAL;
        }
        self.filesystem_paths = clean_paths(&self.filesystem_paths);
        self
    }
}

/// Cleans, de-duplicates, and caps filesystem paths. An empty result becomes `["."]`.
pub(crate) fn clean_paths(paths: &[String]) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();
    for raw in paths {
        if out.len() == MAX_FILESYSTEM_PATHS {
            break;
        }
        let cleaned = clean_path(raw);
        if cleaned.is_empty() || out.contains(&cleaned) {
            continue;
        }
        out.push(cleaned);
    }
    if out.is_empty() {
        out.push(DEFAULT_FILESYSTEM_PATH.to_string());
    }
    out
}

fn clean_path(raw: &str) -> String {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return String::new();
    }
    let cleaned: PathBuf = Path::new(trimmed).components().collect();
    let text = cleaned.to_string_lossy().into_owned();
    if text.is_empty() {
        DEFAULT_FILESYSTEM_PATH.to_string()
    } else {
        text
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn zero_values_take_the_go_defaults() {
        let cfg = Config::default().normalized();
        assert_eq!(cfg.timeout, Duration::from_secs(5), "timeout default");
        assert_eq!(
            cfg.metrics_interval,
            Duration::from_secs(15),
            "interval default"
        );
        assert_eq!(cfg.filesystem_paths, vec![".".to_string()], "cwd default");
    }

    #[test]
    fn explicit_durations_are_kept() {
        let cfg = Config {
            timeout: Duration::from_millis(250),
            metrics_interval: Duration::from_secs(2),
            ..Config::default()
        }
        .normalized();
        assert_eq!(
            cfg.timeout,
            Duration::from_millis(250),
            "explicit timeout kept"
        );
        assert_eq!(
            cfg.metrics_interval,
            Duration::from_secs(2),
            "explicit interval kept"
        );
    }

    #[test]
    fn filesystem_paths_are_cleaned_unique_and_capped() {
        let raw: Vec<String> = [
            "/var/lib/app/",
            "/var/lib/app",
            "  ",
            "/data",
            "/a",
            "/b",
            "/c",
            "/d",
            "/e",
            "/f",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect();
        let paths = clean_paths(&raw);
        assert_eq!(paths.len(), MAX_FILESYSTEM_PATHS, "capped at eight");
        assert_eq!(paths[0], "/var/lib/app", "trailing slash cleaned");
        assert_eq!(paths[1], "/data", "duplicate and blank entries dropped");
    }

    #[test]
    fn blank_only_paths_fall_back_to_cwd() {
        assert_eq!(
            clean_paths(&["   ".to_string()]),
            vec![".".to_string()],
            "blank falls back"
        );
    }
}
