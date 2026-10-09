//! Prometheus remote write. Snappy-compressed protobuf is POSTed to
//! `{url}/api/v1/prom/remote/write`. Mirrors `go/obs/prometheus.go` and `go/obs/obs.go`
//! `loopMetrics`.

use std::sync::Arc;
use std::sync::mpsc::{self, RecvTimeoutError, Sender};
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use prost::Message;
use reqwest::blocking::Client;
use reqwest::header::{CONTENT_ENCODING, CONTENT_TYPE, USER_AGENT};

use crate::metrics::{Gatherer, write_request};
use crate::prompb::WriteRequest;
use crate::warn::Warner;

/// Path appended to the Gigapipe base URL for remote write.
pub const REMOTE_WRITE_PATH: &str = "/api/v1/prom/remote/write";
/// `User-Agent` for remote-write pushes (`go/obs`: `auth-sdks-obs/1`).
pub const USER_AGENT_VALUE: &str = "auth-sdks-obs/1";
/// Warning key for failed writes, as in Go.
pub const WARN_WRITE_FAILED: &str = "gigapipe prometheus write failed";

/// Appends `suffix` to `base` unless `base` already ends with it. A trailing slash is trimmed.
pub(crate) fn join_api(base: &str, suffix: &str) -> String {
    let base = base.trim().trim_end_matches('/');
    if base.ends_with(suffix) {
        base.to_string()
    } else {
        format!("{base}{suffix}")
    }
}

/// The remote-write endpoint for a Gigapipe base URL.
pub fn remote_write_url(base: &str) -> String {
    join_api(base, REMOTE_WRITE_PATH)
}

/// Where and as whom to push.
pub(crate) struct Remote {
    pub(crate) url: String,
    pub(crate) username: String,
    pub(crate) password: String,
    pub(crate) identity_labels: Vec<(String, String)>,
}

/// Snappy-compresses the protobuf encoding of a request.
pub fn encode(req: &WriteRequest) -> Result<Vec<u8>, snap::Error> {
    snap::raw::Encoder::new().compress_vec(&req.encode_to_vec())
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| i64::try_from(d.as_millis()).unwrap_or(i64::MAX))
        .unwrap_or(0)
}

/// One gather-encode-POST cycle. A failure is reported through `warner` and never returned.
pub(crate) fn push_once(client: &Client, remote: &Remote, gatherer: &Gatherer, warner: &Warner) {
    let families = gatherer.gather();
    let req = write_request(&families, &remote.identity_labels, now_ms());
    let body = match encode(&req) {
        Ok(body) => body,
        Err(err) => {
            warner.warn(WARN_WRITE_FAILED, &err.to_string());
            return;
        }
    };
    let mut request = client
        .post(&remote.url)
        .header(CONTENT_ENCODING, "snappy")
        .header(CONTENT_TYPE, "application/x-protobuf")
        .header("X-Prometheus-Remote-Write-Version", "0.1.0")
        .header(USER_AGENT, USER_AGENT_VALUE)
        .body(body);
    if !remote.username.is_empty() || !remote.password.is_empty() {
        request = request.basic_auth(&remote.username, Some(&remote.password));
    }
    match request.send() {
        Err(err) => warner.warn(WARN_WRITE_FAILED, &err.to_string()),
        Ok(response) if !response.status().is_success() => {
            warner.warn(WARN_WRITE_FAILED, &response.status().to_string());
        }
        Ok(_) => {}
    }
}

/// A background thread that pushes on start, on every interval, and once more on stop.
pub(crate) struct MetricsWorker {
    stop: Sender<()>,
    join: JoinHandle<()>,
}

impl MetricsWorker {
    /// Signals the thread, waits for its final push, and joins it.
    pub(crate) fn stop(self) {
        let _ = self.stop.send(());
        let _ = self.join.join();
    }
}

