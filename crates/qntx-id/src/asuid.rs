//! Content-addressed Attestation System Unique IDs (ASUIDs).
//!
//! An ASUID is a deterministic identifier derived from an attestation's content.
//! Same content produces the same ASUID on any node, offline or online.
//!
//! ## Structure
//!
//! ```text
//! AS-SARAH-AUTHOR-GITHUB-7K4M3B9X
//! ╰prefix╯╰─S──╯╰──P──╯╰──C──╯╰hash suffix╯
//! ```
//!
//! - **Prefix**: 2-letter domain (`AS` attestation, `JB` job, `PX` pulse execution)
//! - **S, P, C**: Truncated subject, predicate, context for log readability
//! - **Hash suffix**: Derived from content hash for uniqueness

use crate::alphabet::{clean_seed, ALPHABET};

/// Maximum length for each SPC display segment in an ASUID.
const SEGMENT_MAX_LEN: usize = 8;

/// Number of hash suffix characters in the full ASUID.
const HASH_SUFFIX_LEN: usize = 8;

/// Number of hash suffix characters shown in the short display form.
const HASH_SUFFIX_SHORT: usize = 4;

/// A content-addressed attestation identifier.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Asuid {
    prefix: [u8; 2],
    subject: String,
    predicate: String,
    context: String,
    hash_suffix: String,
}

impl Asuid {
    /// Build an ASUID from its components and a content hash.
    ///
    /// The `content_hash` should be a hex-encoded hash (e.g. from
    /// `qntx_core::sync::content_hash`). The suffix is derived from
    /// these hash bytes mapped to the QNTX alphabet.
    ///
    /// ```
    /// use qntx_id::Asuid;
    ///
    /// let id = Asuid::new(
    ///     "AS",
    ///     "Sarah",
    ///     "author_of",
    ///     "GitHub",
    ///     "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2",
    /// );
    /// assert!(id.is_some());
    /// let id = id.unwrap();
    /// assert!(id.to_string().starts_with("AS-"));
    /// assert_eq!(id.prefix(), "AS");
    /// ```
    pub fn new(
        prefix: &str,
        subject: &str,
        predicate: &str,
        context: &str,
        content_hash: &str,
    ) -> Option<Self> {
        let prefix_bytes = validate_prefix(prefix)?;
        let hash_suffix = derive_suffix(content_hash)?;

        Some(Self {
            prefix: prefix_bytes,
            subject: truncate_segment(subject),
            predicate: truncate_segment(predicate),
            context: truncate_segment(context),
            hash_suffix,
        })
    }

    /// The 2-letter prefix (e.g. "AS", "JB", "PX").
    pub fn prefix(&self) -> &str {
        std::str::from_utf8(&self.prefix).unwrap()
    }

    /// Full ASUID string with all hash suffix characters.
    ///
    /// ```
    /// use qntx_id::Asuid;
    ///
    /// let hash = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2";
    /// let id = Asuid::new("AS", "Alice", "knows", "work", hash).unwrap();
    /// let full = id.to_string();
    /// // AS-ALICE-KNOWS-WORK-XXXXXXXX (8-char suffix)
    /// assert_eq!(full.matches('-').count(), 4);
    /// ```
    pub fn full(&self) -> String {
        format!(
            "{}-{}-{}-{}-{}",
            self.prefix(),
            self.subject,
            self.predicate,
            self.context,
            self.hash_suffix,
        )
    }

    /// Short display form with truncated hash suffix (for logs).
    pub fn short(&self) -> String {
        format!(
            "{}-{}-{}-{}-{}",
            self.prefix(),
            self.subject,
            self.predicate,
            self.context,
            &self.hash_suffix[..HASH_SUFFIX_SHORT],
        )
    }
}

impl std::fmt::Display for Asuid {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.full())
    }
}

/// Validate and extract a 2-character uppercase prefix.
fn validate_prefix(prefix: &str) -> Option<[u8; 2]> {
    let bytes = prefix.as_bytes();
    if bytes.len() != 2 {
        return None;
    }
    // Must be uppercase ASCII letters
    if !bytes[0].is_ascii_uppercase() || !bytes[1].is_ascii_uppercase() {
        return None;
    }
    Some([bytes[0], bytes[1]])
}

/// Clean and truncate an SPC value into a display segment.
fn truncate_segment(value: &str) -> String {
    let cleaned = clean_seed(value);
    if cleaned.len() <= SEGMENT_MAX_LEN {
        cleaned
    } else {
        cleaned[..SEGMENT_MAX_LEN].to_string()
    }
}

