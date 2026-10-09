//! Gigapipe/Loki push sink. Padanan `go/stdlog/loki.go` (`WrapLoki`).
//!
//! [`LogExporter::export`] only appends to a bounded buffer, so the caller never waits on the
//! network. One worker thread owns the blocking HTTP client and posts a batch when the buffer
//! reaches `batch_size`, when `flush_interval` elapses, or at [`LokiSink::shutdown`].

use std::collections::{BTreeMap, HashMap, VecDeque};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex, MutexGuard};
use std::thread::JoinHandle;
use std::time::{Duration, UNIX_EPOCH};

use reqwest::StatusCode;
use reqwest::blocking::Client;
use reqwest::header::{CONTENT_TYPE, USER_AGENT};
use serde_json::{Map, Value, json};

use crate::config::{LokiConfig, WarnFn};
use crate::kind::{KIND_AUDIT, kind_from};
use crate::record::{LogExporter, LogRecord};
use crate::redact::{redact_text, skip_secret_key};

/// Path appended to the Gigapipe base URL.
pub const PUSH_PATH: &str = "/loki/api/v1/push";
/// `User-Agent` sent with every push (`go/stdlog`: `auth-sdks-stdlog/1`).
pub const USER_AGENT_VALUE: &str = "auth-sdks-stdlog/1";
/// Warning key reported once per sink, as Go's `warnPush` does.
pub const WARN_KEY: &str = "gigapipe loki push failed";

/// Normalizes a base URL into the push endpoint. A URL that already ends in the push path is kept.
pub fn push_url(base: &str) -> String {
    let base = base.trim().trim_end_matches('/');
    if base.ends_with(PUSH_PATH) {
        return base.to_string();
    }
    format!("{base}{PUSH_PATH}")
}

/// One record ready to push.
struct Entry {
    ts_ns: i64,
    labels: BTreeMap<String, String>,
    line: String,
}

struct State {
    buf: VecDeque<Entry>,
    closed: bool,
}

/// State shared by the producer side and the worker thread.
struct Shared {
    push_url: String,
    username: String,
    password: String,
    env: String,
    service: String,
    timeout: Duration,
    batch_size: usize,
    flush_interval: Duration,
    max_buffer: usize,
    secret_values: Vec<String>,
    on_warn: Option<WarnFn>,
    warned: AtomicBool,
    state: Mutex<State>,
    cond: Condvar,
}

/// A Loki sink. Create it with [`LokiSink::new`], share it as `Arc<LokiSink>`, and call
/// [`LokiSink::shutdown`] before exit so the buffered tail is pushed.
pub struct LokiSink {
    shared: Arc<Shared>,
    worker: Mutex<Option<JoinHandle<()>>>,
    enabled: bool,
}

impl LokiSink {
    /// Starts the worker. An empty `cfg.url` returns a disabled sink that drops every record,
    /// like `WrapLoki` with an empty URL.
    pub fn new(cfg: LokiConfig) -> Arc<Self> {
        let cfg = cfg.normalized();
        if cfg.url.trim().is_empty() {
            let shared = Arc::new(Shared::new(String::new(), cfg));
            return Arc::new(Self {
                shared,
                worker: Mutex::new(None),
                enabled: false,
            });
        }
        let shared = Arc::new(Shared::new(push_url(&cfg.url), cfg));
        let worker_shared = Arc::clone(&shared);
        let spawned = std::thread::Builder::new()
            .name("stdlog-loki".to_string())
            .spawn(move || run_worker(worker_shared));
        match spawned {
            Ok(handle) => Arc::new(Self {
                shared,
                worker: Mutex::new(Some(handle)),
                enabled: true,
            }),
            Err(_) => {
                shared.warn("gigapipe loki worker failed", "spawn");
                Arc::new(Self {
                    shared,
                    worker: Mutex::new(None),
                    enabled: false,
                })
            }
        }
    }

