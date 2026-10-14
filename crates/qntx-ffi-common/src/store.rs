//! A store's refusal as it crosses the FFI: the sacred shape, so the caller on
//! the other side reads the kind of refusal from its region rather than its
//! wording.

use ats::storage::StoreError;

/// The region of a refusal `Attestation::writable` made: the caller was told
/// what no store takes, and asking again unchanged is refused again.
pub const UNWRITABLE: &str = "unwritable";

/// The trait's error as the sacred shape. A `Backend` already carries the
/// shape as its string; the trait's own variants are built into one here.
pub fn sacred(e: StoreError, surface: &str, call: &str) -> String {
    let region = match &e {
        StoreError::AlreadyExists(_) => "already-exists",
        StoreError::NotFound(_) => "not-found",
        StoreError::Unwritable(_) => UNWRITABLE,
        StoreError::InvalidData(_) => "invalid-data",
        StoreError::Backend(_) => "backend",
        StoreError::Query(_) => "query",
        StoreError::Serialization(_) => "serialization",
        StoreError::QuotaExceeded { .. } => "quota-exceeded",
    };
    if let StoreError::Backend(json) = &e {
        if serde_json::from_str::<laye_error::Error>(json).is_ok() {
            return json.clone();
        }
    }
    // A clock before 1970 is a time too, and said as one.
    let at = match std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH) {
        Ok(since) => since.as_millis().to_string(),
        Err(before) => format!("-{}", before.duration().as_millis()),
    };
    let sacred = laye_error::Error {
        id: format!("err-{surface}-{at}"),
        severity: laye_error::Severity::Error,
        context: laye_error::Context {
            surface: surface.to_string(),
            region: Some(region.to_string()),
            anchor: None,
        },
        title: e.to_string(),
        why: String::new(),
        trace: Vec::new(),
        raw: Some(format!("{e:?}")),
        at,
        source: None,
        ffi_call: Some(call.to_string()),
        location: None,
        js_stack: None,
        raw_stderr: None,
        requires_reload: false,
    };
    // Strings and a few enums: there is nothing in it that does not encode.
    // Were there, the text still crosses, unshaped, and is read as it came.
    match serde_json::to_string(&sacred) {
        Ok(json) => json,
        Err(why) => format!(
            "{} (and it did not encode as the sacred shape: {why})",
            sacred.title
        ),
    }
}
