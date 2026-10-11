//! The canonical JSON of an attestation: the bytes its signature is over.
//!
//! The form is the one Go's `encoding/json` gives the wire struct in
//! `ats/signing`: fields in declaration order, `created_at` and the signature
//! fields left out, `attributes` left out when empty (Go's `omitempty`), map
//! keys sorted, `<` `>` `&` escaped, and every number in attributes printed as
//! Go prints a float64. A number is a float64 because that is what it is after
//! crossing JSON into Go — which is the form a verifier on another node holds.

use std::io;

use serde::{Deserialize, Serialize};
use serde_json::ser::Formatter;
use serde_json::{Map, Number, Value};

#[derive(Debug, thiserror::Error)]
pub enum CanonicalError {
    #[error("the attestation does not read: {0}")]
    Read(serde_json::Error),
    #[error("the attestation was not written: {0}")]
    Write(serde_json::Error),
    #[error("timestamp {seconds}s is beyond what milliseconds can hold")]
    TimestampOverflow { seconds: i64 },
    #[error("{what} has no float64 Go would read it as")]
    NotAFloat64 { what: String },
}

/// The fields a signature covers, in the order Go declares them. Map keys come
/// out sorted because serde_json's Map is a BTreeMap; were preserve_order ever
/// turned on in this workspace, the vectors in ats/signing/testdata would fail.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Signable {
    pub id: String,
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    #[serde(rename = "timestamp")]
    pub timestamp_ms: i64,
    pub source: String,
    #[serde(skip_serializing_if = "Map::is_empty")]
    pub attributes: Map<String, Value>,
}

/// An attestation as JSON, with every signed field named. `attributes` alone
/// may be absent, as Go's `omitempty` leaves it out.
#[derive(Deserialize)]
struct Fields {
    id: String,
    subjects: Vec<String>,
    predicates: Vec<String>,
    contexts: Vec<String>,
    actors: Vec<String>,
    timestamp: i64,
    source: String,
    #[serde(default)]
    attributes: Map<String, Value>,
}

impl Signable {
    /// From the wire Go signs: `timestamp` in milliseconds.
    pub fn from_wire_json(json: &str) -> Result<Self, CanonicalError> {
        let f: Fields = serde_json::from_str(json).map_err(CanonicalError::Read)?;
        let ms = f.timestamp;
        Self::from_fields(f, ms)
    }

    /// From an attestation in proto JSON, as the browser holds it: `timestamp`
    /// in seconds. The node stores it as those seconds, so what it verifies is
    /// their milliseconds.
    pub fn from_proto_json(json: &str) -> Result<Self, CanonicalError> {
        let f: Fields = serde_json::from_str(json).map_err(CanonicalError::Read)?;
        let seconds = f.timestamp;
        let ms = seconds
            .checked_mul(1000)
            .ok_or(CanonicalError::TimestampOverflow { seconds })?;
        Self::from_fields(f, ms)
    }

    fn from_fields(f: Fields, timestamp_ms: i64) -> Result<Self, CanonicalError> {
        Ok(Signable {
            id: f.id,
            subjects: f.subjects,
            predicates: f.predicates,
            contexts: f.contexts,
            actors: f.actors,
            timestamp_ms,
            source: f.source,
            attributes: as_go_reads_map(f.attributes)?,
        })
    }

    /// The bytes a signature is over.
    pub fn canonical_json(&self) -> Result<String, CanonicalError> {
        let mut out = Vec::new();
        let mut ser = serde_json::Serializer::with_formatter(&mut out, GoFormatter);
        self.serialize(&mut ser).map_err(CanonicalError::Write)?;
        // The serializer writes str and escapes; it has nothing else to write.
        String::from_utf8(out).map_err(|e| CanonicalError::Write(serde::ser::Error::custom(e)))
    }
}

/// Every number as the float64 Go's json.Unmarshal reads it into.
fn as_go_reads(value: Value) -> Result<Value, CanonicalError> {
    Ok(match value {
        Value::Number(n) => {
            let what = || CanonicalError::NotAFloat64 { what: n.to_string() };
            let f = n.as_f64().ok_or_else(what)?;
            Value::Number(Number::from_f64(f).ok_or_else(what)?)
        }
        Value::Array(items) => Value::Array(
            items
                .into_iter()
                .map(as_go_reads)
                .collect::<Result<_, _>>()?,
        ),
        Value::Object(map) => Value::Object(as_go_reads_map(map)?),
        v @ (Value::Null | Value::Bool(_) | Value::String(_)) => v,
    })
}

