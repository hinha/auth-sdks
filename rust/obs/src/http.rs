//! HTTP instrumentation. [`ServerLayer`] wraps an inbound `tower` service (axum, hyper, tower).
//! [`execute`] runs an outbound `reqwest` request as a hop. Mirrors `go/obs/http.go` and
//! `hop_http.go`.

use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context as TaskContext, Poll};

use http::{Request, Response};
use opentelemetry::KeyValue;
use opentelemetry::trace::FutureExt;
use tower::{Layer, Service};

use crate::handle::Handle;
use crate::hop::{COMPONENT_HTTP_CLIENT, COMPONENT_HTTP_SERVER, Hop, UNKNOWN, http_status_label};

/// Maps `(method, path)` to a route template for the `operation` label. Templates keep label
/// cardinality bounded, so a raw path with IDs must never be returned.
pub type RouteFn = Arc<dyn Fn(&str, &str) -> String + Send + Sync>;

/// Route label when no resolver is set. Go uses the literal `unmatched`, never the URL path.
pub const UNMATCHED_ROUTE: &str = "unmatched";

fn unmatched(_method: &str, _path: &str) -> String {
    UNMATCHED_ROUTE.to_string()
}

/// Records every inbound request as one `http_server` hop. Build it once at startup.
#[derive(Clone)]
pub struct ServerLayer {
    handle: Arc<Handle>,
    route: RouteFn,
}

impl ServerLayer {
    /// A layer whose route is `unmatched`, as in Go's default. Use [`ServerLayer::with_route`] to
    /// report templates.
    pub fn new(handle: Arc<Handle>) -> Self {
        Self {
            handle,
            route: Arc::new(unmatched),
        }
    }

    /// Sets the route resolver, e.g. from the framework's matched-route extension.
    pub fn with_route(
        mut self,
        route: impl Fn(&str, &str) -> String + Send + Sync + 'static,
    ) -> Self {
        self.route = Arc::new(route);
        self
    }
}

impl<S> Layer<S> for ServerLayer {
    type Service = ServerService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        ServerService {
            inner,
            handle: Arc::clone(&self.handle),
            route: Arc::clone(&self.route),
        }
    }
}

/// The service produced by [`ServerLayer`].
#[derive(Clone)]
pub struct ServerService<S> {
    inner: S,
    handle: Arc<Handle>,
    route: RouteFn,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for ServerService<S>
where
    S: Service<Request<ReqBody>, Response = Response<ResBody>>,
    S::Future: Send + 'static,
    S::Error: Send + 'static,
    ResBody: Send + 'static,
{
    type Response = Response<ResBody>;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut TaskContext<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<ReqBody>) -> Self::Future {
        let method = req.method().as_str().to_string();
        let path = req.uri().path().to_string();
        let operation = format!("{method} {}", (self.route)(&method, &path));
        let hop = Hop::new(COMPONENT_HTTP_SERVER, operation, "");
        let timer = self.handle.start_hop(&hop, Vec::new());
        let fut = self.inner.call(req);
        Box::pin(async move {
            let result = fut.with_context(timer.context()).await;
            match &result {
                Ok(response) => timer.finish(&http_status_label(response.status().as_u16()), None),
                Err(_) => timer.finish("500", Some("service error")),
            }
            result
        })
    }
}

/// Sends an outbound request as one `http_client` hop. An empty `peer` falls back to the request's
/// `host:port`, the virtual node Go uses. Status is the response code, or `500` on a transport
/// error. The `server.address` attribute carries the host, without path or query.
pub async fn execute(
    handle: &Handle,
    peer: &str,
    client: &reqwest::Client,
    request: reqwest::Request,
) -> Result<reqwest::Response, reqwest::Error> {
    let method = request.method().as_str().to_string();
    let url = request.url().clone();
    let host = url.host_str().unwrap_or(UNKNOWN).to_string();
    let peer = if peer.trim().is_empty() {
        match url.port() {
            Some(port) => format!("{host}:{port}"),
            None => host.clone(),
        }
    } else {
        peer.to_string()
    };
    let hop = Hop::new(COMPONENT_HTTP_CLIENT, method, peer);
    let timer = handle.start_hop(&hop, vec![KeyValue::new("server.address", host)]);
    let result = client.execute(request).with_context(timer.context()).await;
    match &result {
        Ok(response) => timer.finish(&http_status_label(response.status().as_u16()), None),
        Err(err) => timer.finish("500", Some(&err.to_string())),
    }
    result
}

