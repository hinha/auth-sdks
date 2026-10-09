//! Client and server hops. One histogram family plus one span per hop, so a request reads as
//! `incoming HTTP -> DB -> Redis -> outbound HTTP` in Grafana.

use std::collections::HashSet;
use std::future::Future;
use std::sync::{Arc, Mutex};
use std::time::Instant;

use opentelemetry::trace::{FutureExt, SpanKind, Status, TraceContextExt, Tracer};
use opentelemetry::{Context, KeyValue};
use prometheus::{HistogramOpts, HistogramVec, Registry};

use crate::handle::Handle;

/// Histogram family name.
pub const METRIC_CLIENT_REQUEST_DURATION: &str = "client_request_duration_seconds";
/// Component of an inbound HTTP request.
pub const COMPONENT_HTTP_SERVER: &str = "http_server";
/// Component of an outbound HTTP request.
pub const COMPONENT_HTTP_CLIENT: &str = "http_client";
/// Label value used when a label is empty or a bound is hit.
pub const UNKNOWN: &str = "unknown";

const STATUS_OK: &str = "ok";
const STATUS_ERROR: &str = "error";
const MAX_COMPONENT: usize = 32;
const MAX_OPERATION: usize = 128;
const MAX_PEER: usize = 64;
const MAX_STATUS: usize = 16;
/// Distinct label tuples recorded before new ones collapse to `unknown`.
pub const MAX_HOP_SERIES: usize = 1024;

/// One step in a request. Labels must be templates or classes, never raw keys, SQL, or URLs with
/// IDs.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Hop {
    /// Kind of step, e.g. `redis`, `db`, `http_client`.
    pub component: String,
    /// Command, query template, or `METHOD route`. Never the interpolated SQL or key.
    pub operation: String,
    /// Downstream service name. It must match the downstream `service` for Tempo to join them.
    pub peer: String,
}

impl Hop {
    pub fn new(
        component: impl Into<String>,
        operation: impl Into<String>,
        peer: impl Into<String>,
    ) -> Self {
        Self {
            component: component.into(),
            operation: operation.into(),
            peer: peer.into(),
        }
    }

    fn normalized(&self) -> Self {
        Self {
            component: bound_label(&self.component, MAX_COMPONENT),
            operation: bound_label(&self.operation, MAX_OPERATION),
            peer: bound_label(&self.peer, MAX_PEER),
        }
    }

    fn unknown() -> Self {
        Self {
            component: UNKNOWN.to_string(),
            operation: UNKNOWN.to_string(),
            peer: UNKNOWN.to_string(),
        }
    }
}

/// Trims, substitutes `unknown` for empty input, and truncates to `max` characters.
pub fn bound_label(raw: &str, max: usize) -> String {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return UNKNOWN.to_string();
    }
    if trimmed.chars().count() <= max {
        return trimmed.to_string();
    }
    trimmed.chars().take(max).collect()
}

/// Status label for an HTTP code. Codes outside 100–599 become `unknown`.
pub fn http_status_label(code: u16) -> String {
    if !(100..=599).contains(&code) {
        return UNKNOWN.to_string();
    }
    code.to_string()
}

fn is_http_5xx(status: &str) -> bool {
    status.len() == 3 && status.starts_with('5')
}

/// `db.system` for a database peer name.
pub fn db_system_from_peer(peer: &str) -> &'static str {
    match peer.to_ascii_lowercase().as_str() {
        "postgres" | "postgresql" => "postgresql",
        "mysql" => "mysql",
        "sqlite" => "sqlite",
        _ => "other_sql",
    }
}

/// Splits `METHOD route` at the first space. Without a space the whole value is the method.
pub fn split_http_operation(op: &str) -> (String, String) {
    match op.trim().split_once(' ') {
        Some((method, route)) => (method.to_string(), route.trim().to_string()),
        None => (op.trim().to_string(), String::new()),
    }
}

