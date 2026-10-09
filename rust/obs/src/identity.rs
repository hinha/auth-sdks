//! The one resolved service identity shared by metrics, traces, and profiles, so the three cannot
//! drift apart. Mirrors `go/obs/identity.go`.

use crate::config::Config;

/// Fallback `service_name` when no `service` is configured (`go/obs`: `auth-sdks`).
pub const DEFAULT_SERVICE_NAME: &str = "auth-sdks";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Identity {
    pub service: String,
    pub env: String,
    pub service_name: String,
    pub service_namespace: String,
    pub service_version: String,
}

impl Identity {
    /// Resolves the identity from a config. `service_name` equals `service` whenever `service` is
    /// set, so the two labels never disagree.
    pub fn resolve(cfg: &Config) -> Self {
        let service = cfg.service.trim().to_string();
        let service_name = if service.is_empty() {
            DEFAULT_SERVICE_NAME.to_string()
        } else {
            service.clone()
        };
        Self {
            service,
            env: cfg.env.trim().to_string(),
            service_name,
            service_namespace: cfg.service_namespace.trim().to_string(),
            service_version: cfg.service_version.trim().to_string(),
        }
    }

    /// Labels attached to every remote-written series. `service_version` is excluded on purpose:
    /// it changes every deploy, so carrying it on each series would churn the whole set. It rides
    /// on `target_info` only.
    ///
    /// `deployment_environment` and `deployment_environment_name` carry the same value, so the
    /// locked spelling and the OTel-native one both work.
    pub fn labels(&self) -> Vec<(String, String)> {
        let mut out = Vec::with_capacity(6);
        let mut add = |name: &str, value: &str| {
            if !value.is_empty() {
                out.push((name.to_string(), value.to_string()));
            }
        };
        add("service", &self.service);
        add("service_name", &self.service_name);
        add("env", &self.env);
        add("deployment_environment", &self.env);
        add("deployment_environment_name", &self.env);
        add("service_namespace", &self.service_namespace);
        out
    }

    /// OpenTelemetry resource attributes for traces and profiles, derived from the same fields as
    /// [`Identity::labels`].
    pub fn resource_attributes(&self) -> Vec<(&'static str, String)> {
        let mut attrs = vec![("service.name", self.service_name.clone())];
        if !self.service_namespace.is_empty() {
            attrs.push(("service.namespace", self.service_namespace.clone()));
        }
        if !self.service_version.is_empty() {
            attrs.push(("service.version", self.service_version.clone()));
        }
        if !self.env.is_empty() {
            attrs.push(("deployment.environment.name", self.env.clone()));
        }
        attrs
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(service: &str, env: &str) -> Config {
        Config {
            service: service.to_string(),
            env: env.to_string(),
            service_namespace: "payments".to_string(),
            service_version: " v1.4.2 ".to_string(),
            ..Config::default()
        }
    }

    #[test]
    fn service_name_falls_back_to_the_default() {
        let id = Identity::resolve(&cfg("", ""));
        assert_eq!(id.service_name, DEFAULT_SERVICE_NAME, "fallback name");
        assert_eq!(id.service, "", "service label stays empty");
    }

    #[test]
    fn service_name_matches_service_when_set() {
        let id = Identity::resolve(&cfg("  money-tracker ", "prod"));
        assert_eq!(id.service, "money-tracker", "trimmed service");
        assert_eq!(
            id.service_name, "money-tracker",
            "service_name is an alias of service"
        );
        assert_eq!(id.service_version, "v1.4.2", "version trimmed");
    }

    #[test]
    fn labels_omit_empty_values_and_exclude_version() {
        let labels = Identity::resolve(&cfg("svc", "")).labels();
        let names: Vec<&str> = labels.iter().map(|(n, _)| n.as_str()).collect();
        assert_eq!(
            names,
            vec!["service", "service_name", "service_namespace"],
            "env labels omitted when env empty, version never a label"
        );
    }

    #[test]
    fn labels_carry_env_twice_for_both_spellings() {
        let labels = Identity::resolve(&cfg("svc", "prod")).labels();
        let env_names: Vec<&str> = labels
            .iter()
            .filter(|(_, v)| v == "prod")
            .map(|(n, _)| n.as_str())
            .collect();
        assert_eq!(
            env_names,
            vec![
                "env",
                "deployment_environment",
                "deployment_environment_name"
            ],
            "three env spellings"
        );
    }

    #[test]
    fn resource_attributes_include_version_and_env() {
        let attrs = Identity::resolve(&cfg("svc", "prod")).resource_attributes();
        assert!(
            attrs.contains(&("service.name", "svc".to_string())),
            "service.name"
        );
        assert!(
            attrs.contains(&("service.version", "v1.4.2".to_string())),
            "service.version"
        );
        assert!(
            attrs.contains(&("deployment.environment.name", "prod".to_string())),
            "env attribute"
        );
    }

    #[test]
    fn resource_attributes_skip_empty_optionals() {
        let id = Identity::resolve(&Config::default());
        let attrs = id.resource_attributes();
        assert_eq!(
            attrs,
            vec![("service.name", DEFAULT_SERVICE_NAME.to_string())],
            "only the name"
        );
    }
}