    /// Stops accepting records, pushes everything still buffered, and joins the worker. Calling it
    /// again is a no-op.
    pub fn shutdown(&self) {
        if !self.enabled {
            return;
        }
        {
            let mut state = lock(&self.shared.state);
            if state.closed {
                return;
            }
            state.closed = true;
        }
        self.shared.cond.notify_all();
        if let Some(handle) = lock(&self.worker).take() {
            let _ = handle.join();
        }
    }
}

impl LogExporter for LokiSink {
    fn export(&self, record: &LogRecord) {
        if !self.enabled {
            return;
        }
        let entry = build_entry(&self.shared, record);
        let wake = {
            let mut state = lock(&self.shared.state);
            if state.closed {
                return;
            }
            while state.buf.len() >= self.shared.max_buffer {
                state.buf.pop_front();
            }
            state.buf.push_back(entry);
            state.buf.len() >= self.shared.batch_size
        };
        if wake {
            self.shared.cond.notify_all();
        }
    }
}

impl Drop for LokiSink {
    fn drop(&mut self) {
        self.shutdown();
    }
}

impl Shared {
    fn new(push_url: String, cfg: LokiConfig) -> Self {
        Self {
            push_url,
            username: cfg.username,
            password: cfg.password,
            env: cfg.env,
            service: cfg.service,
            timeout: cfg.timeout,
            batch_size: cfg.batch_size,
            flush_interval: cfg.flush_interval,
            max_buffer: cfg.max_buffer,
            secret_values: cfg.secret_values,
            on_warn: cfg.on_warn,
            warned: AtomicBool::new(false),
            state: Mutex::new(State {
                buf: VecDeque::new(),
                closed: false,
            }),
            cond: Condvar::new(),
        }
    }

    /// Waits for the next batch. Returns `(batch, stop)`, and `stop` is true once the sink is
    /// closed. The buffer is drained on close, so the tail is never lost.
    fn next_batch(&self) -> (Vec<Entry>, bool) {
        let mut state = lock(&self.state);
        loop {
            if state.closed {
                return (state.buf.drain(..).collect(), true);
            }
            if state.buf.len() >= self.batch_size {
                // A size-triggered push never exceeds `batch_size`, as Go's enqueue, so a backlog
                // leaves in several requests rather than one oversized body.
                let take = self.batch_size.min(state.buf.len());
                return (state.buf.drain(..take).collect(), false);
            }
            if self.flush_interval.is_zero() {
                state = self.cond.wait(state).unwrap_or_else(|e| e.into_inner());
            } else {
                let (next, timeout) = self
                    .cond
                    .wait_timeout(state, self.flush_interval)
                    .unwrap_or_else(|e| e.into_inner());
                state = next;
                if timeout.timed_out() && !state.buf.is_empty() {
                    return (state.buf.drain(..).collect(), false);
                }
            }
        }
    }

    fn post(&self, client: &Client, batch: &[Entry]) {
        let body = push_body(batch);
        let mut request = client
            .post(&self.push_url)
            .header(CONTENT_TYPE, "application/json")
            .header(USER_AGENT, USER_AGENT_VALUE)
            .body(body);
        if !self.username.is_empty() || !self.password.is_empty() {
            request = request.basic_auth(&self.username, Some(&self.password));
        }
        match request.send() {
            Err(_) => self.warn(WARN_KEY, "error"),
            Ok(response) => {
                let status = response.status();
                if status == StatusCode::UNAUTHORIZED || status == StatusCode::FORBIDDEN {
                    self.warn(WARN_KEY, &status.to_string());
                }
            }
        }
    }

    /// Reports a fail-open condition once per sink. Repeats would flood the log on every batch.
    fn warn(&self, key: &str, detail: &str) {
        if self.warned.swap(true, Ordering::SeqCst) {
            return;
        }
        match &self.on_warn {
            Some(hook) => hook(key, detail),
            None => eprintln!("{key}: {detail}"),
        }
    }
}

fn run_worker(shared: Arc<Shared>) {
    let client = match Client::builder().timeout(shared.timeout).build() {
        Ok(client) => client,
        Err(_) => {
            shared.warn(WARN_KEY, "client");
            return;
        }
    };
    loop {
        let (batch, stop) = shared.next_batch();
        if !batch.is_empty() {
            shared.post(&client, &batch);
        }
        if stop {
            break;
        }
    }
}

