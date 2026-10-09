//! Fail-open warnings. Mirrors `warn` in `go/obs/obs.go`.

use std::collections::HashSet;
use std::sync::Mutex;

use crate::config::WarnFn;

/// Distinct warning keys reported per handle. Later keys are dropped so a flood of new conditions
/// cannot grow memory or the log without bound.
pub const MAX_WARNED_KEYS: usize = 32;

/// Reports a condition once per key.
///
/// Fail-open failures recur on every interval, so an unbounded logger would drown the log. A
/// single one-shot warning would be wrong in the other direction: the first failure can happen
/// before DNS is ready, and it would silence every later, real one.
pub(crate) struct Warner {
    hook: Option<WarnFn>,
    seen: Mutex<HashSet<String>>,
}

impl Warner {
    pub(crate) fn new(hook: Option<WarnFn>) -> Self {
        Self {
            hook,
            seen: Mutex::new(HashSet::new()),
        }
    }

    /// Reports `key` with `detail` the first time the key appears.
    pub(crate) fn warn(&self, key: &str, detail: &str) {
        {
            let mut seen = self.seen.lock().unwrap_or_else(|e| e.into_inner());
            if seen.contains(key) || seen.len() >= MAX_WARNED_KEYS {
                return;
            }
            seen.insert(key.to_string());
        }
        match &self.hook {
            Some(hook) => hook(key, detail),
            None if detail.is_empty() => eprintln!("{key}"),
            None => eprintln!("{key}: {detail}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    fn capturing() -> (Arc<Mutex<Vec<String>>>, WarnFn) {
        let log: Arc<Mutex<Vec<String>>> = Arc::new(Mutex::new(Vec::new()));
        let sink = Arc::clone(&log);
        let hook: WarnFn = Arc::new(move |key: &str, detail: &str| {
            sink.lock().expect("log").push(format!("{key}|{detail}"));
        });
        (log, hook)
    }

    #[test]
    fn a_key_is_reported_only_once() {
        let (log, hook) = capturing();
        let warner = Warner::new(Some(hook));
        warner.warn("gigapipe prometheus write failed", "500");
        warner.warn("gigapipe prometheus write failed", "503");
        let got = log.lock().expect("log").clone();
        assert_eq!(
            got,
            vec!["gigapipe prometheus write failed|500".to_string()],
            "first detail wins"
        );
    }

    #[test]
    fn distinct_keys_are_each_reported() {
        let (log, hook) = capturing();
        let warner = Warner::new(Some(hook));
        warner.warn("a", "1");
        warner.warn("b", "2");
        assert_eq!(log.lock().expect("log").len(), 2, "two keys, two warnings");
    }

    #[test]
    fn keys_beyond_the_cap_are_dropped() {
        let (log, hook) = capturing();
        let warner = Warner::new(Some(hook));
        for i in 0..(MAX_WARNED_KEYS + 5) {
            warner.warn(&format!("key-{i}"), "x");
        }
        assert_eq!(
            log.lock().expect("log").len(),
            MAX_WARNED_KEYS,
            "cap respected"
        );
    }

    #[test]
    fn default_hook_writes_to_stderr_without_panicking() {
        let warner = Warner::new(None);
        warner.warn("gigapipe test warning", "detail");
        warner.warn("gigapipe test warning no detail", "");
    }
}
