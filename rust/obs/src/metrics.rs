//! Merged gather and conversion into Prometheus remote-write series.

use std::collections::{BTreeMap, HashSet};

use prometheus::Registry;
use prometheus::proto::{Metric, MetricFamily, MetricType};

use crate::prompb::{
    Label, MetricMetadata, MetricType as PromType, Sample, TimeSeries, WriteRequest,
};

/// Labels that never reach Gigapipe. Matched on the exact label name.
pub const DROPPED_LABELS: [&str; 4] = ["password", "token", "authorization", "api_key"];

/// Produces families on each gather, for collectors that read live state such as `/proc`.
pub(crate) type Producer = Box<dyn Fn() -> Vec<MetricFamily> + Send + Sync>;

/// Gathers the application registry merged with the default families. On a name clash the
/// application's family wins and the default is dropped, so the same series is never sent twice.
pub struct Gatherer {
    pub(crate) registry: Registry,
    defaults: Registry,
    producers: Vec<Producer>,
}

impl Gatherer {
    pub(crate) fn new(registry: Registry, defaults: Registry, producers: Vec<Producer>) -> Self {
        Self {
            registry,
            defaults,
            producers,
        }
    }

    /// The merged families, application first.
    pub fn gather(&self) -> Vec<MetricFamily> {
        let mut families = self.registry.gather();
        let mut seen: HashSet<String> = families.iter().map(|mf| mf.name().to_string()).collect();
        let mut push = |mf: MetricFamily| {
            if seen.insert(mf.name().to_string()) {
                families.push(mf);
            }
        };
        for mf in self.defaults.gather() {
            push(mf);
        }
        for producer in &self.producers {
            for mf in producer() {
                push(mf);
            }
        }
        families
    }
}

/// The remote-write body for `families`. `identity_labels` are added to every series, unless the
/// series already carries that label name. Each sample uses `timestamp_ms`.
pub fn write_request(
    families: &[MetricFamily],
    identity_labels: &[(String, String)],
    timestamp_ms: i64,
) -> WriteRequest {
    let mut timeseries = Vec::new();
    let mut metadata = Vec::new();
    for mf in families {
        metadata.push(MetricMetadata {
            r#type: prom_type(mf.get_field_type()) as i32,
            metric_family_name: mf.name().to_string(),
            help: mf.help().to_string(),
            unit: unit_for(mf.name()).to_string(),
        });
        for metric in mf.get_metric() {
            series_for(mf, metric, identity_labels, timestamp_ms, &mut timeseries);
        }
    }
    WriteRequest {
        timeseries,
        metadata,
    }
}

fn series_for(
    mf: &MetricFamily,
    metric: &Metric,
    identity_labels: &[(String, String)],
    ts: i64,
    out: &mut Vec<TimeSeries>,
) {
    let name = mf.name();
    let base = base_labels(metric, identity_labels);
    match mf.get_field_type() {
        MetricType::COUNTER => out.push(series(name, &base, &[], metric.counter.value(), ts)),
        MetricType::GAUGE => out.push(series(name, &base, &[], metric.gauge.value(), ts)),
        MetricType::UNTYPED => out.push(series(name, &base, &[], metric.untyped.value(), ts)),
        MetricType::HISTOGRAM => {
            let hist = metric.get_histogram();
            for bucket in hist.get_bucket() {
                let le = format_bound(bucket.upper_bound());
                out.push(series(
                    &format!("{name}_bucket"),
                    &base,
                    &[("le".to_string(), le)],
                    bucket.cumulative_count() as f64,
                    ts,
                ));
            }
            out.push(series(
                &format!("{name}_sum"),
                &base,
                &[],
                hist.sample_sum(),
                ts,
            ));
            out.push(series(
                &format!("{name}_count"),
                &base,
                &[],
                hist.sample_count() as f64,
                ts,
            ));
        }
        MetricType::SUMMARY => {
            let summary = metric.get_summary();
            for quantile in summary.get_quantile() {
                let q = format_bound(quantile.quantile());
                out.push(series(
                    name,
                    &base,
                    &[("quantile".to_string(), q)],
                    quantile.value(),
                    ts,
                ));
            }
            out.push(series(
                &format!("{name}_sum"),
                &base,
                &[],
                summary.sample_sum(),
                ts,
            ));
            out.push(series(
                &format!("{name}_count"),
                &base,
                &[],
                summary.sample_count() as f64,
                ts,
            ));
        }
    }
}