fn build_entry(shared: &Shared, record: &LogRecord) -> Entry {
    let kind = kind_from(&record.msg, &record.fields);
    let ts_ns = record
        .time
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or(0)
        .min(i64::MAX as u128) as i64;
    Entry {
        ts_ns,
        labels: labels_for(shared, record, &kind),
        line: line_json(shared, record),
    }
}

/// The Loki stream labels, as `labelsFor` in `go/stdlog/loki.go`. Audit records also carry
/// `event_type` and `decision`. Labels stay low-cardinality, so IDs and session values are never
/// added here.
fn labels_for(shared: &Shared, record: &LogRecord, kind: &str) -> BTreeMap<String, String> {
    let mut labels = BTreeMap::new();
    labels.insert("kind".to_string(), kind.to_string());
    labels.insert("level".to_string(), record.level.as_str().to_string());
    if !shared.env.is_empty() {
        labels.insert("env".to_string(), shared.env.clone());
    }
    let mut service = shared.service.clone();
    let mut event_type = String::new();
    let mut decision = String::new();
    for (key, value) in &record.fields {
        match (key.as_str(), value) {
            ("service", Value::String(name)) if !name.is_empty() => service = name.clone(),
            ("event_type", Value::String(name)) => event_type = name.clone(),
            ("decision", Value::String(name)) => decision = name.clone(),
            _ => {}
        }
    }
    if !service.is_empty() {
        labels.insert("service".to_string(), service);
    }
    if kind == KIND_AUDIT {
        if !event_type.is_empty() {
            labels.insert("event_type".to_string(), event_type);
        }
        if !decision.is_empty() {
            labels.insert("decision".to_string(), decision);
        }
    }
    labels
}

/// The JSON line for one record, as `lineJSON` in `go/stdlog/loki.go`. Secret-bearing keys are
/// dropped, and configured secret values are scrubbed from string fields. Nested values are not
/// walked, so keep secrets out of nested objects.
fn line_json(shared: &Shared, record: &LogRecord) -> String {
    let mut map = Map::new();
    map.insert(
        "msg".to_string(),
        Value::String(redact_text(&record.msg, &shared.secret_values)),
    );
    map.insert(
        "level".to_string(),
        Value::String(record.level.as_str().to_string()),
    );
    for (key, value) in &record.fields {
        if skip_secret_key(key) {
            continue;
        }
        map.insert(key.clone(), redact_value(value, &shared.secret_values));
    }
    Value::Object(map).to_string()
}

fn redact_value(value: &Value, secrets: &[String]) -> Value {
    match value {
        Value::String(text) => Value::String(redact_text(text, secrets)),
        other => other.clone(),
    }
}

/// The Loki push body. Entries are grouped into one stream per distinct label set, in the order
/// the streams first appear, as in Go.
fn push_body(batch: &[Entry]) -> String {
    let mut streams: Vec<(BTreeMap<String, String>, Vec<Value>)> = Vec::new();
    let mut index: HashMap<String, usize> = HashMap::new();
    for entry in batch {
        let key = stream_key(&entry.labels);
        let slot = match index.get(&key) {
            Some(slot) => *slot,
            None => {
                streams.push((entry.labels.clone(), Vec::new()));
                let slot = streams.len() - 1;
                index.insert(key, slot);
                slot
            }
        };
        streams[slot]
            .1
            .push(json!([entry.ts_ns.to_string(), entry.line]));
    }
    let streams: Vec<Value> = streams
        .into_iter()
        .map(|(labels, values)| json!({ "stream": labels, "values": values }))
        .collect();
    json!({ "streams": streams }).to_string()
}

