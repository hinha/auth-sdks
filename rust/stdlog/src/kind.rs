//! Loki `kind` label.

use serde_json::Value;

pub const KIND_AUDIT: &str = "audit";
pub const KIND_ACCESS: &str = "access";
pub const KIND_APP: &str = "app";
pub const KIND_SDK: &str = "sdk";

/// Explicit field that overrides the message-based kind.
pub const FIELD_KIND: &str = "kind";
/// Message of an audit record.
pub const AUDIT_LOG_MESSAGE: &str = "audit event";
/// Message of an access-log record.
pub const ACCESS_LOG_MESSAGE: &str = "http request completed";

/// The first non-empty string `kind` field wins. Otherwise the message decides, and the default is
/// [`KIND_APP`].
pub fn kind_from(msg: &str, fields: &[(String, Value)]) -> String {
    for (key, value) in fields {
        if key != FIELD_KIND {
            continue;
        }
        if let Value::String(explicit) = value
            && !explicit.is_empty()
        {
            return explicit.clone();
        }
    }
    match msg {
        AUDIT_LOG_MESSAGE => KIND_AUDIT,
        ACCESS_LOG_MESSAGE => KIND_ACCESS,
        "http_request_end" | "http_request_error" | "http_request_start" => KIND_SDK,
        _ => KIND_APP,
    }
    .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn explicit_kind_field_wins_over_message() {
        let fields = vec![("kind".to_string(), json!("custom"))];
        assert_eq!(
            kind_from(AUDIT_LOG_MESSAGE, &fields),
            "custom",
            "explicit kind wins"
        );
    }

    #[test]
    fn empty_kind_field_is_ignored() {
        let fields = vec![
            ("kind".to_string(), json!("")),
            ("kind".to_string(), json!(42)),
        ];
        assert_eq!(
            kind_from(AUDIT_LOG_MESSAGE, &fields),
            KIND_AUDIT,
            "empty and non-string kind ignored"
        );
    }

    #[test]
    fn message_selects_kind_when_no_field() {
        assert_eq!(
            kind_from(AUDIT_LOG_MESSAGE, &[]),
            KIND_AUDIT,
            "audit message"
        );
        assert_eq!(
            kind_from(ACCESS_LOG_MESSAGE, &[]),
            KIND_ACCESS,
            "access message"
        );
        assert_eq!(kind_from("http_request_start", &[]), KIND_SDK, "sdk start");
        assert_eq!(kind_from("http_request_end", &[]), KIND_SDK, "sdk end");
        assert_eq!(kind_from("http_request_error", &[]), KIND_SDK, "sdk error");
        assert_eq!(kind_from("anything else", &[]), KIND_APP, "default app");
    }
}