/// The series labels from the metric itself, plus the identity labels that it does not already
/// carry. Dropped secret names are filtered here.
fn base_labels(metric: &Metric, identity_labels: &[(String, String)]) -> Vec<(String, String)> {
    let mut labels: BTreeMap<String, String> = BTreeMap::new();
    for pair in &metric.label {
        let name = pair.name();
        if DROPPED_LABELS.contains(&name) {
            continue;
        }
        labels.insert(name.to_string(), pair.value().to_string());
    }
    for (name, value) in identity_labels {
        if DROPPED_LABELS.contains(&name.as_str()) {
            continue;
        }
        labels.entry(name.clone()).or_insert_with(|| value.clone());
    }
    labels.into_iter().collect()
}

fn series(
    name: &str,
    base: &[(String, String)],
    extra: &[(String, String)],
    value: f64,
    ts: i64,
) -> TimeSeries {
    let mut labels: BTreeMap<String, String> = base.iter().cloned().collect();
    for (k, v) in extra {
        labels.insert(k.clone(), v.clone());
    }
    labels.insert("__name__".to_string(), name.to_string());
    TimeSeries {
        labels: labels
            .into_iter()
            .map(|(name, value)| Label { name, value })
            .collect(),
        samples: vec![Sample {
            value,
            timestamp: ts,
        }],
    }
}

/// Prometheus text form of a bucket bound or quantile. Infinity is `+Inf`.
fn format_bound(v: f64) -> String {
    if v.is_infinite() && v.is_sign_positive() {
        "+Inf".to_string()
    } else {
        format!("{v}")
    }
}

fn prom_type(kind: MetricType) -> PromType {
    match kind {
        MetricType::COUNTER => PromType::Counter,
        MetricType::GAUGE => PromType::Gauge,
        MetricType::HISTOGRAM => PromType::Histogram,
        MetricType::SUMMARY => PromType::Summary,
        _ => PromType::Unknown,
    }
}