/// Span attributes derived from the hop. The downstream service is
/// the `peer.service` edge that Tempo's service graph reads.
fn semconv_attributes(hop: &Hop) -> Vec<KeyValue> {
    let mut attrs = Vec::new();
    if hop.component != COMPONENT_HTTP_SERVER && hop.peer != UNKNOWN {
        attrs.push(KeyValue::new("peer.service", hop.peer.clone()));
    }
    match hop.component.as_str() {
        "redis" => attrs.push(KeyValue::new("db.system", "redis")),
        "db" => attrs.push(KeyValue::new("db.system", db_system_from_peer(&hop.peer))),
        COMPONENT_HTTP_SERVER => {
            let (method, route) = split_http_operation(&hop.operation);
            if !method.is_empty() {
                attrs.push(KeyValue::new("http.request.method", method));
            }
            if !route.is_empty() {
                attrs.push(KeyValue::new("http.route", route));
            }
        }
        _ => {}
    }
    attrs
}

/// Duration histogram plus the distinct-series bound.
pub(crate) struct HopMetrics {
    histogram: HistogramVec,
    series: Mutex<HashSet<String>>,
}

impl HopMetrics {
    /// Registers the histogram on a fresh registry. Registration on an empty registry cannot fail.
    pub(crate) fn register(registry: &Registry) -> Arc<Self> {
        let histogram = HistogramVec::new(
            HistogramOpts::new(
                METRIC_CLIENT_REQUEST_DURATION,
                "Duration of a client hop (HTTP, DB, Redis, …) in seconds.",
            )
            .buckets(prometheus::DEFAULT_BUCKETS.to_vec()),
            &["component", "operation", "peer", "status"],
        )
        .expect("valid hop histogram");
        registry
            .register(Box::new(histogram.clone()))
            .expect("fresh registry accepts the hop histogram");
        Arc::new(Self {
            histogram,
            series: Mutex::new(HashSet::new()),
        })
    }

    pub(crate) fn observe(&self, hop: &Hop, status: &str, seconds: f64) {
        let (hop, status) = self.limit(hop, status);
        self.histogram
            .with_label_values(&[
                hop.component.as_str(),
                hop.operation.as_str(),
                hop.peer.as_str(),
                status.as_str(),
            ])
            .observe(seconds);
    }

    /// Admits a tuple until [`MAX_HOP_SERIES`] distinct tuples exist. Later tuples collapse to
    /// `unknown`, so an unbounded `operation` cannot grow the histogram without limit.
    fn limit(&self, hop: &Hop, status: &str) -> (Hop, String) {
        let key = format!(
            "{}\u{0}{}\u{0}{}\u{0}{}",
            hop.component, hop.operation, hop.peer, status
        );
        let mut series = self.series.lock().unwrap_or_else(|e| e.into_inner());
        if series.contains(&key) {
            return (hop.clone(), status.to_string());
        }
        if series.len() >= MAX_HOP_SERIES {
            return (Hop::unknown(), UNKNOWN.to_string());
        }
        series.insert(key);
        (hop.clone(), status.to_string())
    }
}

/// A running hop: its span context plus the start time. Created by [`Handle::start_hop`] and
/// closed with [`HopTimer::finish`].
pub(crate) struct HopTimer {
    cx: Context,
    hop: Hop,
    start: Instant,
    metrics: Arc<HopMetrics>,
}

impl HopTimer {
    /// The context to run the hop's work under, so child spans parent correctly.
    pub(crate) fn context(&self) -> Context {
        self.cx.clone()
    }

    /// Records the status, the error (if any), the span end, and the duration.
    pub(crate) fn finish(self, status: &str, error: Option<&str>) {
        let status = bound_label(status, MAX_STATUS);
        let span = self.cx.span();
        span.set_attributes([
            KeyValue::new("component", self.hop.component.clone()),
            KeyValue::new("operation", self.hop.operation.clone()),
            KeyValue::new("peer", self.hop.peer.clone()),
            KeyValue::new("status", status.clone()),
        ]);
        if let Some(message) = error {
            span.set_status(Status::error(message.to_string()));
        } else if is_http_5xx(&status) {
            span.set_status(Status::error(status.clone()));
        }
        span.end();
        self.metrics
            .observe(&self.hop, &status, self.start.elapsed().as_secs_f64());
    }
}

