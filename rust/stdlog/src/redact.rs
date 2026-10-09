//! Secret scrubbing for Loki lines. Mirrors `skipSecretKey` in `go/stdlog/loki.go`.

/// Text written in place of a configured secret value.
pub const REDACTED: &str = "[REDACTED]";

/// Keys that never reach Loki, matched case-insensitively after trimming.
pub fn skip_secret_key(key: &str) -> bool {
    matches!(
        key.trim().to_ascii_lowercase().as_str(),
        "password"
            | "token"
            | "access_token"
            | "refresh_token"
            | "authorization"
            | "api_key"
            | "x-api-key"
            | "current_password"
            | "new_password"
            | "confirm_password"
    )
}

/// Replaces every occurrence of each non-empty secret with [`REDACTED`].
pub fn redact_text(text: &str, secrets: &[String]) -> String {
    let mut out = text.to_string();
    for secret in secrets {
        if !secret.is_empty() && out.contains(secret.as_str()) {
            out = out.replace(secret.as_str(), REDACTED);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn secret_keys_are_matched_case_and_space_insensitively() {
        assert!(skip_secret_key("password"), "plain password key");
        assert!(skip_secret_key("  Authorization "), "trimmed, mixed case");
        assert!(skip_secret_key("X-API-Key"), "header style key");
        assert!(skip_secret_key("confirm_password"), "confirm password key");
        assert!(!skip_secret_key("user_id"), "ordinary key kept");
        assert!(!skip_secret_key("passwords"), "near miss kept");
    }

    #[test]
    fn configured_secrets_are_replaced_and_empty_ones_ignored() {
        let secrets = vec!["sk-live-123".to_string(), String::new()];
        let out = redact_text("key=sk-live-123 end", &secrets);
        assert_eq!(out, "key=[REDACTED] end", "secret replaced");
        assert_eq!(
            redact_text("nothing here", &secrets),
            "nothing here",
            "no match untouched"
        );
        assert_eq!(
            redact_text("abc", &[String::new()]),
            "abc",
            "empty secret never matches"
        );
    }
}
