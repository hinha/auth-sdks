//! Default metric families emitted without any registration. Mirrors `go/obs` `metrics.go`,
//! `process_io*.go`, and `filesystem*.go`. Rust has no `go_*` runtime metrics, so that part of the
//! Go default set is intentionally absent.

use prometheus::Registry;
use prometheus::proto::{Gauge, LabelPair, Metric, MetricFamily, MetricType};

use crate::identity::Identity;

/// Labels of one sample: name/value pairs, not yet sorted.
pub(crate) type Labels = Vec<(String, String)>;

pub(crate) fn gauge_family(name: &str, help: &str, samples: Vec<(Labels, f64)>) -> MetricFamily {
    family(name, help, MetricType::GAUGE, samples, |metric, value| {
        let mut gauge = Gauge::new();
        gauge.set_value(value);
        metric.set_gauge(gauge);
    })
}

#[cfg(any(target_os = "linux", test))]
pub(crate) fn counter_family(name: &str, help: &str, samples: Vec<(Labels, f64)>) -> MetricFamily {
    family(name, help, MetricType::COUNTER, samples, |metric, value| {
        let mut counter = prometheus::proto::Counter::new();
        counter.set_value(value);
        metric.set_counter(counter);
    })
}

fn family(
    name: &str,
    help: &str,
    kind: MetricType,
    samples: Vec<(Labels, f64)>,
    set_value: impl Fn(&mut Metric, f64),
) -> MetricFamily {
    let mut mf = MetricFamily::new();
    mf.set_name(name.to_string());
    mf.set_help(help.to_string());
    mf.set_field_type(kind);
    for (labels, value) in samples {
        let mut metric = Metric::new();
        for (label, label_value) in labels {
            let mut pair = LabelPair::new();
            pair.set_name(label);
            pair.set_value(label_value);
            metric.label.push(pair);
        }
        set_value(&mut metric, value);
        mf.mut_metric().push(metric);
    }
    mf
}

/// The `target_info` gauge: constant 1, with the service identity in its labels. `service_version`
/// lives here and only here.
pub(crate) fn target_info(identity: &Identity) -> MetricFamily {
    let mut labels = identity.labels();
    if !identity.service_version.is_empty() {
        labels.push((
            "service_version".to_string(),
            identity.service_version.clone(),
        ));
    }
    gauge_family(
        "target_info",
        "Target metadata. Constant 1; the service identity is carried in the labels.",
        vec![(labels, 1.0)],
    )
}

/// Registers the runtime collectors that exist on this platform. Linux gets the standard process
/// collector. Other platforms get nothing here, and the other families are added by [`crate::Handle`].
pub(crate) fn register_defaults(defaults: &Registry) {
    #[cfg(target_os = "linux")]
    {
        let pid = i32::try_from(std::process::id()).unwrap_or(0);
        let collector = prometheus::process_collector::ProcessCollector::new(pid, "");
        let _ = defaults.register(Box::new(collector));
    }
    #[cfg(not(target_os = "linux"))]
    let _ = defaults;
}

/// `process_io_*` counters from `/proc/self/io`. Linux only; empty elsewhere or when unreadable.
#[cfg(target_os = "linux")]
pub(crate) fn process_io() -> Vec<MetricFamily> {
    let Ok(io) = procfs::process::Process::myself().and_then(|p| p.io()) else {
        return Vec::new();
    };
    vec![
        counter_family(
            "process_io_read_bytes_total",
            "Bytes read from the storage layer (kernel read_bytes).",
            vec![(Vec::new(), io.read_bytes as f64)],
        ),
        counter_family(
            "process_io_write_bytes_total",
            "Bytes written to the storage layer (kernel write_bytes).",
            vec![(Vec::new(), io.write_bytes as f64)],
        ),
        counter_family(
            "process_io_rchar_bytes_total",
            "Bytes read through read syscalls, including page cache hits (rchar).",
            vec![(Vec::new(), io.rchar as f64)],
        ),
        counter_family(
            "process_io_wchar_bytes_total",
            "Bytes written through write syscalls, including page cache (wchar).",
            vec![(Vec::new(), io.wchar as f64)],
        ),
    ]
}

/// Off Linux there is no `/proc/self/io`, so the families are omitted.
#[cfg(not(target_os = "linux"))]
pub(crate) fn process_io() -> Vec<MetricFamily> {
    Vec::new()
}