/// Starts the metrics thread. It returns `None` only if the OS refuses to spawn it. The blocking
/// HTTP client is built inside the thread, so no client is ever dropped on an async runtime.
pub(crate) fn start(
    gatherer: Arc<Gatherer>,
    remote: Remote,
    warner: Arc<Warner>,
    interval: Duration,
    timeout: Duration,
) -> Option<MetricsWorker> {
    let (stop, stop_rx) = mpsc::channel::<()>();
    let spawned = thread::Builder::new()
        .name("obs-metrics".to_string())
        .spawn(move || {
            let client = match Client::builder().timeout(timeout).build() {
                Ok(client) => client,
                Err(err) => {
                    warner.warn(WARN_WRITE_FAILED, &err.to_string());
                    return;
                }
            };
            push_once(&client, &remote, &gatherer, &warner);
            while let Err(RecvTimeoutError::Timeout) = stop_rx.recv_timeout(interval) {
                push_once(&client, &remote, &gatherer, &warner);
            }
            push_once(&client, &remote, &gatherer, &warner);
        });
    spawned.ok().map(|join| MetricsWorker { stop, join })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::collectors::counter_family;
    use crate::test_server::TestServer;
    use prometheus::Registry;
    use prost::Message;

    fn remote_for(url: &str) -> Remote {
        Remote {
            url: remote_write_url(url),
            username: String::new(),
            password: String::new(),
            identity_labels: vec![("service".to_string(), "svc".to_string())],
        }
    }

    fn gatherer_with_counter() -> Arc<Gatherer> {
        Arc::new(Gatherer::new(
            Registry::new(),
            Registry::new(),
            vec![Box::new(|| {
                vec![counter_family("hits_total", "h", vec![(Vec::new(), 4.0)])]
            })],
        ))
    }

    fn warner_capturing() -> (Arc<Warner>, Arc<std::sync::Mutex<Vec<String>>>) {
        let log = Arc::new(std::sync::Mutex::new(Vec::new()));
        let sink = Arc::clone(&log);
        let hook: crate::config::WarnFn = Arc::new(move |key: &str, detail: &str| {
            sink.lock().expect("log").push(format!("{key}|{detail}"));
        });
        (Arc::new(Warner::new(Some(hook))), log)
    }

    fn decode_body(body: &[u8]) -> WriteRequest {
        let raw = snap::raw::Decoder::new()
            .decompress_vec(body)
            .expect("snappy body");
        WriteRequest::decode(raw.as_slice()).expect("protobuf body")
    }

    #[test]
    fn remote_write_url_appends_the_path_once() {
        assert_eq!(
            remote_write_url("https://g.example"),
            "https://g.example/api/v1/prom/remote/write",
            "bare base"
        );
        assert_eq!(
            remote_write_url("https://g.example/"),
            "https://g.example/api/v1/prom/remote/write",
            "slash"
        );
        assert_eq!(
            remote_write_url("https://g.example/api/v1/prom/remote/write"),
            "https://g.example/api/v1/prom/remote/write",
            "already complete"
        );
    }

    #[test]
    fn encode_round_trips_through_snappy_and_protobuf() {
        let req = WriteRequest::default();
        let packed = encode(&req).expect("encode");
        let raw = snap::raw::Decoder::new()
            .decompress_vec(&packed)
            .expect("decompress");
        assert_eq!(
            WriteRequest::decode(raw.as_slice()).expect("decode"),
            req,
            "lossless"
        );
    }

    #[test]
    fn push_once_sends_snappy_protobuf_with_headers_and_auth() {
        let server = TestServer::start(vec![204]);
        let mut remote = remote_for(&server.url);
        remote.username = "user".to_string();
        remote.password = "pass".to_string();
        let (warner, log) = warner_capturing();
        let client = Client::new();
        push_once(&client, &remote, &gatherer_with_counter(), &warner);

        let got = server.requests();
        assert_eq!(got.len(), 1, "one push");
        let req = &got[0];
        assert_eq!(req.path, "/api/v1/prom/remote/write", "remote write path");
        assert_eq!(
            req.headers.get("content-encoding").map(String::as_str),
            Some("snappy"),
            "snappy header"
        );
        assert_eq!(
            req.headers.get("content-type").map(String::as_str),
            Some("application/x-protobuf"),
            "protobuf content type"
        );
        assert_eq!(
            req.headers
                .get("x-prometheus-remote-write-version")
                .map(String::as_str),
            Some("0.1.0"),
            "version header"
        );
        assert_eq!(
            req.headers.get("user-agent").map(String::as_str),
            Some("auth-sdks-obs/1"),
            "user agent"
        );
        assert_eq!(
            req.headers.get("authorization").map(String::as_str),
            Some("Basic dXNlcjpwYXNz"),
            "basic auth"
        );
        let decoded = decode_body(&req.raw);
        assert!(
            decoded
                .timeseries
                .iter()
                .any(|ts| ts.labels.iter().any(|l| l.value == "hits_total")),
            "counter present in body"
        );
        assert!(
            log.lock().expect("log").is_empty(),
            "successful push warns nothing"
        );
    }

    #[test]
    fn push_once_without_credentials_sends_no_authorization() {
        let server = TestServer::start(vec![204]);
        let (warner, _) = warner_capturing();
        push_once(
            &Client::new(),
            &remote_for(&server.url),
            &gatherer_with_counter(),
            &warner,
        );
        assert!(
            !server.requests()[0].headers.contains_key("authorization"),
            "no auth header"
        );
    }

    #[test]
    fn rejected_push_reports_the_status_once() {
        let server = TestServer::start(vec![500, 500]);
        let (warner, log) = warner_capturing();
        let client = Client::new();
        push_once(
            &client,
            &remote_for(&server.url),
            &gatherer_with_counter(),
            &warner,
        );
        push_once(
            &client,
            &remote_for(&server.url),
            &gatherer_with_counter(),
            &warner,
        );
        let got = log.lock().expect("log").clone();
        assert_eq!(
            got,
            vec![format!("{WARN_WRITE_FAILED}|500 Internal Server Error")],
            "one warning, with status"
        );
    }

    #[test]
    fn transport_failure_is_reported_not_raised() {
        let (warner, log) = warner_capturing();
        let remote = remote_for("http://127.0.0.1:1");
        push_once(&Client::new(), &remote, &gatherer_with_counter(), &warner);
        assert_eq!(log.lock().expect("log").len(), 1, "one transport warning");
    }

    #[test]
    fn worker_pushes_on_start_and_again_on_stop() {
        let server = TestServer::start(vec![204]);
        let (warner, _) = warner_capturing();
        let worker = start(
            gatherer_with_counter(),
            remote_for(&server.url),
            warner,
            Duration::from_secs(600),
            Duration::from_secs(5),
        )
        .expect("worker starts");
        // Give the immediate push time to land before stopping.
        let deadline = std::time::Instant::now() + Duration::from_secs(5);
        while server.requests().is_empty() && std::time::Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }
        worker.stop();
        assert_eq!(
            server.requests().len(),
            2,
            "immediate push plus final push on stop"
        );
    }

    #[test]
    fn worker_pushes_on_each_interval() {
        let server = TestServer::start(vec![204]);
        let (warner, _) = warner_capturing();
        let worker = start(
            gatherer_with_counter(),
            remote_for(&server.url),
            warner,
            Duration::from_millis(30),
            Duration::from_secs(5),
        )
        .expect("worker starts");
        let deadline = std::time::Instant::now() + Duration::from_secs(5);
        while server.requests().len() < 3 && std::time::Instant::now() < deadline {
            thread::sleep(Duration::from_millis(10));
        }
        worker.stop();
        assert!(server.requests().len() >= 3, "interval pushes fired");
    }

    #[test]
    fn now_ms_is_after_2020() {
        assert!(now_ms() > 1_577_836_800_000, "milliseconds since epoch");
    }
}
