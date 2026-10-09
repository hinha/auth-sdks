//! Prometheus remote-write 1.0 protobuf messages. Hand-written to match `prompb/remote.proto` and
//! `types.proto`, so the build needs no `protoc`.

/// `prometheus.WriteRequest`.
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct WriteRequest {
    #[prost(message, repeated, tag = "1")]
    pub timeseries: Vec<TimeSeries>,
    #[prost(message, repeated, tag = "3")]
    pub metadata: Vec<MetricMetadata>,
}

/// `prometheus.TimeSeries`. Labels include `__name__` and are sorted by name.
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct TimeSeries {
    #[prost(message, repeated, tag = "1")]
    pub labels: Vec<Label>,
    #[prost(message, repeated, tag = "2")]
    pub samples: Vec<Sample>,
}

/// `prometheus.Label`.
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct Label {
    #[prost(string, tag = "1")]
    pub name: String,
    #[prost(string, tag = "2")]
    pub value: String,
}

/// `prometheus.Sample`. The timestamp is milliseconds since the Unix epoch.
#[derive(Clone, Copy, PartialEq, ::prost::Message)]
pub struct Sample {
    #[prost(double, tag = "1")]
    pub value: f64,
    #[prost(int64, tag = "2")]
    pub timestamp: i64,
}

/// `prometheus.MetricMetadata`, which carries HELP, TYPE, and UNIT for each family.
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct MetricMetadata {
    #[prost(enumeration = "MetricType", tag = "1")]
    pub r#type: i32,
    #[prost(string, tag = "2")]
    pub metric_family_name: String,
    #[prost(string, tag = "4")]
    pub help: String,
    #[prost(string, tag = "5")]
    pub unit: String,
}

/// `prometheus.MetricMetadata.MetricType`.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum MetricType {
    Unknown = 0,
    Counter = 1,
    Gauge = 2,
    Histogram = 3,
    Summary = 5,
}

#[cfg(test)]
mod tests {
    use super::*;
    use prost::Message;

    #[test]
    fn write_request_round_trips_through_protobuf() {
        let req = WriteRequest {
            timeseries: vec![TimeSeries {
                labels: vec![Label {
                    name: "__name__".into(),
                    value: "up".into(),
                }],
                samples: vec![Sample {
                    value: 1.5,
                    timestamp: 42,
                }],
            }],
            metadata: vec![MetricMetadata {
                r#type: MetricType::Gauge as i32,
                metric_family_name: "up".into(),
                help: "is up".into(),
                unit: String::new(),
            }],
        };
        let bytes = req.encode_to_vec();
        let decoded = WriteRequest::decode(bytes.as_slice()).expect("decode");
        assert_eq!(decoded, req, "encode/decode is lossless");
    }

    #[test]
    fn metric_type_numbers_match_the_prometheus_proto() {
        assert_eq!(MetricType::Counter as i32, 1, "counter");
        assert_eq!(MetricType::Gauge as i32, 2, "gauge");
        assert_eq!(MetricType::Histogram as i32, 3, "histogram");
        assert_eq!(MetricType::Summary as i32, 5, "summary");
        assert_eq!(MetricType::Unknown as i32, 0, "unknown");
    }
}
