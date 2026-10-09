//! The runtime handle.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};

use opentelemetry::trace::TracerProvider;
use opentelemetry_sdk::trace::{SdkTracer, SdkTracerProvider};
use prometheus::proto::MetricFamily;
use prometheus::{Encoder, Registry, TextEncoder};
use pyroscope::pyroscope::{PyroscopeAgent, PyroscopeAgentRunning};

use crate::collectors;
use crate::config::Config;
use crate::hop::HopMetrics;
use crate::identity::Identity;
use crate::metrics::{Gatherer, Producer};
use crate::profiles;
use crate::remote_write::{self, MetricsWorker, Remote};
use crate::traces;
use crate::warn::Warner;

/// Name of the tracer used for hop spans.
pub const TRACER_NAME: &str = "obs";

/// The Gigapipe runtime for one process: metrics, traces, and profiles.
///
/// Build it once at startup. Share it as `Arc<Handle>` across request handlers. Call
/// [`Handle::shutdown`] before exit to push the last metrics and flush spans and profiles. Drop
/// also shuts down.
///
/// An empty `Config::url` makes it a local-only handle. The registry still works, and nothing is
/// sent. Every failure is fail-open: the caller is never blocked or panicked.
pub struct Handle {
    identity: Identity,
    gatherer: Arc<Gatherer>,
    hop: Arc<HopMetrics>,
    warner: Arc<Warner>,
    tracer_provider: SdkTracerProvider,
    tracer: SdkTracer,
    metrics_worker: Mutex<Option<MetricsWorker>>,
    profiler: Mutex<Option<PyroscopeAgent<PyroscopeAgentRunning>>>,
    closed: AtomicBool,
}

impl Handle {
    /// Starts the enabled signals. Remote-write pushes once right away, so the service is visible
    /// without waiting for an interval.
    pub fn new(cfg: Config) -> Handle {
        let cfg = cfg.normalized();
        let identity = Identity::resolve(&cfg);
        let warner = Arc::new(Warner::new(cfg.on_warn.clone()));

        let registry = Registry::new();
        let defaults = Registry::new();
        let hop = HopMetrics::register(&registry);

        let mut producers: Vec<Producer> = Vec::new();
        let target = identity.clone();
        producers.push(Box::new(move || vec![collectors::target_info(&target)]));
        if !cfg.disable_default_collectors {
            collectors::register_defaults(&defaults);
            producers.push(Box::new(collectors::process_io));
            let paths = cfg.filesystem_paths.clone();
            producers.push(Box::new(move || collectors::filesystem(&paths)));
        }
        let gatherer = Arc::new(Gatherer::new(registry, defaults, producers));

        let remote_on = !cfg.url.trim().is_empty();
        let traces_on = remote_on && !cfg.disable_traces;
        let tracer_provider = match traces::build_provider(&cfg, &identity, traces_on) {
            Ok(provider) => provider,
            Err(err) => {
                warner.warn("gigapipe tempo start failed", &err);
                traces::build_provider(&cfg, &identity, false)
                    .expect("provider without exporter always builds")
            }
        };
        let tracer = tracer_provider.tracer(TRACER_NAME);
        if cfg.install_global && traces_on {
            traces::install_global(&tracer_provider);
        }

        let metrics_worker = if remote_on && !cfg.disable_metrics {
            let remote = Remote {
                url: remote_write::remote_write_url(&cfg.url),
                username: cfg.username.clone(),
                password: cfg.password.clone(),
                identity_labels: identity.labels(),
            };
            remote_write::start(
                Arc::clone(&gatherer),
                remote,
                Arc::clone(&warner),
                cfg.metrics_interval,
                cfg.timeout,
            )
        } else {
            None
        };

        let profiler = if remote_on && !cfg.disable_profiles {
            match profiles::start(&cfg, &identity) {
                Ok(agent) => Some(agent),
                Err(err) => {
                    warner.warn("gigapipe pyroscope start failed", &err);
                    None
                }
            }
        } else {
            None
        };

        Handle {
            identity,
            gatherer,
            hop,
            warner,
            tracer_provider,
            tracer,
            metrics_worker: Mutex::new(metrics_worker),
            profiler: Mutex::new(profiler),
            closed: AtomicBool::new(false),
        }
    }

