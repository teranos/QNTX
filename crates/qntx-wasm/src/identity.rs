//! ASUID generation and ID normalization for both WASM targets.
//!
//! Wraps qntx-id functions for use through the WASM bridge.
//! Used by both the wazero (raw memory ABI) and browser (wasm-bindgen) targets.

/// JSON input for ASUID generation.
#[derive(serde::Deserialize)]
pub(crate) struct AsuidInput {
    pub prefix: String,
    pub subject: String,
    pub predicate: String,
    pub context: String,
    pub content_hash: String,
}

/// Generate an ASUID from JSON input, returning JSON with full and short forms.
pub(crate) fn generate_asuid_impl(input: &str) -> String {
    let parsed: AsuidInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(
                r#"{{"error":"invalid JSON: {}"}}"#,
                e.to_string().replace('"', "\\\"")
            )
        }
    };

    match qntx_id::Asuid::new(
        &parsed.prefix,
        &parsed.subject,
        &parsed.predicate,
        &parsed.context,
        &parsed.content_hash,
    ) {
        Some(id) => format!(r#"{{"full":"{}","short":"{}"}}"#, id.full(), id.short()),
        None => r#"{"error":"invalid ASUID input: check prefix (2 uppercase letters) and content_hash (>= 16 hex chars)"}"#.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const HASH: &str =
        "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2";

    #[test]
    fn generate_asuid_basic() {
        let input = serde_json::json!({
            "prefix": "AS",
            "subject": "Sarah",
            "predicate": "author",
            "context": "GitHub",
            "content_hash": HASH
        });
        let result = generate_asuid_impl(&input.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"].is_null(), "unexpected error: {}", result);
        assert!(parsed["full"]
            .as_str()
            .unwrap()
            .starts_with("AS-SARAH-AUTHOR-GITHUB-"));
        assert!(parsed["short"]
            .as_str()
            .unwrap()
            .starts_with("AS-SARAH-AUTHOR-GITHUB-"));
        let full_suffix = parsed["full"].as_str().unwrap().split('-').last().unwrap();
        let short_suffix = parsed["short"]
            .as_str()
            .unwrap()
            .split('-')
            .last()
            .unwrap();
        assert_eq!(full_suffix.len(), 8);
        assert_eq!(short_suffix.len(), 4);
    }

    #[test]
    fn generate_asuid_deterministic() {
        let input = serde_json::json!({
            "prefix": "AS",
            "subject": "Alice",
            "predicate": "knows",
            "context": "work",
            "content_hash": HASH
        });
        let json = input.to_string();
        let r1 = generate_asuid_impl(&json);
        let r2 = generate_asuid_impl(&json);
        assert_eq!(r1, r2);
    }

    #[test]
    fn generate_asuid_invalid_prefix() {
        let input = serde_json::json!({
            "prefix": "as",
            "subject": "s",
            "predicate": "p",
            "context": "c",
            "content_hash": HASH
        });
        let result = generate_asuid_impl(&input.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"].as_str().is_some());
    }

    #[test]
    fn generate_asuid_invalid_json() {
        let result = generate_asuid_impl("not json");
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"].as_str().unwrap().contains("invalid JSON"));
    }
}
