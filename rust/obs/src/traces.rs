//! OTLP/HTTP traces to `{url}/v1/traces`, with Basic auth and batch export. Export is fail-open:
//! the provider exists even when no exporter could be built.

use std::collections::HashMap;

use base64::Engine;
use base64::engine::general_purpose::STANDARD;
use opentelemetry::propagation::TextMapCompositePropagator;
use opentelemetry::{KeyValue, global};
use opentelemetry_otlp::{SpanExporter, WithExportConfig, WithHttpConfig};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::propagation::{BaggagePropagator, TraceContextPropagator};
use opentelemetry_sdk::trace::SdkTracerProvider;

use crate::config::{Config, DEFAULT_TIMEOUT};
use crate::identity::Identity;
use crate::remote_write::join_api;

/// Path appended to the base URL for OTLP traces.
pub const TRACES_PATH: &str = "/v1/traces";

/// The traces endpoint for a Gigapipe base URL.
pub fn traces_url(base: &str) -> String {
    join_api(base, TRACES_PATH)
}

/// `Authorization: Basic …`, or `None` when both credentials are empty.
pub fn basic_auth_header(username: &str, password: &str) -> Option<String> {
    if username.is_empty() && password.is_empty() {
        return None;
    }
    Some(format!(
        "Basic {}",
        STANDARD.encode(format!("{username}:{password}"))
    ))
}

/// The OTLP HTTP span exporter for `cfg`.
///
/// A zero timeout means "not configured" here, which maps to [`DEFAULT_TIMEOUT`]. Passing zero
/// through would hand reqwest a zero timeout, and every export would then fail immediately.
pub fn span_exporter(cfg: &Config) -> Result<SpanExporter, String> {
    let timeout = if cfg.timeout.is_zero() {
        DEFAULT_TIMEOUT
    } else {
        cfg.timeout
    };
    let mut builder = SpanExporter::builder()
        .with_http()
        .with_endpoint(traces_url(&cfg.url))
        .with_timeout(timeout);
    if let Some(auth) = basic_auth_header(&cfg.username, &cfg.password) {
        let mut headers = HashMap::new();
        headers.insert("Authorization".to_string(), auth);
        builder = builder.with_headers(headers);
    }
    builder.build().map_err(|err| err.to_string())
}

/// A provider with the service resource. When `export` is true, spans go out in batches.
/// Otherwise spans are created but not sent.
pub fn build_provider(
    cfg: &Config,
    identity: &Identity,
    export: bool,
) -> Result<SdkTracerProvider, String> {
    let mut resource = Resource::builder().with_service_name(identity.service_name.clone());
    for (key, value) in identity.resource_attributes() {
        if key == "service.name" {
            continue;
        }
        resource = resource.with_attribute(KeyValue::new(key, value));
    }
    let mut builder = SdkTracerProvider::builder().with_resource(resource.build());
    if export {
        builder = builder.with_batch_exporter(span_exporter(cfg)?);
    }
    Ok(builder.build())
}

/// Makes `provider` the global tracer provider, and installs W3C `tracecontext` and `baggage`
/// propagation. Off by default; see `Config::install_global`.
pub fn install_global(provider: &SdkTracerProvider) {
    global::set_tracer_provider(provider.clone());
    global::set_text_map_propagator(TextMapCompositePropagator::new(vec![
        Box::new(TraceContextPropagator::new()),
        Box::new(BaggagePropagator::new()),
    ]));
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::test_server::TestServer;
    use opentelemetry::trace::{Span, SpanKind, Tracer, TracerProvider};

    #[test]
    fn traces_url_appends_the_otlp_path_once() {
        assert_eq!(
            traces_url("https://g.example"),
            "https://g.example/v1/traces",
            "bare base"
        );
        assert_eq!(
            traces_url("https://g.example/v1/traces"),
            "https://g.example/v1/traces",
            "already complete"
        );
    }

    #[test]
    fn basic_auth_header_only_when_credentials_are_present() {
        assert_eq!(basic_auth_header("", ""), None, "no credentials, no header");
        assert_eq!(
            basic_auth_header("user", "pass"),
            Some("Basic dXNlcjpwYXNz".to_string()),
            "user:pass base64"
        );
        assert!(
            basic_auth_header("", "secret").is_some(),
            "password alone still authenticates"
        );
    }

    #[test]
    fn provider_without_export_builds_and_creates_spans() {
        let cfg = Config::default();
        let identity = Identity::resolve(&cfg);
        let provider = build_provider(&cfg, &identity, false).expect("provider builds");
        let tracer = provider.tracer("test");
        let mut span = tracer
            .span_builder("op")
            .with_kind(SpanKind::Internal)
            .start(&tracer);
        span.end();
        assert!(
            provider.shutdown().is_ok(),
            "clean shutdown with nothing exported"
        );
    }

    #[test]
    fn span_exporter_builds_for_a_valid_url() {
        let cfg = Config {
            url: "http://127.0.0.1:4318".to_string(),
            username: "u".to_string(),
            password: "p".to_string(),
            ..Config::default()
        };
        assert!(span_exporter(&cfg).is_ok(), "exporter builds with auth");
    }

    #[test]
    fn unset_timeout_is_replaced_by_the_default() {
        let server = TestServer::start(vec![200]);
        let cfg = Config {
            url: server.url.clone(),
            ..Config::default()
        };
        assert!(cfg.timeout.is_zero(), "unnormalized config has no timeout");
        let identity = Identity::resolve(&cfg);
        let provider = build_provider(&cfg, &identity, true).expect("provider builds");
        let tracer = provider.tracer("test");
        let mut span = tracer.span_builder("timeout-default").start(&tracer);
        span.end();
        assert!(
            provider.force_flush().is_ok(),
            "export succeeds with the default timeout"
        );
        assert!(
            !server.requests().is_empty(),
            "the request reached the server"
        );
        let _ = provider.shutdown();
    }

    #[test]
    fn batch_export_posts_otlp_to_the_traces_path_on_shutdown() {
        let server = TestServer::start(vec![200]);
        let cfg = Config {
            url: server.url.clone(),
            username: "user".to_string(),
            password: "pass".to_string(),
            ..Config::default()
        };
        let identity = Identity::resolve(&cfg);
        let provider = build_provider(&cfg, &identity, true).expect("provider builds");
        let tracer = provider.tracer("test");
        let mut span = tracer
            .span_builder("GET /x")
            .with_kind(SpanKind::Server)
            .start(&tracer);
        span.end();
        provider.shutdown().expect("shutdown flushes the batch");

        let got = server.requests();
        assert!(!got.is_empty(), "exporter posted at least once");
        assert_eq!(got[0].path, "/v1/traces", "otlp traces path");
        assert_eq!(
            got[0].headers.get("authorization").map(String::as_str),
            Some("Basic dXNlcjpwYXNz"),
            "basic auth on export"
        );
        assert!(!got[0].raw.is_empty(), "protobuf body present");
    }

    #[test]
    fn install_global_sets_the_provider_and_propagator() {
        let cfg = Config::default();
        let identity = Identity::resolve(&cfg);
        let provider = build_provider(&cfg, &identity, false).expect("provider builds");
        install_global(&provider);
        let tracer = global::tracer("global-test");
        let mut span = tracer.start("global");
        span.end();
        assert!(provider.shutdown().is_ok(), "global provider shuts down");
    }
}
