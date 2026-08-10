//! Namespace is the top-level prefix in a storage location (ADR-026).

/// The node's own namespace. Every other namespace is a signer's DID, but the
/// node's identity names its namespace, so it cannot be keyed by itself.
pub const SYSTEM: &str = "system";

/// The default project. Visible below SUPER, where system is not (ADR-027).
pub const DEFAULT: &str = "default";

/// Everything a namespace owns at `location`.
pub fn root(location: &str, namespace: &str) -> String {
    let base = location.strip_prefix("file://").unwrap_or(location);
    format!("{}/{namespace}", base.trim_end_matches('/'))
}

/// Where `kind` lives for `namespace` at `location`.
pub fn prefix(location: &str, namespace: &str, kind: &str) -> String {
    format!("{}/{kind}", root(location, namespace))
}

#[cfg(test)]
mod tests {
    use super::*;

    // A namespace is a signer's DID, so it owns everything written under it.
    #[test]
    fn a_namespace_owns_the_top_level() {
        assert_eq!(
            prefix("file:///data", "did:key:zabc", "attestations"),
            "/data/did:key:zabc/attestations"
        );
    }

    // The node's own identity names its namespace, so it cannot live under a
    // path derived from itself. SYSTEM is the one literal.
    #[test]
    fn the_system_namespace_is_a_literal_not_a_did() {
        assert_eq!(
            prefix("file:///data", SYSTEM, "identity"),
            "/data/system/identity"
        );
    }

    #[test]
    fn a_remote_location_keeps_its_scheme() {
        assert_eq!(
            prefix("s3://bucket/q", "did:key:zabc", "watchers"),
            "s3://bucket/q/did:key:zabc/watchers"
        );
    }

    #[test]
    fn a_trailing_slash_does_not_double() {
        assert_eq!(prefix("file:///data/", "ns", "x"), "/data/ns/x");
    }

    // Some things sit at the namespace root rather than under a kind.
    #[test]
    fn a_namespace_has_a_root_of_its_own() {
        assert_eq!(root("file:///data", SYSTEM), "/data/system");
    }
}