/// Derive the hash suffix from a hex-encoded content hash.
///
/// Takes the first N bytes of the hash and maps them to the QNTX alphabet.
fn derive_suffix(content_hash: &str) -> Option<String> {
    if content_hash.len() < HASH_SUFFIX_LEN * 2 {
        return None;
    }

    let suffix: String = (0..HASH_SUFFIX_LEN)
        .map(|i| {
            let hex_byte = &content_hash[i * 2..i * 2 + 2];
            let byte = u8::from_str_radix(hex_byte, 16).unwrap_or(0);
            ALPHABET[(byte as usize) % ALPHABET.len()] as char
        })
        .collect();

    Some(suffix)
}

#[cfg(test)]
mod tests {
    use super::*;

    const HASH_A: &str =
        "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2a3b4c5d6a7b8c9d0e1f2";
    const HASH_B: &str =
        "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff";

    #[test]
    fn basic_construction() {
        let id = Asuid::new("AS", "Sarah", "author", "GitHub", HASH_A).unwrap();
        assert_eq!(id.prefix(), "AS");
        assert!(id.to_string().starts_with("AS-SARAH-AUTHOR-GITHUB-"));
    }

    #[test]
    fn deterministic() {
        let id1 = Asuid::new("AS", "Alice", "knows", "work", HASH_A).unwrap();
        let id2 = Asuid::new("AS", "Alice", "knows", "work", HASH_A).unwrap();
        assert_eq!(id1.to_string(), id2.to_string());
    }

    #[test]
    fn different_hash_different_suffix() {
        let id1 = Asuid::new("AS", "Alice", "knows", "work", HASH_A).unwrap();
        let id2 = Asuid::new("AS", "Alice", "knows", "work", HASH_B).unwrap();
        assert_ne!(id1.to_string(), id2.to_string());
        // SPC segments should match
        assert_eq!(id1.subject, id2.subject);
        assert_eq!(id1.predicate, id2.predicate);
        assert_eq!(id1.context, id2.context);
    }

    #[test]
    fn short_form_fewer_hash_chars() {
        let id = Asuid::new("AS", "Alice", "knows", "work", HASH_A).unwrap();
        let full = id.full();
        let short = id.short();
        assert!(full.len() > short.len());
        assert!(short.starts_with("AS-ALICE-KNOWS-WORK-"));
    }

    #[test]
    fn segments_cleaned_and_truncated() {
        let id = Asuid::new(
            "AS",
            "very long subject name here",
            "also_a_very_long_predicate",
            "context!@#",
            HASH_A,
        )
        .unwrap();
        // Each segment should be at most SEGMENT_MAX_LEN
        let full = id.to_string();
        let parts: Vec<&str> = full.split('-').collect();
        assert!(parts[1].len() <= SEGMENT_MAX_LEN);
        assert!(parts[2].len() <= SEGMENT_MAX_LEN);
        assert!(parts[3].len() <= SEGMENT_MAX_LEN);
    }

    #[test]
    fn segments_are_uppercased() {
        let id = Asuid::new("AS", "alice", "knows", "work", HASH_A).unwrap();
        assert!(id.to_string().starts_with("AS-ALICE-KNOWS-WORK-"));
    }

    #[test]
    fn prefixes() {
        assert!(Asuid::new("AS", "s", "p", "c", HASH_A).is_some());
        assert!(Asuid::new("JB", "s", "p", "c", HASH_A).is_some());
        assert!(Asuid::new("PX", "s", "p", "c", HASH_A).is_some());
    }

    #[test]
    fn invalid_prefix() {
        assert!(Asuid::new("A", "s", "p", "c", HASH_A).is_none());
        assert!(Asuid::new("ABC", "s", "p", "c", HASH_A).is_none());
        assert!(Asuid::new("as", "s", "p", "c", HASH_A).is_none());
        assert!(Asuid::new("12", "s", "p", "c", HASH_A).is_none());
    }

    #[test]
    fn invalid_hash() {
        // Too short
        assert!(Asuid::new("AS", "s", "p", "c", "abcd").is_none());
        // Empty
        assert!(Asuid::new("AS", "s", "p", "c", "").is_none());
    }

    #[test]
    fn display_matches_full() {
        let id = Asuid::new("AS", "Sarah", "author", "GitHub", HASH_A).unwrap();
        assert_eq!(format!("{}", id), id.full());
    }

    #[test]
    fn unicode_subjects() {
        let id = Asuid::new("AS", "Müller", "café", "Straße", HASH_A).unwrap();
        let s = id.to_string();
        // Should be normalized to ASCII and uppercased
        assert!(s.contains("MULER"));
        assert!(s.contains("CAFE"));
        assert!(s.contains("STRASE"));
    }
}
