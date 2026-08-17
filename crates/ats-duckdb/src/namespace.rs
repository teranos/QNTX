//! Namespace is the top-level prefix in a storage location (ADR-026). A
//! namespace is named, and carries the DID that owns it — keying it by that DID
//! would let an owner hold exactly one.

/// The node's own namespace. ADR-026 makes it the node, so it is a literal
/// rather than something SUPER created and could delete.
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

    // A namespace owns everything written under it, which is what a prefix is.
    #[test]
    fn a_namespace_owns_the_top_level() {
        assert_eq!(
            prefix("file:///park", "playground", "attestations"),
            "/park/playground/attestations"
        );
    }

    // The node is the system namespace rather than the owner of one, so its
    // name is the one literal — nobody created it and nobody may delete it.
    #[test]
    fn the_system_namespace_is_a_literal() {
        assert_eq!(
            prefix("file:///park", SYSTEM, "identity"),
            "/park/system/identity"
        );
    }

    #[test]
    fn a_remote_location_keeps_its_scheme() {
        assert_eq!(
            prefix("s3://bucket/park", "tenniscourt", "watchers"),
            "s3://bucket/park/tenniscourt/watchers"
        );
    }

    #[test]
    fn a_trailing_slash_does_not_double() {
        assert_eq!(prefix("file:///park/", "pond", "ducks"), "/park/pond/ducks");
    }

    // Some things sit at the namespace root rather than under a kind.
    #[test]
    fn a_namespace_has_a_root_of_its_own() {
        assert_eq!(root("file:///park", SYSTEM), "/park/system");
    }
}