    /// The application registry. Register your own collectors here.
    /// Your families take precedence over default families with the same name.
    pub fn registry(&self) -> &Registry {
        &self.gatherer.registry
    }

    /// The tracer used for hop spans. Use it to start your own spans.
    pub fn tracer(&self) -> &SdkTracer {
        &self.tracer
    }

    /// The resolved service identity shared by metrics, traces, and profiles.
    pub fn identity(&self) -> &Identity {
        &self.identity
    }

    /// The merged metric families that are sent to Gigapipe.
    pub fn gather(&self) -> Vec<MetricFamily> {
        self.gatherer.gather()
    }

    /// The merged families in the Prometheus text format, for a `/metrics` route.
    pub fn metrics_text(&self) -> String {
        let families = self.gather();
        let mut buffer = Vec::new();
        if TextEncoder::new().encode(&families, &mut buffer).is_err() {
            return String::new();
        }
        String::from_utf8(buffer).unwrap_or_default()
    }

    /// Stops the metrics thread after its final push, stops the profiler, and flushes spans.
    /// Calling it again does nothing.
    pub fn shutdown(&self) {
        if self.closed.swap(true, Ordering::SeqCst) {
            return;
        }
        if let Some(worker) = lock(&self.metrics_worker).take() {
            worker.stop();
        }
        let agent = lock(&self.profiler).take();
        if let Some(Ok(ready)) = agent.map(|agent| agent.stop()) {
            ready.shutdown();
        }
        if let Err(err) = self.tracer_provider.shutdown() {
            self.warner
                .warn("gigapipe tempo shutdown failed", &err.to_string());
        }
    }

    pub(crate) fn hop_metrics(&self) -> Arc<HopMetrics> {
        Arc::clone(&self.hop)
    }
}