/// `process_filesystem_avail_bytes` and `process_filesystem_size_bytes`, one sample per path.
/// Unreadable paths are skipped. A family with no samples is omitted.
#[cfg(unix)]
pub(crate) fn filesystem(paths: &[String]) -> Vec<MetricFamily> {
    let mut avail = Vec::new();
    let mut size = Vec::new();
    for path in paths {
        let Ok(stat) = nix::sys::statvfs::statvfs(path.as_str()) else {
            continue;
        };
        let block = stat.fragment_size() as f64;
        let labels = vec![("path".to_string(), path.clone())];
        avail.push((labels.clone(), stat.blocks_available() as f64 * block));
        size.push((labels, stat.blocks() as f64 * block));
    }
    let mut out = Vec::new();
    if !avail.is_empty() {
        out.push(gauge_family(
            "process_filesystem_avail_bytes",
            "Bytes available to unprivileged processes on the filesystem holding path.",
            avail,
        ));
    }
    if !size.is_empty() {
        out.push(gauge_family(
            "process_filesystem_size_bytes",
            "Total size in bytes of the filesystem holding path.",
            size,
        ));
    }
    out
}

/// Non-unix platforms have no statvfs, so the filesystem families are omitted.
#[cfg(not(unix))]
pub(crate) fn filesystem(_paths: &[String]) -> Vec<MetricFamily> {
    Vec::new()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::Config;

    fn identity(version: &str) -> Identity {
        Identity::resolve(&Config {
            service: "svc".to_string(),
            env: "prod".to_string(),
            service_version: version.to_string(),
            ..Config::default()
        })
    }

    fn label_names(metric: &Metric) -> Vec<String> {
        metric.label.iter().map(|l| l.name().to_string()).collect()
    }

    #[test]
    fn target_info_is_a_constant_one_with_identity_labels() {
        let mf = target_info(&identity("1.2.3"));
        assert_eq!(mf.name(), "target_info", "family name");
        assert_eq!(mf.get_field_type(), MetricType::GAUGE, "gauge type");
        let metric = &mf.get_metric()[0];
        assert_eq!(metric.gauge.value(), 1.0, "constant one");
        let names = label_names(metric);
        assert!(
            names.iter().any(|n| n == "service_version"),
            "version rides on target_info"
        );
        assert!(
            names.iter().any(|n| n == "service"),
            "service identity label"
        );
    }

    #[test]
    fn target_info_omits_version_when_unknown() {
        let mf = target_info(&identity(""));
        let names = label_names(&mf.get_metric()[0]);
        assert!(
            !names.iter().any(|n| n == "service_version"),
            "no version label when empty"
        );
    }

    #[test]
    fn gauge_and_counter_builders_set_type_and_value() {
        let g = gauge_family("g", "h", vec![(vec![("a".into(), "b".into())], 2.5)]);
        assert_eq!(g.get_field_type(), MetricType::GAUGE, "gauge kind");
        assert_eq!(g.get_metric()[0].gauge.value(), 2.5, "gauge value");
        assert_eq!(g.get_metric()[0].label[0].value(), "b", "label value");

        let c = counter_family("c", "h", vec![(Vec::new(), 7.0)]);
        assert_eq!(c.get_field_type(), MetricType::COUNTER, "counter kind");
        assert_eq!(c.get_metric()[0].counter.value(), 7.0, "counter value");
    }

    #[test]
    fn register_defaults_populates_the_defaults_registry() {
        let defaults = Registry::new();
        register_defaults(&defaults);
        #[cfg(target_os = "linux")]
        assert!(
            defaults
                .gather()
                .iter()
                .any(|mf| mf.name().starts_with("process_")),
            "process collector present on linux"
        );
        #[cfg(not(target_os = "linux"))]
        assert!(
            defaults.gather().is_empty(),
            "no process collector off linux"
        );
    }

    #[test]
    fn filesystem_reports_avail_and_size_per_path() {
        let families = filesystem(&[".".to_string()]);
        if cfg!(unix) {
            let avail = families
                .iter()
                .find(|mf| mf.name() == "process_filesystem_avail_bytes")
                .expect("avail family");
            assert_eq!(avail.get_metric()[0].label[0].name(), "path", "path label");
            let size = families
                .iter()
                .find(|mf| mf.name() == "process_filesystem_size_bytes")
                .expect("size family");
            assert!(size.get_metric()[0].gauge.value() > 0.0, "non-zero size");
        } else {
            assert!(families.is_empty(), "no filesystem families off unix");
        }
    }

    #[test]
    fn filesystem_skips_unreadable_paths() {
        let families = filesystem(&["/definitely/not/a/real/path/xyz".to_string()]);
        assert!(families.is_empty(), "unreadable path omitted");
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn process_io_counters_are_present_on_linux() {
        let families = process_io();
        assert_eq!(families.len(), 4, "four io counters");
        assert!(
            families
                .iter()
                .all(|mf| mf.get_field_type() == MetricType::COUNTER),
            "all counters"
        );
    }

    #[cfg(not(target_os = "linux"))]
    #[test]
    fn process_io_is_empty_off_linux() {
        assert!(process_io().is_empty(), "no /proc/self/io");
    }
}
