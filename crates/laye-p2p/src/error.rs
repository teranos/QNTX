//! Per-consumer wrapper around `laye_error::Error`.
//!
//! Thread-local buffer + `emit` / `drain` / `next_id` / `now_ms`. The
//! wire shape lives in `laye_error`; this module owns the id-prefix
//! and the clock source (JS Date on wasm32, SystemTime on native).
//!
//! Every failing boundary in laye-p2p routes through `emit`. Nobody
//! collapses to a String, nobody returns `Result<(), JsValue>`, nobody
//! drops silently. The overlay reads via `drain` and renders each Error
//! as the sacred terminal-block per ERROR.md's visual contract.

use laye_error::{Anchor, Context, Error, Severity};
use std::cell::RefCell;
use std::collections::VecDeque;

const CAP: usize = 100;

thread_local! {
    static BUFFER: RefCell<VecDeque<Error>> = const { RefCell::new(VecDeque::new()) };
    static COUNTER: RefCell<u64> = const { RefCell::new(0) };
}

pub fn next_id() -> String {
    COUNTER.with(|c| {
        let mut c = c.borrow_mut();
        *c += 1;
        format!("err-laye-{}", *c)
    })
}

#[cfg(target_arch = "wasm32")]
pub fn now_ms() -> String {
    (js_sys::Date::now() as u64).to_string()
}

#[cfg(not(target_arch = "wasm32"))]
pub fn now_ms() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0);
    ms.to_string()
}

pub fn emit(error: Error) {
    BUFFER.with(|b| {
        let mut b = b.borrow_mut();
        if b.len() >= CAP {
            b.pop_front();
        }
        b.push_back(error);
    });
}

pub fn drain() -> Vec<Error> {
    BUFFER.with(|b| b.borrow_mut().drain(..).collect())
}

pub fn peek_all() -> Vec<Error> {
    BUFFER.with(|b| b.borrow().iter().cloned().collect())
}

pub fn clear() {
    BUFFER.with(|b| b.borrow_mut().clear());
}

/// Convenience builder for the common shape at a laye boundary.
pub fn build(
    severity: Severity,
    surface: &str,
    region: &str,
    title: &str,
    why: impl Into<String>,
) -> Error {
    Error {
        id: next_id(),
        severity,
        context: Context {
            surface: surface.to_string(),
            region: Some(region.to_string()),
            anchor: None,
        },
        title: title.to_string(),
        why: why.into(),
        trace: Vec::new(),
        raw: None,
        at: now_ms(),
        source: Some("rust-ffi".to_string()),
        ffi_call: None,
        location: None,
        js_stack: None,
        raw_stderr: None,
        requires_reload: false,
    }
}

/// The panic hook builds this shape.
pub fn build_panic(location: Option<String>, message: String, raw_stderr: Option<String>) -> Error {
    Error {
        id: next_id(),
        severity: Severity::Panic,
        context: Context {
            surface: "laye-p2p".to_string(),
            region: Some("wasm-panic".to_string()),
            anchor: None,
        },
        title: "laye-p2p panicked".to_string(),
        why: message,
        trace: Vec::new(),
        raw: None,
        at: now_ms(),
        source: Some("rust-panic".to_string()),
        ffi_call: None,
        location,
        js_stack: None,
        raw_stderr,
        requires_reload: true,
    }
}

/// Attach cursor anchor for click-triggered errors.
pub fn with_anchor(mut e: Error, x: f64, y: f64) -> Error {
    e.context.anchor = Some(Anchor { x, y });
    e
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    #[test]
    fn next_id_is_monotonic_within_prefix() {
        clear();
        let a = next_id();
        let b = next_id();
        assert!(a.starts_with("err-laye-"));
        assert!(b.starts_with("err-laye-"));
        assert_ne!(a, b);
    }

    #[test]
    fn emit_and_drain_returns_pushed_error() {
        clear();
        let e = build(Severity::Warn, "test", "region", "title", "why");
        let id = e.id.clone();
        emit(e);
        let drained = drain();
        assert_eq!(drained.len(), 1);
        assert_eq!(drained[0].id, id);
        assert_eq!(drained[0].severity, Severity::Warn);
        assert_eq!(drained[0].context.surface, "test");
        assert_eq!(drained[0].context.region.as_deref(), Some("region"));
        assert!(drain().is_empty());
    }

    #[test]
    fn buffer_evicts_oldest_when_over_cap() {
        clear();
        for i in 0..CAP + 5 {
            emit(build(
                Severity::Info,
                "test",
                "region",
                &format!("t{i}"),
                format!("w{i}"),
            ));
        }
        let all = peek_all();
        assert_eq!(all.len(), CAP);
        assert_eq!(all[0].title, "t5");
        assert_eq!(all.last().unwrap().title, format!("t{}", CAP + 4));
    }

    #[test]
    fn with_anchor_populates_context() {
        clear();
        let e = build(Severity::Error, "s", "r", "t", "w");
        let e = with_anchor(e, 100.0, 200.0);
        let a = e.context.anchor.unwrap();
        assert_eq!(a.x, 100.0);
        assert_eq!(a.y, 200.0);
    }
}