fn as_go_reads_map(map: Map<String, Value>) -> Result<Map<String, Value>, CanonicalError> {
    map.into_iter()
        .map(|(k, v)| Ok((k, as_go_reads(v)?)))
        .collect()
}

/// What json.Marshal writes beyond what serde_json does: HTML escaping, and a
/// float64 in Go's form.
struct GoFormatter;

impl Formatter for GoFormatter {
    fn write_string_fragment<W: ?Sized + io::Write>(
        &mut self,
        writer: &mut W,
        fragment: &str,
    ) -> io::Result<()> {
        let mut utf8 = [0u8; 4];
        for c in fragment.chars() {
            match c {
                '<' => writer.write_all(b"\\u003c")?,
                '>' => writer.write_all(b"\\u003e")?,
                '&' => writer.write_all(b"\\u0026")?,
                '\u{2028}' => writer.write_all(b"\\u2028")?,
                '\u{2029}' => writer.write_all(b"\\u2029")?,
                c => writer.write_all(c.encode_utf8(&mut utf8).as_bytes())?,
            }
        }
        Ok(())
    }

    fn write_f64<W: ?Sized + io::Write>(&mut self, writer: &mut W, value: f64) -> io::Result<()> {
        writer.write_all(go_float(value).as_bytes())
    }
}

/// A float64 as encoding/json prints it: the shortest digits that read back,
/// in exponent form below 1e-6 or from 1e21, the exponent's sign written.
fn go_float(f: f64) -> String {
    let abs = f.abs();
    if abs == 0.0 || (1e-6..1e21).contains(&abs) {
        f.to_string()
    } else if abs >= 1e21 {
        format!("{f:e}").replacen('e', "e+", 1)
    } else {
        format!("{f:e}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Deserialize)]
    struct Vector {
        name: String,
        wire: Value,
        canonical: String,
    }

    fn vectors() -> Vec<Vector> {
        serde_json::from_str(include_str!(
            "../../../ats/signing/testdata/canonical.json"
        ))
        .unwrap()
    }

    #[test]
    fn writes_what_go_wrote() {
        for v in vectors() {
            let wire = serde_json::to_string(&v.wire).unwrap();
            let got = Signable::from_wire_json(&wire)
                .unwrap()
                .canonical_json()
                .unwrap();
            assert_eq!(got, v.canonical, "{}", v.name);
        }
    }

    #[test]
    fn proto_seconds_sign_as_their_milliseconds() {
        let proto = r#"{"id":"AS-1","subjects":["alice"],"predicates":["knows"],"contexts":["work"],"actors":["bob"],"timestamp":1735689600,"source":"test","created_at":1735689600,"signature":"","signer_did":""}"#;
        let got = Signable::from_proto_json(proto)
            .unwrap()
            .canonical_json()
            .unwrap();
        let first = &vectors()[0];
        assert_eq!(first.name, "no attributes");
        assert_eq!(got, first.canonical);
    }

    #[test]
    fn a_missing_field_is_named_not_filled() {
        let err = Signable::from_wire_json(
            r#"{"id":"AS-1","subjects":[],"predicates":[],"contexts":[],"timestamp":0,"source":""}"#,
        )
        .unwrap_err();
        assert!(err.to_string().contains("actors"), "{err}");
    }

    #[test]
    fn null_attributes_are_refused_not_read_as_none() {
        let err = Signable::from_wire_json(
            r#"{"id":"AS-1","subjects":[],"predicates":[],"contexts":[],"actors":[],"timestamp":0,"source":"","attributes":null}"#,
        )
        .unwrap_err();
        assert!(matches!(err, CanonicalError::Read(_)), "{err}");
    }

    #[test]
    fn seconds_that_overflow_milliseconds_are_refused() {
        let proto = format!(
            r#"{{"id":"x","subjects":[],"predicates":[],"contexts":[],"actors":[],"timestamp":{},"source":""}}"#,
            i64::MAX
        );
        assert!(matches!(
            Signable::from_proto_json(&proto),
            Err(CanonicalError::TimestampOverflow { .. })
        ));
    }

    /// A timestamp is an int64 in Go and printed as one, past where a float64
    /// would round it.
    #[test]
    fn a_timestamp_is_an_integer_not_a_float() {
        let wire = r#"{"id":"x","subjects":[],"predicates":[],"contexts":[],"actors":[],"timestamp":9007199254740993,"source":""}"#;
        let got = Signable::from_wire_json(wire)
            .unwrap()
            .canonical_json()
            .unwrap();
        assert!(got.contains(r#""timestamp":9007199254740993"#), "{got}");
    }
}
