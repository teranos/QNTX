//! Sacred Error primitive for laye.
//!
//! One typed value that crosses every layer of the system unchanged.
//! Byte-compatible with tsot-roam's `crates/sacred-error/src/lib.rs` —
//! same JSON shape, same `Severity` closed enum, same axiom-enforcement
//! test (unknown severity FAILS decode).
//!
//! # Scope
//!
//! Only the data shape (`Error`, `Severity`, `Context`, `Anchor`) plus
//! wire tests. The thread-local buffer + `emit` / `drain` / `next_id`
//! live in each consumer crate's `error.rs` (see laye-p2p) because
//! id-prefix and clock source differ per consumer.
//!
//! # The axiom
//!
//! Errors are sacred — first-class citizens, never collapsed, dropped,
//! swallowed, or suppressed. They land in front of the user,
//! contextually at points of interaction. The wire shape is one place
//! (this crate); the render path is one place (laye-p2p's overlay);
//! every boundary preserves the typed value, never down-converts to
//! `String` or `JsValue::from_str`.

use serde::{Deserialize, Serialize};

/// One typed Error. Fields and field order match the tsot Elm decoder
/// so JSON round-trips byte-for-byte across projects.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Error {
    pub id: String,
    pub severity: Severity,
    pub context: Context,
    pub title: String,
    pub why: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub trace: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub raw: Option<String>,
    pub at: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ffi_call: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub location: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub js_stack: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub raw_stderr: Option<String>,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub requires_reload: bool,
}

/// Severity vocabulary — closed set. Unknown labels MUST fail decode
/// (axiom enforcement, no silent Info fallback).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Severity {
    Info,
    Warn,
    Error,
    Panic,
}

/// Where the failure happened. `surface` is required; `region` and
/// `anchor` are optional. `anchor` carries pixel position of the click
/// that triggered the failure — the renderer anchors the overlay there.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Context {
    pub surface: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub region: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub anchor: Option<Anchor>,
}

/// Pixel position from `MouseEvent.clientX/Y`.
#[derive(Clone, Copy, Debug, PartialEq, Serialize, Deserialize)]
pub struct Anchor {
    pub x: f64,
    pub y: f64,
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use serde_json::Value;

    #[test]
    fn severity_serializes_lowercase_matching_tsot_wire_shape() {
        assert_eq!(serde_json::to_string(&Severity::Info).unwrap(), "\"info\"");
        assert_eq!(serde_json::to_string(&Severity::Warn).unwrap(), "\"warn\"");
        assert_eq!(
            serde_json::to_string(&Severity::Error).unwrap(),
            "\"error\""
        );
        assert_eq!(
            serde_json::to_string(&Severity::Panic).unwrap(),
            "\"panic\""
        );
    }

    #[test]
    fn unknown_severity_label_fails_decode() {
        // Axiom enforcement: closed set. Unknown label = contract
        // violation. Never silently downgrade to a default.
        let result: Result<Severity, _> = serde_json::from_str("\"unknown\"");
        assert!(result.is_err());
    }

    #[test]
    fn full_round_trip_preserves_every_field() {
        let e = Error {
            id: "err-laye-1".into(),
            severity: Severity::Error,
            context: Context {
                surface: "laye-p2p".into(),
                region: Some("chat-publish".into()),
                anchor: Some(Anchor { x: 120.0, y: 40.0 }),
            },
            title: "chat publish failed".into(),
            why: "NoPeersSubscribedToTopic on laye-chat/v1".into(),
            trace: vec!["publish_chat".into(), "with_net".into()],
            raw: Some("body=hi".into()),
            at: "1751000000000".into(),
            source: Some("rust-ffi".into()),
            ffi_call: Some("publish".into()),
            location: Some("crates/laye-p2p/src/wasm.rs".into()),
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        };
        let json = serde_json::to_string(&e).unwrap();
        let v: Value = serde_json::from_str(&json).unwrap();
        assert_eq!(v["id"], "err-laye-1");
        assert_eq!(v["severity"], "error");
        assert_eq!(v["context"]["surface"], "laye-p2p");
        assert_eq!(v["context"]["region"], "chat-publish");
        assert_eq!(v["context"]["anchor"]["x"], 120.0);
        assert_eq!(v["source"], "rust-ffi");
        assert_eq!(v["ffi_call"], "publish");
        assert!(v.get("js_stack").is_none());
        let round: Error = serde_json::from_value(v).unwrap();
        assert_eq!(round, e);
    }

    #[test]
    fn optional_fields_omitted_when_default() {
        let e = Error {
            id: "x".into(),
            severity: Severity::Info,
            context: Context {
                surface: "test".into(),
                region: None,
                anchor: None,
            },
            title: "t".into(),
            why: "w".into(),
            trace: vec![],
            raw: None,
            at: "0".into(),
            source: None,
            ffi_call: None,
            location: None,
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        };
        let json = serde_json::to_string(&e).unwrap();
        for absent in [
            "\"trace\"",
            "\"raw\"",
            "\"region\"",
            "\"anchor\"",
            "\"source\"",
            "\"ffi_call\"",
            "\"location\"",
            "\"js_stack\"",
            "\"raw_stderr\"",
            "\"requires_reload\"",
        ] {
            assert!(!json.contains(absent), "{absent} should be omitted: {json}");
        }
    }
}