/// UNIT metadata, derived from the conventional name suffix.
fn unit_for(name: &str) -> &'static str {
    if name.ends_with("_seconds") {
        "seconds"
    } else if name.ends_with("_bytes") {
        "bytes"
    } else {
        ""
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::collectors::{counter_family, gauge_family};
    use prometheus::proto::{Histogram, Summary};

    fn labels(pairs: &[(&str, &str)]) -> Vec<(String, String)> {
        pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect()
    }

    fn sample_map(req: &WriteRequest) -> BTreeMap<String, (f64, Vec<(String, String)>)> {
        req.timeseries
            .iter()
            .map(|ts| {
                let name = ts
                    .labels
                    .iter()
                    .find(|l| l.name == "__name__")
                    .map(|l| l.value.clone())
                    .unwrap_or_default();
                let rest: Vec<(String, String)> = ts
                    .labels
                    .iter()
                    .filter(|l| l.name != "__name__")
                    .map(|l| (l.name.clone(), l.value.clone()))
                    .collect();
                (name, (ts.samples[0].value, rest))
            })
            .collect()
    }

    #[test]
    fn counter_and_gauge_become_one_series_each_with_name_label() {
        let families = vec![
            counter_family("reqs_total", "h", vec![(labels(&[("code", "200")]), 3.0)]),
            gauge_family("temp", "h", vec![(Vec::new(), 1.5)]),
        ];
        let req = write_request(&families, &[], 1000);
        let map = sample_map(&req);
        assert_eq!(map["reqs_total"].0, 3.0, "counter value");
        assert_eq!(map["temp"].0, 1.5, "gauge value");
        assert_eq!(req.metadata.len(), 2, "one metadata entry per family");
        assert_eq!(
            req.timeseries[0].samples[0].timestamp, 1000,
            "timestamp in ms"
        );
    }

    #[test]
    fn histogram_expands_to_buckets_sum_and_count() {
        let mut hist = Histogram::new();
        hist.set_sample_count(4);
        hist.set_sample_sum(2.5);
        for (ub, cum) in [(0.5, 1), (1.0, 3)] {
            let mut b = prometheus::proto::Bucket::new();
            b.set_upper_bound(ub);
            b.set_cumulative_count(cum);
            hist.bucket.push(b);
        }
        let mut metric = Metric::new();
        metric.set_histogram(hist);
        let mut mf = MetricFamily::new();
        mf.set_name("latency_seconds".into());
        mf.set_field_type(MetricType::HISTOGRAM);
        mf.mut_metric().push(metric);

        let req = write_request(&[mf], &[], 1);
        let map = sample_map(&req);
        assert!(map.contains_key("latency_seconds_bucket"), "bucket series");
        assert!(map.contains_key("latency_seconds_sum"), "sum series");
        assert_eq!(map["latency_seconds_count"].0, 4.0, "count value");
        let les: Vec<String> = req
            .timeseries
            .iter()
            .flat_map(|ts| ts.labels.iter())
            .filter(|l| l.name == "le")
            .map(|l| l.value.clone())
            .collect();
        assert_eq!(les, vec!["0.5", "1"], "bucket bounds as le labels");
    }

    #[test]
    fn summary_emits_quantiles_sum_and_count() {
        let mut summary = Summary::new();
        summary.set_sample_count(10);
        summary.set_sample_sum(5.0);
        let mut q = prometheus::proto::Quantile::new();
        q.set_quantile(0.5);
        q.set_value(0.4);
        summary.quantile.push(q);
        let mut metric = Metric::new();
        metric.set_summary(summary);
        let mut mf = MetricFamily::new();
        mf.set_name("work".into());
        mf.set_field_type(MetricType::SUMMARY);
        mf.mut_metric().push(metric);

        let req = write_request(&[mf], &[], 1);
        let map = sample_map(&req);
        assert_eq!(map["work"].0, 0.4, "quantile value");
        assert!(
            map["work"]
                .1
                .contains(&("quantile".to_string(), "0.5".to_string())),
            "quantile label"
        );
        assert_eq!(map["work_count"].0, 10.0, "count");
        assert_eq!(map["work_sum"].0, 5.0, "sum");
    }

    #[test]
    fn untyped_family_is_written_as_a_plain_value() {
        let mut metric = Metric::new();
        metric.untyped.mut_or_insert_default().set_value(9.0);
        let mut mf = MetricFamily::new();
        mf.set_name("legacy".into());
        mf.set_field_type(MetricType::UNTYPED);
        mf.mut_metric().push(metric);
        let req = write_request(&[mf], &[], 1);
        assert_eq!(sample_map(&req)["legacy"].0, 9.0, "untyped value");
        assert_eq!(
            req.metadata[0].r#type,
            PromType::Unknown as i32,
            "unknown metadata type"
        );
    }

    #[test]
    fn identity_labels_are_added_but_never_override_a_series_label() {
        let families = vec![counter_family(
            "c",
            "h",
            vec![(labels(&[("service", "own")]), 1.0)],
        )];
        let identity = labels(&[("service", "from-identity"), ("env", "prod")]);
        let req = write_request(&families, &identity, 1);
        let map = sample_map(&req);
        let rest = &map["c"].1;
        assert!(
            rest.contains(&("service".to_string(), "own".to_string())),
            "series label wins"
        );
        assert!(
            rest.contains(&("env".to_string(), "prod".to_string())),
            "identity label added"
        );
    }

    #[test]
    fn secret_labels_are_dropped_from_series_and_identity() {
        let families = vec![counter_family(
            "c",
            "h",
            vec![(labels(&[("token", "abc"), ("ok", "1")]), 1.0)],
        )];
        let identity = labels(&[("authorization", "Bearer x"), ("api_key", "k")]);
        let req = write_request(&families, &identity, 1);
        let rest = &sample_map(&req)["c"].1;
        let names: Vec<&str> = rest.iter().map(|(n, _)| n.as_str()).collect();
        assert_eq!(names, vec!["ok"], "only the safe label survives");
    }

    #[test]
    fn units_follow_name_suffixes() {
        assert_eq!(unit_for("x_seconds"), "seconds", "seconds suffix");
        assert_eq!(unit_for("x_bytes"), "bytes", "bytes suffix");
        assert_eq!(unit_for("x_total"), "", "no unit");
    }

    #[test]
    fn infinite_bounds_render_as_plus_inf() {
        assert_eq!(format_bound(f64::INFINITY), "+Inf", "positive infinity");
        assert_eq!(format_bound(0.25), "0.25", "finite bound");
    }

    #[test]
    fn gather_prefers_application_families_over_defaults() {
        let app = Registry::new();
        let defaults = Registry::new();
        let app_counter = prometheus::IntCounter::new("shared_total", "app").expect("counter");
        app.register(Box::new(app_counter.clone()))
            .expect("register app");
        app_counter.inc_by(5);
        let default_counter =
            prometheus::IntCounter::new("shared_total", "default").expect("counter");
        defaults
            .register(Box::new(default_counter))
            .expect("register default");
        let gatherer = Gatherer::new(app, defaults, Vec::new());

        let families = gatherer.gather();
        let shared: Vec<&MetricFamily> = families
            .iter()
            .filter(|mf| mf.name() == "shared_total")
            .collect();
        assert_eq!(shared.len(), 1, "no duplicate family");
        assert_eq!(shared[0].help(), "app", "application family wins");
    }

    #[test]
    fn gather_includes_producers_when_not_clashing() {
        let gatherer = Gatherer::new(
            Registry::new(),
            Registry::new(),
            vec![Box::new(|| {
                vec![gauge_family("produced", "h", vec![(Vec::new(), 1.0)])]
            })],
        );
        assert!(
            gatherer.gather().iter().any(|mf| mf.name() == "produced"),
            "producer family present"
        );
    }
}