fn stream_key(labels: &BTreeMap<String, String>) -> String {
    labels
        .iter()
        .map(|(key, value)| format!("{key}={value}"))
        .collect::<Vec<_>>()
        .join("\u{1f}")
}

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|e| e.into_inner())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kind::{ACCESS_LOG_MESSAGE, AUDIT_LOG_MESSAGE};
    use crate::record::Level;
    use crate::test_server::TestServer;
    use std::sync::Mutex as StdMutex;
    use std::time::Instant;

    fn cfg_for(url: &str) -> LokiConfig {
        LokiConfig {
            url: url.to_string(),
            service: "svc".to_string(),
            env: "test".to_string(),
            ..LokiConfig::default()
        }
    }

    /// Polls the server until `want` requests arrive, or fails after a deadline.
    fn wait_for_requests(server: &TestServer, want: usize) -> Vec<crate::test_server::Captured> {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let got = server.requests();
            if got.len() >= want {
                return got;
            }
            assert!(
                Instant::now() < deadline,
                "timed out waiting for {want} requests, got {}",
                got.len()
            );
            std::thread::sleep(Duration::from_millis(10));
        }
    }

    fn entry(labels: &[(&str, &str)], line: &str, ts_ns: i64) -> Entry {
        Entry {
            ts_ns,
            labels: labels
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            line: line.to_string(),
        }
    }

    #[test]
    fn push_url_appends_the_path_once() {
        assert_eq!(
            push_url("https://g.example"),
            "https://g.example/loki/api/v1/push",
            "bare base"
        );
        assert_eq!(
            push_url("https://g.example/"),
            "https://g.example/loki/api/v1/push",
            "trailing slash"
        );
        assert_eq!(
            push_url("https://g.example/loki/api/v1/push/"),
            "https://g.example/loki/api/v1/push",
            "already complete"
        );
    }

    #[test]
    fn empty_url_disables_the_sink() {
        let sink = LokiSink::new(LokiConfig::default());
        let rec = LogRecord::new(Level::Info, "dropped");
        sink.export(&rec);
        sink.shutdown();
        assert!(!sink.enabled, "no url means disabled");
        assert!(lock(&sink.shared.state).buf.is_empty(), "nothing buffered");
    }

    #[test]
    fn labels_follow_go_rules() {
        let shared = Shared::new(String::new(), cfg_for("").normalized());
        let rec = LogRecord::new(Level::Info, "hi");
        let labels = labels_for(&shared, &rec, "app");
        assert_eq!(
            labels.get("kind").map(String::as_str),
            Some("app"),
            "kind label"
        );
        assert_eq!(
            labels.get("level").map(String::as_str),
            Some("info"),
            "level label"
        );
        assert_eq!(
            labels.get("env").map(String::as_str),
            Some("test"),
            "env label"
        );
        assert_eq!(
            labels.get("service").map(String::as_str),
            Some("svc"),
            "default service"
        );
        assert!(
            !labels.contains_key("event_type"),
            "no event_type outside audit"
        );
    }

    #[test]
    fn service_field_overrides_default_and_empty_env_is_omitted() {
        let mut cfg = cfg_for("");
        cfg.env = String::new();
        let shared = Shared::new(String::new(), cfg.normalized());
        let rec = LogRecord::new(Level::Warn, "hi").with_field("service", "override");
        let labels = labels_for(&shared, &rec, "app");
        assert_eq!(
            labels.get("service").map(String::as_str),
            Some("override"),
            "field overrides"
        );
        assert!(!labels.contains_key("env"), "empty env omitted");
    }

    #[test]
    fn empty_service_field_keeps_default() {
        let shared = Shared::new(String::new(), cfg_for("").normalized());
        let rec = LogRecord::new(Level::Info, "hi").with_field("service", "");
        let labels = labels_for(&shared, &rec, "app");
        assert_eq!(
            labels.get("service").map(String::as_str),
            Some("svc"),
            "empty field ignored"
        );
    }

    #[test]
    fn audit_records_carry_event_type_and_decision() {
        let shared = Shared::new(String::new(), cfg_for("").normalized());
        let rec = LogRecord::new(Level::Info, AUDIT_LOG_MESSAGE)
            .with_field("event_type", "login")
            .with_field("decision", "allow");
        let kind = kind_from(&rec.msg, &rec.fields);
        let labels = labels_for(&shared, &rec, &kind);
        assert_eq!(
            labels.get("kind").map(String::as_str),
            Some("audit"),
            "audit kind"
        );
        assert_eq!(
            labels.get("event_type").map(String::as_str),
            Some("login"),
            "event_type label"
        );
        assert_eq!(
            labels.get("decision").map(String::as_str),
            Some("allow"),
            "decision label"
        );
    }

    #[test]
    fn event_type_and_decision_accept_empty_strings_only_for_audit() {
        let shared = Shared::new(String::new(), cfg_for("").normalized());
        let rec = LogRecord::new(Level::Info, AUDIT_LOG_MESSAGE).with_field("event_type", "");
        let labels = labels_for(&shared, &rec, KIND_AUDIT);
        assert_eq!(
            labels.get("event_type").map(String::as_str),
            None,
            "empty event_type dropped"
        );
    }

    #[test]
    fn line_json_keeps_fields_and_drops_secrets() {
        let mut cfg = cfg_for("");
        cfg.secret_values = vec!["sk-live-999".to_string()];
        let shared = Shared::new(String::new(), cfg.normalized());
        let rec = LogRecord::new(Level::Error, "upstream failed for sk-live-999")
            .with_field("status", 502)
            .with_field("password", "hunter2")
            .with_field("Authorization", "Bearer x")
            .with_field("note", "key sk-live-999 used");
        let line = line_json(&shared, &rec);
        let parsed: Value = serde_json::from_str(&line).expect("line is valid json");
        assert_eq!(
            parsed["msg"], "upstream failed for [REDACTED]",
            "msg scrubbed"
        );
        assert_eq!(parsed["level"], "error", "level present");
        assert_eq!(parsed["status"], 502, "numeric field kept");
        assert_eq!(
            parsed["note"], "key [REDACTED] used",
            "string field scrubbed"
        );
        assert!(parsed.get("password").is_none(), "password dropped");
        assert!(
            parsed.get("Authorization").is_none(),
            "authorization dropped"
        );
    }

    #[test]
    fn field_named_msg_overrides_the_message_like_go() {
        let shared = Shared::new(String::new(), cfg_for("").normalized());
        let rec = LogRecord::new(Level::Info, "original").with_field("msg", "overridden");
        let parsed: Value = serde_json::from_str(&line_json(&shared, &rec)).expect("json");
        assert_eq!(parsed["msg"], "overridden", "later field wins, as in Go");
    }

    #[test]
    fn push_body_groups_entries_by_stream() {
        let batch = vec![
            entry(&[("kind", "app"), ("level", "info")], "{\"msg\":\"a\"}", 1),
            entry(
                &[("kind", "audit"), ("level", "info")],
                "{\"msg\":\"b\"}",
                2,
            ),
            entry(&[("kind", "app"), ("level", "info")], "{\"msg\":\"c\"}", 3),
        ];
        let body: Value = serde_json::from_str(&push_body(&batch)).expect("body json");
        let streams = body["streams"].as_array().expect("streams array");
        assert_eq!(streams.len(), 2, "two distinct label sets");
        assert_eq!(streams[0]["stream"]["kind"], "app", "first stream is app");
        assert_eq!(
            streams[0]["values"].as_array().expect("values").len(),
            2,
            "app has two values"
        );
        assert_eq!(
            streams[0]["values"][0][0], "1",
            "timestamp is a string of nanoseconds"
        );
        assert_eq!(
            streams[1]["stream"]["kind"], "audit",
            "second stream is audit"
        );
    }

    #[test]
    fn stream_key_is_stable_for_equal_labels() {
        let a: BTreeMap<String, String> = [("a", "1"), ("b", "2")]
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        let b: BTreeMap<String, String> = [("b", "2"), ("a", "1")]
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        assert_eq!(
            stream_key(&a),
            stream_key(&b),
            "order of insertion does not matter"
        );
    }

    #[test]
    fn batch_size_triggers_a_push_with_basic_auth_and_headers() {
        let server = TestServer::start(vec![204]);
        let mut cfg = cfg_for(&server.url);
        cfg.username = "user".to_string();
        cfg.password = "pass".to_string();
        cfg.batch_size = 2;
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "one"));
        sink.export(&LogRecord::new(Level::Info, ACCESS_LOG_MESSAGE));

        let got = wait_for_requests(&server, 1);
        sink.shutdown();
        let req = &got[0];
        assert_eq!(req.method, "POST", "push is a POST");
        assert_eq!(req.path, "/loki/api/v1/push", "push path");
        assert_eq!(
            req.headers.get("content-type").map(String::as_str),
            Some("application/json"),
            "json body"
        );
        assert_eq!(
            req.headers.get("user-agent").map(String::as_str),
            Some("auth-sdks-stdlog/1"),
            "user agent"
        );
        assert_eq!(
            req.headers.get("authorization").map(String::as_str),
            Some("Basic dXNlcjpwYXNz"),
            "basic auth for user:pass"
        );
        let body: Value = serde_json::from_str(&req.body).expect("body json");
        let total: usize = body["streams"]
            .as_array()
            .expect("streams")
            .iter()
            .map(|s| s["values"].as_array().map_or(0, Vec::len))
            .sum();
        assert_eq!(total, 2, "both records pushed in one batch");
    }

    #[test]
    fn no_credentials_means_no_authorization_header() {
        let server = TestServer::start(vec![204]);
        let sink = LokiSink::new(cfg_for(&server.url));
        sink.export(&LogRecord::new(Level::Info, "x"));
        sink.shutdown();
        let got = server.requests();
        assert_eq!(got.len(), 1, "shutdown flushed the tail");
        assert!(
            !got[0].headers.contains_key("authorization"),
            "no auth header without credentials"
        );
    }

    #[test]
    fn shutdown_flushes_the_buffered_tail_and_is_idempotent() {
        let server = TestServer::start(vec![204]);
        let sink = LokiSink::new(cfg_for(&server.url));
        for i in 0..3 {
            sink.export(&LogRecord::new(Level::Info, format!("line {i}")));
        }
        sink.shutdown();
        sink.shutdown();
        let got = server.requests();
        assert_eq!(got.len(), 1, "one final push, second shutdown does nothing");
        let body: Value = serde_json::from_str(&got[0].body).expect("body");
        assert_eq!(
            body["streams"][0]["values"]
                .as_array()
                .expect("values")
                .len(),
            3,
            "all three flushed"
        );
    }

    #[test]
    fn export_after_shutdown_is_dropped() {
        let server = TestServer::start(vec![204]);
        let sink = LokiSink::new(cfg_for(&server.url));
        sink.shutdown();
        sink.export(&LogRecord::new(Level::Info, "late"));
        assert!(server.requests().is_empty(), "closed sink never pushes");
    }

    #[test]
    fn buffer_cap_drops_the_oldest_record() {
        let server = TestServer::start(vec![204]);
        let mut cfg = cfg_for(&server.url);
        cfg.max_buffer = 2;
        cfg.batch_size = 100;
        let sink = LokiSink::new(cfg);
        for msg in ["first", "second", "third"] {
            sink.export(&LogRecord::new(Level::Info, msg));
        }
        sink.shutdown();
        let got = server.requests();
        let body: Value = serde_json::from_str(&got[0].body).expect("body");
        let lines: Vec<String> = body["streams"][0]["values"]
            .as_array()
            .expect("values")
            .iter()
            .map(|v| v[1].as_str().expect("line").to_string())
            .collect();
        assert_eq!(lines.len(), 2, "cap of two kept");
        assert!(
            lines[0].contains("second"),
            "oldest (first) dropped, got {lines:?}"
        );
        assert!(lines[1].contains("third"), "newest kept, got {lines:?}");
    }

    #[test]
    fn flush_interval_pushes_without_reaching_batch_size() {
        let server = TestServer::start(vec![204]);
        let mut cfg = cfg_for(&server.url);
        cfg.flush_interval = Duration::from_millis(40);
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "tick"));
        let got = wait_for_requests(&server, 1);
        sink.shutdown();
        assert_eq!(got.len(), 1, "periodic flush fired before shutdown");
    }

    #[test]
    fn rejected_push_warns_once_with_the_status() {
        let warnings: Arc<StdMutex<Vec<(String, String)>>> = Arc::new(StdMutex::new(Vec::new()));
        let sink_warnings = Arc::clone(&warnings);
        let server = TestServer::start(vec![401, 403]);
        let mut cfg = cfg_for(&server.url);
        cfg.batch_size = 1;
        cfg.on_warn = Some(Arc::new(move |key: &str, detail: &str| {
            sink_warnings
                .lock()
                .expect("warnings")
                .push((key.to_string(), detail.to_string()));
        }));
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "a"));
        sink.export(&LogRecord::new(Level::Info, "b"));
        let _ = wait_for_requests(&server, 2);
        sink.shutdown();
        let got = lock_vec(&warnings);
        assert_eq!(got.len(), 1, "warning reported once for repeated failures");
        assert_eq!(got[0].0, WARN_KEY, "warning key");
        assert_eq!(
            got[0].1, "401 Unauthorized",
            "status detail from the first rejection"
        );
    }

    #[test]
    fn transport_error_warns_with_error_detail() {
        let warnings: Arc<StdMutex<Vec<(String, String)>>> = Arc::new(StdMutex::new(Vec::new()));
        let sink_warnings = Arc::clone(&warnings);
        // Port 1 on loopback refuses connections, so the push fails at the transport layer.
        let mut cfg = cfg_for("http://127.0.0.1:1");
        cfg.batch_size = 1;
        cfg.timeout = Duration::from_millis(500);
        cfg.on_warn = Some(Arc::new(move |key: &str, detail: &str| {
            sink_warnings
                .lock()
                .expect("warnings")
                .push((key.to_string(), detail.to_string()));
        }));
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "x"));
        sink.shutdown();
        let got = lock_vec(&warnings);
        assert_eq!(got.len(), 1, "one transport warning");
        assert_eq!(got[0].1, "error", "transport failure reported as error");
    }

    #[test]
    fn slow_server_hits_the_timeout_and_does_not_panic() {
        let server = TestServer::start(vec![0]);
        let mut cfg = cfg_for(&server.url);
        cfg.timeout = Duration::from_millis(150);
        cfg.batch_size = 1;
        let warnings: Arc<StdMutex<Vec<String>>> = Arc::new(StdMutex::new(Vec::new()));
        let sink_warnings = Arc::clone(&warnings);
        cfg.on_warn = Some(Arc::new(move |_key: &str, detail: &str| {
            sink_warnings
                .lock()
                .expect("warnings")
                .push(detail.to_string());
        }));
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "slow"));
        sink.shutdown();
        assert_eq!(
            lock_vec(&warnings),
            vec!["error".to_string()],
            "timeout surfaces as transport error"
        );
    }

    #[test]
    fn default_warning_goes_to_stderr_path_without_panicking() {
        let server = TestServer::start(vec![500]);
        let mut cfg = cfg_for(&server.url);
        cfg.batch_size = 1;
        let sink = LokiSink::new(cfg);
        sink.export(&LogRecord::new(Level::Info, "x"));
        sink.shutdown();
        assert_eq!(
            server.requests().len(),
            1,
            "server saw the push; 500 is not warned"
        );
    }

    #[test]
    fn dropping_the_sink_flushes_pending_records() {
        let server = TestServer::start(vec![204]);
        {
            let sink = LokiSink::new(cfg_for(&server.url));
            sink.export(&LogRecord::new(Level::Info, "on drop"));
        }
        let got = server.requests();
        assert_eq!(got.len(), 1, "drop runs shutdown and pushes the tail");
    }

    fn lock_vec<T: Clone>(m: &StdMutex<Vec<T>>) -> Vec<T> {
        m.lock().expect("vec lock").clone()
    }
}