impl Handle {
    /// Starts a hop. `extra` adds attributes to the span, such as `server.address`.
    pub(crate) fn start_hop(&self, hop: &Hop, extra: Vec<KeyValue>) -> HopTimer {
        let kind = if hop.component == COMPONENT_HTTP_SERVER {
            SpanKind::Server
        } else {
            SpanKind::Client
        };
        let mut attrs = semconv_attributes(hop);
        attrs.extend(extra);
        let tracer = self.tracer();
        let span = tracer
            .span_builder(hop.operation.clone())
            .with_kind(kind)
            .with_attributes(attrs)
            .start(tracer);
        HopTimer {
            cx: Context::current_with_span(span),
            hop: hop.clone(),
            start: Instant::now(),
            metrics: self.hop_metrics(),
        }
    }
}

/// Runs `fut` as one hop. The duration is recorded in `client_request_duration_seconds`, and a
/// child span is opened under the caller's context. `ok` or `error` is the status, and an error
/// is returned unchanged.
pub async fn observe<T, E, Fut>(handle: &Handle, hop: Hop, fut: Fut) -> Result<T, E>
where
    Fut: Future<Output = Result<T, E>>,
    E: std::fmt::Display,
{
    let hop = hop.normalized();
    let timer = handle.start_hop(&hop, Vec::new());
    let result = fut.with_context(timer.context()).await;
    match &result {
        Ok(_) => timer.finish(STATUS_OK, None),
        Err(err) => timer.finish(STATUS_ERROR, Some(&err.to_string())),
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;

    fn handle() -> Handle {
        Handle::new(Config {
            disable_default_collectors: true,
            disable_profiles: true,
            ..Config::default()
        })
    }

    fn find_hop_samples(handle: &Handle) -> Vec<(Vec<(String, String)>, u64)> {
        let mut out = Vec::new();
        for mf in handle.gather() {
            if mf.name() != METRIC_CLIENT_REQUEST_DURATION {
                continue;
            }
            for metric in mf.get_metric() {
                let labels = metric
                    .get_label()
                    .iter()
                    .map(|l| (l.name().to_string(), l.value().to_string()))
                    .collect();
                out.push((labels, metric.get_histogram().get_sample_count()));
            }
        }
        out
    }

    fn label<'a>(labels: &'a [(String, String)], name: &str) -> &'a str {
        labels
            .iter()
            .find(|(n, _)| n == name)
            .map(|(_, v)| v.as_str())
            .unwrap_or("")
    }

    #[test]
    fn bound_label_trims_defaults_and_truncates() {
        assert_eq!(bound_label("  redis  ", 32), "redis", "trimmed");
        assert_eq!(bound_label("   ", 32), "unknown", "blank becomes unknown");
        assert_eq!(
            bound_label(&"x".repeat(40), 32).chars().count(),
            32,
            "truncated by chars"
        );
        assert_eq!(bound_label("héllo", 3), "hél", "multibyte safe");
    }

    #[test]
    fn http_status_label_rejects_out_of_range_codes() {
        assert_eq!(http_status_label(200), "200", "ok code");
        assert_eq!(http_status_label(99), "unknown", "below range");
        assert_eq!(http_status_label(600), "unknown", "above range");
    }

    #[test]
    fn five_xx_detection_matches_three_char_status() {
        assert!(is_http_5xx("503"), "503 is 5xx");
        assert!(!is_http_5xx("404"), "404 is not");
        assert!(!is_http_5xx("error"), "non-numeric is not");
    }

    #[test]
    fn db_system_maps_known_peers_and_defaults_to_other_sql() {
        assert_eq!(
            db_system_from_peer("Postgres"),
            "postgresql",
            "postgres alias"
        );
        assert_eq!(
            db_system_from_peer("postgresql"),
            "postgresql",
            "postgresql"
        );
        assert_eq!(db_system_from_peer("MySQL"), "mysql", "mysql");
        assert_eq!(db_system_from_peer("sqlite"), "sqlite", "sqlite");
        assert_eq!(db_system_from_peer("oracle"), "other_sql", "fallback");
    }

    #[test]
    fn split_http_operation_separates_method_and_route() {
        assert_eq!(
            split_http_operation("GET /notes/{id}"),
            ("GET".to_string(), "/notes/{id}".to_string()),
            "method and route"
        );
        assert_eq!(
            split_http_operation("POST"),
            ("POST".to_string(), String::new()),
            "method only"
        );
    }

    #[test]
    fn semconv_attributes_follow_the_component() {
        let redis = semconv_attributes(&Hop::new("redis", "GET", "cache"));
        assert!(
            redis.iter().any(|kv| kv.key.as_str() == "db.system"),
            "redis has db.system"
        );
        assert!(
            redis.iter().any(|kv| kv.key.as_str() == "peer.service"),
            "redis has peer.service"
        );

        let db = semconv_attributes(&Hop::new("db", "SELECT users", "postgres"));
        assert!(
            db.iter().any(|kv| kv.key.as_str() == "db.system"),
            "db has db.system"
        );

        let server = semconv_attributes(&Hop::new(COMPONENT_HTTP_SERVER, "GET /x", "ignored"));
        assert!(
            server
                .iter()
                .any(|kv| kv.key.as_str() == "http.request.method"),
            "server method"
        );
        assert!(
            server.iter().any(|kv| kv.key.as_str() == "http.route"),
            "server route"
        );
        assert!(
            !server.iter().any(|kv| kv.key.as_str() == "peer.service"),
            "server has no peer.service"
        );

        let unknown_peer = semconv_attributes(&Hop::new("http_client", "GET", UNKNOWN));
        assert!(unknown_peer.is_empty(), "unknown peer is not exported");
    }

    #[test]
    fn normalized_hop_bounds_every_label() {
        let hop = Hop::new(" ", "x".repeat(200), "p".repeat(100)).normalized();
        assert_eq!(hop.component, "unknown", "empty component");
        assert_eq!(
            hop.operation.chars().count(),
            MAX_OPERATION,
            "operation cap"
        );
        assert_eq!(hop.peer.chars().count(), MAX_PEER, "peer cap");
    }

    #[test]
    fn series_cap_collapses_new_tuples_to_unknown() {
        let registry = Registry::new();
        let metrics = HopMetrics::register(&registry);
        for i in 0..MAX_HOP_SERIES {
            let (hop, _) = metrics.limit(&Hop::new("c", format!("op-{i}"), "p"), "ok");
            assert_eq!(hop.operation, format!("op-{i}"), "admitted below the cap");
        }
        let (hop, status) = metrics.limit(&Hop::new("c", "overflow", "p"), "ok");
        assert_eq!(hop.component, UNKNOWN, "overflow component");
        assert_eq!(status, UNKNOWN, "overflow status");
        let (kept, _) = metrics.limit(&Hop::new("c", "op-0", "p"), "ok");
        assert_eq!(kept.operation, "op-0", "existing series still admitted");
    }

    #[tokio::test]
    async fn observe_records_ok_and_passes_the_value_through() {
        let handle = handle();
        let value: Result<u32, String> =
            observe(&handle, Hop::new("db", "SELECT users", "postgres"), async {
                Ok::<u32, String>(7)
            })
            .await;
        assert_eq!(value, Ok(7), "value returned unchanged");
        let samples = find_hop_samples(&handle);
        let ok = samples
            .iter()
            .find(|(labels, _)| label(labels, "status") == "ok")
            .expect("ok sample");
        assert_eq!(label(&ok.0, "component"), "db", "component label");
        assert_eq!(ok.1, 1, "one observation");
    }

    #[tokio::test]
    async fn observe_records_error_and_returns_it_unchanged() {
        let handle = handle();
        let value: Result<(), String> =
            observe(&handle, Hop::new("redis", "GET", "cache"), async {
                Err::<(), String>("boom".to_string())
            })
            .await;
        assert_eq!(value, Err("boom".to_string()), "error returned unchanged");
        let samples = find_hop_samples(&handle);
        assert!(
            samples
                .iter()
                .any(|(labels, _)| label(labels, "status") == "error"),
            "error status recorded"
        );
    }

    #[tokio::test]
    async fn observe_runs_children_under_the_hop_span() {
        let handle = handle();
        let result: Result<(), String> =
            observe(&handle, Hop::new("db", "outer", "postgres"), async {
                let cx = Context::current();
                assert!(
                    cx.has_active_span(),
                    "hop span is current inside the future"
                );
                Ok(())
            })
            .await;
        assert!(result.is_ok(), "future completed");
    }
}