/// Runs an outbound send that has already been started as one `http_client` hop. The status label
/// is the response code, as in Go, or `500` on a transport error. The response or error is
/// returned unchanged.
pub async fn observe_send<Fut, E>(
    handle: &Handle,
    peer: &str,
    method: &str,
    send: Fut,
) -> Result<reqwest::Response, E>
where
    Fut: Future<Output = Result<reqwest::Response, E>>,
    E: std::fmt::Display,
{
    let hop = Hop::new(COMPONENT_HTTP_CLIENT, method, peer);
    let timer = handle.start_hop(&hop, Vec::new());
    let result = send.with_context(timer.context()).await;
    match &result {
        Ok(response) => timer.finish(&http_status_label(response.status().as_u16()), None),
        Err(err) => timer.finish("500", Some(&err.to_string())),
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;
    use crate::hop::METRIC_CLIENT_REQUEST_DURATION;
    use crate::test_server::TestServer;
    use std::convert::Infallible;
    use tower::ServiceExt;

    #[tokio::test]
    async fn observe_send_labels_the_hop_with_the_response_code() {
        let server = TestServer::start(vec![503]);
        let handle = handle();
        let client = reqwest::Client::new();
        let send = client
            .post(format!("{}/v1/messages", server.url))
            .body("{}")
            .send();
        let response = observe_send(&handle, "upstream.example", "POST", send)
            .await
            .expect("response");
        assert_eq!(response.status(), 503, "response passes through");
        let samples = hop_samples(&handle);
        assert!(
            samples
                .iter()
                .any(|s| has(s, "peer", "upstream.example") && has(s, "status", "503")),
            "status is the response code: {samples:?}"
        );
    }

    #[tokio::test]
    async fn observe_send_returns_transport_errors_unchanged() {
        let handle = handle();
        let client = reqwest::Client::new();
        let send = client.post("http://127.0.0.1:1/down").send();
        let result = observe_send(&handle, "down", "POST", send).await;
        assert!(result.is_err(), "transport error returned");
        let samples = hop_samples(&handle);
        assert!(
            samples
                .iter()
                .any(|s| has(s, "peer", "down") && has(s, "status", "500")),
            "transport failure recorded as 500: {samples:?}"
        );
    }

    fn handle() -> Arc<Handle> {
        Arc::new(Handle::new(Config {
            disable_default_collectors: true,
            disable_profiles: true,
            ..Config::default()
        }))
    }

    fn hop_samples(handle: &Handle) -> Vec<Vec<(String, String)>> {
        handle
            .gather()
            .iter()
            .filter(|mf| mf.name() == METRIC_CLIENT_REQUEST_DURATION)
            .flat_map(|mf| mf.get_metric().iter())
            .map(|m| {
                m.get_label()
                    .iter()
                    .map(|l| (l.name().to_string(), l.value().to_string()))
                    .collect()
            })
            .collect()
    }

    fn has(labels: &[(String, String)], name: &str, value: &str) -> bool {
        labels.iter().any(|(n, v)| n == name && v == value)
    }

    #[tokio::test]
    async fn server_layer_records_method_route_and_status() {
        let handle = handle();
        let layer = ServerLayer::new(Arc::clone(&handle)).with_route(|_method, path| {
            if path.starts_with("/notes/") {
                "/notes/{id}".to_string()
            } else {
                path.to_string()
            }
        });
        let inner = tower::service_fn(|_req: Request<String>| async {
            Ok::<_, Infallible>(
                Response::builder()
                    .status(201)
                    .body(String::new())
                    .expect("response"),
            )
        });
        let mut svc = layer.layer(inner);
        let req = Request::builder()
            .method("GET")
            .uri("/notes/42")
            .body(String::new())
            .expect("request");
        let response = svc
            .ready()
            .await
            .expect("ready")
            .call(req)
            .await
            .expect("call");
        assert_eq!(response.status(), 201, "inner response passes through");

        let samples = hop_samples(&handle);
        assert!(
            samples.iter().any(|s| has(s, "component", "http_server")
                && has(s, "operation", "GET /notes/{id}")
                && has(s, "status", "201")),
            "server hop recorded with template and status: {samples:?}"
        );
    }

    #[tokio::test]
    async fn server_layer_defaults_to_unmatched_and_records_errors_as_500() {
        let handle = handle();
        let layer = ServerLayer::new(Arc::clone(&handle));
        let inner = tower::service_fn(|_req: Request<String>| async {
            Err::<Response<String>, &'static str>("upstream gone")
        });
        let mut svc = layer.layer(inner);
        let req = Request::builder()
            .method("POST")
            .uri("/x/1")
            .body(String::new())
            .expect("request");
        let result = svc.ready().await.expect("ready").call(req).await;
        assert!(result.is_err(), "error propagates");

        let samples = hop_samples(&handle);
        assert!(
            samples
                .iter()
                .any(|s| has(s, "operation", "POST unmatched") && has(s, "status", "500")),
            "unmatched route and 500 recorded: {samples:?}"
        );
    }

    #[tokio::test]
    async fn execute_records_an_outbound_hop_with_peer_and_status() {
        let server = TestServer::start(vec![200]);
        let handle = handle();
        let client = reqwest::Client::new();
        let request = client
            .get(format!("{}/health", server.url))
            .build()
            .expect("request");
        let response = execute(&handle, "auth-service", &client, request)
            .await
            .expect("send");
        assert_eq!(response.status(), 200, "status returned");

        let samples = hop_samples(&handle);
        assert!(
            samples.iter().any(|s| has(s, "component", "http_client")
                && has(s, "operation", "GET")
                && has(s, "peer", "auth-service")
                && has(s, "status", "200")),
            "client hop recorded: {samples:?}"
        );
    }

    #[tokio::test]
    async fn execute_defaults_peer_to_host_and_port() {
        let server = TestServer::start(vec![204]);
        let handle = handle();
        let client = reqwest::Client::new();
        let request = client
            .post(format!("{}/x", server.url))
            .build()
            .expect("request");
        let _ = execute(&handle, "", &client, request).await.expect("send");
        let samples = hop_samples(&handle);
        let port = server.url.rsplit(':').next().expect("port").to_string();
        assert!(
            samples
                .iter()
                .any(|s| has(s, "peer", &format!("127.0.0.1:{port}"))),
            "peer falls back to host:port: {samples:?}"
        );
    }

    #[tokio::test]
    async fn execute_records_transport_failure_as_500() {
        let handle = handle();
        let client = reqwest::Client::new();
        let request = client
            .get("http://127.0.0.1:1/down")
            .build()
            .expect("request");
        let result = execute(&handle, "down-svc", &client, request).await;
        assert!(result.is_err(), "transport error returned");
        let samples = hop_samples(&handle);
        assert!(
            samples
                .iter()
                .any(|s| has(s, "peer", "down-svc") && has(s, "status", "500")),
            "transport failure recorded as 500: {samples:?}"
        );
    }
}
