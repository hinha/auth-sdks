//! Continuous CPU profiles pushed to Pyroscope's `/ingest` API, as Gigapipe documents. The agent
//! is the `pyroscope` 0.5 line with the pprof backend. The 2.x line pushes to a Connect RPC path
//! that Gigapipe does not expose, so it is not used.

use pyroscope::pyroscope::{PyroscopeAgent, PyroscopeAgentBuilder, PyroscopeAgentRunning};
use pyroscope_pprofrs::{PprofConfig, pprof_backend};

use crate::config::Config;
use crate::identity::Identity;

/// Sampling frequency in Hz.
pub const SAMPLE_RATE_HZ: u32 = 100;

/// The origin (`scheme://host[:port]`) of a Gigapipe URL. The agent appends `/ingest` itself, so a
/// base URL with a path would otherwise produce a bad endpoint.
pub fn origin_url(raw: &str) -> Result<String, String> {
    let url = reqwest::Url::parse(raw.trim()).map_err(|err| err.to_string())?;
    let host = url
        .host_str()
        .ok_or_else(|| "url has no host".to_string())?;
    let port = url.port().map(|p| format!(":{p}")).unwrap_or_default();
    Ok(format!("{}://{host}{port}", url.scheme()))
}

/// Starts the profiler. The tags are the identity labels, plus `service_version` when known, so
/// profiles and metrics agree on which service they belong to.
pub fn start(
    cfg: &Config,
    identity: &Identity,
) -> Result<PyroscopeAgent<PyroscopeAgentRunning>, String> {
    let server = origin_url(&cfg.url)?;
    let mut tags: Vec<(String, String)> = identity.labels();
    if !identity.service_version.is_empty() {
        tags.push((
            "service_version".to_string(),
            identity.service_version.clone(),
        ));
    }
    let tag_refs: Vec<(&str, &str)> = tags.iter().map(|(k, v)| (k.as_str(), v.as_str())).collect();
    let backend = pprof_backend(PprofConfig::new().sample_rate(SAMPLE_RATE_HZ));
    let mut builder = PyroscopeAgentBuilder::new(server.as_str(), identity.service_name.as_str())
        .backend(backend)
        .tags(tag_refs);
    if !cfg.username.is_empty() || !cfg.password.is_empty() {
        builder = builder.basic_auth(&cfg.username, &cfg.password);
    }
    let ready = builder.build().map_err(|err| err.to_string())?;
    ready.start().map_err(|err| err.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn origin_url_strips_the_path_and_keeps_the_port() {
        assert_eq!(
            origin_url("https://gigapipe.example/some/path").expect("origin"),
            "https://gigapipe.example",
            "path removed"
        );
        assert_eq!(
            origin_url("http://127.0.0.1:4040/").expect("origin"),
            "http://127.0.0.1:4040",
            "port kept"
        );
    }

    #[test]
    fn origin_url_rejects_garbage() {
        assert!(origin_url("not a url").is_err(), "unparseable");
    }

    #[test]
    fn start_fails_for_an_unparseable_url() {
        let cfg = Config {
            url: "::not-a-url::".to_string(),
            ..Config::default()
        };
        let identity = Identity::resolve(&cfg);
        assert!(
            start(&cfg, &identity).is_err(),
            "bad url reported, not panicked"
        );
    }

    #[test]
    fn start_and_shutdown_with_credentials_and_version() {
        let cfg = Config {
            url: "http://127.0.0.1:1".to_string(),
            username: "user".to_string(),
            password: "pass".to_string(),
            service: "svc".to_string(),
            service_version: "1.0.0".to_string(),
            ..Config::default()
        };
        let identity = Identity::resolve(&cfg);
        let agent = start(&cfg, &identity).expect("agent starts");
        agent.stop().expect("agent stops").shutdown();
    }
}
