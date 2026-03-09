//! Attestation System Unique IDs (ASUIDs).
//!
//! An ASUID is a human-readable identifier with a random suffix for uniqueness.
//! The SPC segments make attestations recognizable in logs without lookup.
//!
//! ## Structure
//!
//! ```text
//! AS-SARAH-AUTHOR-GITHUB-7K4M3B9X
//! ╰prefix╯╰─S──╯╰──P──╯╰──C──╯╰─suffix──╯
//! ```
//!
//! - **Prefix**: 2-letter domain (`AS` attestation, `JB` job, `PX` pulse execution)
//! - **S, P, C**: Truncated subject, predicate, context for log readability
//! - **Suffix**: Random characters from QNTX alphabet for uniqueness

use crate::alphabet::{clean_seed, ALPHABET};

/// Maximum length for each SPC display segment in an ASUID.
const SEGMENT_MAX_LEN: usize = 8;

/// Number of random suffix characters in the full ASUID.
const SUFFIX_LEN: usize = 8;

/// Number of suffix characters shown in the short display form.
const SUFFIX_SHORT: usize = 4;

/// An attestation identifier with readable SPC segments and random suffix.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Asuid {
    prefix: [u8; 2],
    subject: String,
    predicate: String,
    context: String,
    suffix: String,
}

impl Asuid {
    /// Build an ASUID from its components and random bytes.
    ///
    /// The caller provides randomness — the crate has no RNG dependency.
    /// Go callers use `crypto/rand`, browser uses `crypto.getRandomValues`.
    ///
    /// Returns `None` if the prefix is invalid or insufficient random bytes.
    ///
    /// ```
    /// use qntx_id::Asuid;
    ///
    /// let random_bytes = [0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6, 0xA7, 0xB8];
    /// let id = Asuid::new("AS", "Sarah", "author_of", "GitHub", &random_bytes);
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
        random_bytes: &[u8],
    ) -> Option<Self> {
        let prefix_bytes = validate_prefix(prefix)?;
        if random_bytes.len() < SUFFIX_LEN {
            return None;
        }
        let suffix = derive_suffix(random_bytes);

        Some(Self {
            prefix: prefix_bytes,
            subject: truncate_segment(subject),
            predicate: truncate_segment(predicate),
            context: truncate_segment(context),
            suffix,
        })
    }

    /// The 2-letter prefix (e.g. "AS", "JB", "PX").
    pub fn prefix(&self) -> &str {
        std::str::from_utf8(&self.prefix).unwrap()
    }

    /// Full ASUID string with all suffix characters.
    ///
    /// ```
    /// use qntx_id::Asuid;
    ///
    /// let bytes = [0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6, 0xA7, 0xB8];
    /// let id = Asuid::new("AS", "Alice", "knows", "work", &bytes).unwrap();
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
            self.suffix,
        )
    }

    /// Short display form with truncated suffix (for logs).
    pub fn short(&self) -> String {
        format!(
            "{}-{}-{}-{}-{}",
            self.prefix(),
            self.subject,
            self.predicate,
            self.context,
            &self.suffix[..SUFFIX_SHORT],
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

/// Derive the suffix from random bytes mapped to the QNTX alphabet.
fn derive_suffix(random_bytes: &[u8]) -> String {
    random_bytes[..SUFFIX_LEN]
        .iter()
        .map(|&b| ALPHABET[(b as usize) % ALPHABET.len()] as char)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    const BYTES_A: [u8; 8] = [0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6, 0xA7, 0xB8];
    const BYTES_B: [u8; 8] = [0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77];

    #[test]
    fn basic_construction() {
        let id = Asuid::new("AS", "Sarah", "author", "GitHub", &BYTES_A).unwrap();
        assert_eq!(id.prefix(), "AS");
        assert!(id.to_string().starts_with("AS-SARAH-AUTHOR-GITHUB-"));
    }

    #[test]
    fn deterministic_given_same_bytes() {
        let id1 = Asuid::new("AS", "Alice", "knows", "work", &BYTES_A).unwrap();
        let id2 = Asuid::new("AS", "Alice", "knows", "work", &BYTES_A).unwrap();
        assert_eq!(id1.to_string(), id2.to_string());
    }

    #[test]
    fn different_bytes_different_suffix() {
        let id1 = Asuid::new("AS", "Alice", "knows", "work", &BYTES_A).unwrap();
        let id2 = Asuid::new("AS", "Alice", "knows", "work", &BYTES_B).unwrap();
        assert_ne!(id1.to_string(), id2.to_string());
        // SPC segments should match
        assert_eq!(id1.subject, id2.subject);
        assert_eq!(id1.predicate, id2.predicate);
        assert_eq!(id1.context, id2.context);
    }

    #[test]
    fn short_form_fewer_suffix_chars() {
        let id = Asuid::new("AS", "Alice", "knows", "work", &BYTES_A).unwrap();
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
            &BYTES_A,
        )
        .unwrap();
        let full = id.to_string();
        let parts: Vec<&str> = full.split('-').collect();
        assert!(parts[1].len() <= SEGMENT_MAX_LEN);
        assert!(parts[2].len() <= SEGMENT_MAX_LEN);
        assert!(parts[3].len() <= SEGMENT_MAX_LEN);
    }

    #[test]
    fn segments_are_uppercased() {
        let id = Asuid::new("AS", "alice", "knows", "work", &BYTES_A).unwrap();
        assert!(id.to_string().starts_with("AS-ALICE-KNOWS-WORK-"));
    }

    #[test]
    fn prefixes() {
        assert!(Asuid::new("AS", "s", "p", "c", &BYTES_A).is_some());
        assert!(Asuid::new("JB", "s", "p", "c", &BYTES_A).is_some());
        assert!(Asuid::new("PX", "s", "p", "c", &BYTES_A).is_some());
    }

    #[test]
    fn invalid_prefix() {
        assert!(Asuid::new("A", "s", "p", "c", &BYTES_A).is_none());
        assert!(Asuid::new("ABC", "s", "p", "c", &BYTES_A).is_none());
        assert!(Asuid::new("as", "s", "p", "c", &BYTES_A).is_none());
        assert!(Asuid::new("12", "s", "p", "c", &BYTES_A).is_none());
    }

    #[test]
    fn insufficient_random_bytes() {
        assert!(Asuid::new("AS", "s", "p", "c", &[0x01, 0x02]).is_none());
        assert!(Asuid::new("AS", "s", "p", "c", &[]).is_none());
    }

    #[test]
    fn display_matches_full() {
        let id = Asuid::new("AS", "Sarah", "author", "GitHub", &BYTES_A).unwrap();
        assert_eq!(format!("{}", id), id.full());
    }

    #[test]
    fn unicode_subjects() {
        let id = Asuid::new("AS", "Müller", "café", "Straße", &BYTES_A).unwrap();
        let s = id.to_string();
        assert!(s.contains("MULER"));
        assert!(s.contains("CAFE"));
        assert!(s.contains("STRASE"));
    }

    #[test]
    fn suffix_uses_alphabet_chars_only() {
        // All possible byte values should produce valid alphabet chars
        let bytes: Vec<u8> = (0..=255).collect();
        for chunk in bytes.chunks(SUFFIX_LEN) {
            if chunk.len() < SUFFIX_LEN {
                break;
            }
            let id = Asuid::new("AS", "s", "p", "c", chunk).unwrap();
            let suffix = id.full().split('-').last().unwrap().to_string();
            for c in suffix.chars() {
                assert!(
                    matches!(c, '2'..='9' | 'A'..='Z'),
                    "unexpected character in suffix: {}",
                    c
                );
            }
        }
    }
}