impl Drop for Handle {
    fn drop(&mut self) {
        self.shutdown();
    }
}

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|e| e.into_inner())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::hop::{Hop, METRIC_CLIENT_REQUEST_DURATION};
    use crate::remote_write::WARN_WRITE_FAILED;
    use crate::test_server::TestServer;
    use std::time::{Duration, Instant};

    fn local(service: &str) -> Config {
        Config {
            service: service.to_string(),
            env: "test".to_string(),
            disable_profiles: true,
            ..Config::default()
        }
    }

    fn family_names(handle: &Handle) -> Vec<String> {
        handle
            .gather()
            .iter()
            .map(|mf| mf.name().to_string())
            .collect()
    }

    #[test]
    fn local_handle_exposes_target_info_without_sending_anything() {
        let handle = Handle::new(local("money-tracker"));
        assert!(
            family_names(&handle).contains(&"target_info".to_string()),
            "target_info present"
        );
        assert_eq!(
            handle.identity().service_name,
            "money-tracker",
            "identity resolved"
        );
    }

    #[test]
    fn metrics_text_is_prometheus_text_format() {
        let handle = Handle::new(local("svc"));
        let text = handle.metrics_text();
        assert!(
            text.contains("# TYPE target_info gauge"),
            "text exposition: {text}"
        );
        assert!(text.contains("target_info{"), "target_info series present");
    }

    #[test]
    fn application_metrics_register_and_appear_in_gather() {
        let handle = Handle::new(local("svc"));
        let counter = prometheus::IntCounter::new("app_requests_total", "app").expect("counter");
        handle
            .registry()
            .register(Box::new(counter.clone()))
            .expect("register");
        counter.inc_by(3);
        assert!(
            family_names(&handle).contains(&"app_requests_total".to_string()),
            "app family gathered"
        );
    }

    #[test]
    fn hop_histogram_is_registered_on_the_application_registry() {
        let handle = Handle::new(local("svc"));
        handle
            .hop_metrics()
            .observe(&Hop::new("db", "x", "p"), "ok", 0.01);
        assert!(
            family_names(&handle).contains(&METRIC_CLIENT_REQUEST_DURATION.to_string()),
            "hop family present"
        );
    }

    #[test]
    fn default_collectors_can_be_disabled() {
        let handle = Handle::new(Config {
            disable_default_collectors: true,
            ..local("svc")
        });
        let names = family_names(&handle);
        assert!(
            !names.iter().any(|n| n.starts_with("process_filesystem")),
            "no filesystem family"
        );
        assert!(
            names.contains(&"target_info".to_string()),
            "target_info still emitted"
        );
    }

    #[test]
    fn shutdown_is_idempotent_and_drop_is_safe() {
        let handle = Handle::new(local("svc"));
        handle.shutdown();
        handle.shutdown();
        drop(handle);
    }

    #[test]
    fn remote_write_pushes_on_start_and_on_shutdown() {
        let server = TestServer::start(vec![204]);
        let handle = Handle::new(Config {
            url: server.url.clone(),
            username: "user".to_string(),
            password: "pass".to_string(),
            disable_traces: true,
            disable_profiles: true,
            metrics_interval: Duration::from_secs(600),
            ..local("svc")
        });
        let deadline = Instant::now() + Duration::from_secs(5);
        while server.requests().is_empty() && Instant::now() < deadline {
            std::thread::sleep(Duration::from_millis(10));
        }
        handle.shutdown();
        let got = server.requests();
        assert!(
            got.len() >= 2,
            "push on start plus final push: got {}",
            got.len()
        );
        assert_eq!(
            got[0].path, "/api/v1/prom/remote/write",
            "remote write path"
        );
        assert_eq!(
            got[0].headers.get("authorization").map(String::as_str),
            Some("Basic dXNlcjpwYXNz"),
            "basic auth on push"
        );
    }

    #[test]
    fn disabled_metrics_send_nothing() {
        let server = TestServer::start(vec![204]);
        let handle = Handle::new(Config {
            url: server.url.clone(),
            disable_metrics: true,
            disable_traces: true,
            disable_profiles: true,
            ..local("svc")
        });
        handle.shutdown();
        assert!(
            server.requests().is_empty(),
            "no metrics push when disabled"
        );
    }

    #[test]
    fn unreachable_gigapipe_is_fail_open() {
        let handle = Handle::new(Config {
            url: "http://127.0.0.1:1".to_string(),
            disable_traces: true,
            disable_profiles: true,
            ..local("svc")
        });
        handle.shutdown();
        assert!(
            family_names(&handle).contains(&"target_info".to_string()),
            "local metrics intact"
        );
    }

    #[test]
    fn profiler_failure_is_reported_once_and_does_not_panic() {
        let warnings: Arc<Mutex<Vec<String>>> = Arc::new(Mutex::new(Vec::new()));
        let sink = Arc::clone(&warnings);
        let handle = Handle::new(Config {
            url: "::bad::".to_string(),
            disable_metrics: true,
            disable_traces: true,
            disable_profiles: false,
            on_warn: Some(Arc::new(move |key: &str, _detail: &str| {
                sink.lock().expect("warnings").push(key.to_string());
            })),
            ..local("svc")
        });
        handle.shutdown();
        assert_eq!(
            warnings.lock().expect("warnings").clone(),
            vec!["gigapipe pyroscope start failed".to_string()],
            "pyroscope start warning"
        );
    }

    #[test]
    fn tracer_export_failure_falls_back_to_a_local_provider() {
        let handle = Handle::new(Config {
            url: "http://127.0.0.1:4318".to_string(),
            disable_metrics: true,
            disable_profiles: true,
            ..local("svc")
        });
        let tracer = handle.tracer();
        let _ = tracer;
        handle.shutdown();
    }

    #[test]
    fn install_global_option_wires_the_global_provider() {
        let handle = Handle::new(Config {
            url: "http://127.0.0.1:4318".to_string(),
            install_global: true,
            disable_metrics: true,
            disable_profiles: true,
            ..local("svc")
        });
        handle.shutdown();
    }

    #[test]
    fn a_warning_hook_is_called_for_failed_remote_writes() {
        let warnings: Arc<Mutex<Vec<String>>> = Arc::new(Mutex::new(Vec::new()));
        let sink = Arc::clone(&warnings);
        let handle = Handle::new(Config {
            url: "http://127.0.0.1:1".to_string(),
            disable_traces: true,
            disable_profiles: true,
            on_warn: Some(Arc::new(move |key: &str, _detail: &str| {
                sink.lock().expect("warnings").push(key.to_string());
            })),
            ..local("svc")
        });
        handle.shutdown();
        assert!(
            warnings
                .lock()
                .expect("warnings")
                .iter()
                .any(|k| k == WARN_WRITE_FAILED),
            "write failure reported through the hook"
        );
    }
}
