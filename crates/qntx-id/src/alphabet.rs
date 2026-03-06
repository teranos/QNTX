//! Core alphabet, normalization, and seed cleaning.
//!
//! The QNTX alphabet is Crockford-inspired: uppercase letters and digits 2-9.
//! Characters `0` and `1` are excluded because they look like `O` and `I`.
//! Input containing `0` or `1` is mapped to `O` and `I` respectively.

use unicode_normalization::UnicodeNormalization;

/// The QNTX character set: all uppercase letters plus digits 2-9.
/// Excludes `0` (looks like O) and `1` (looks like I).
pub const ALPHABET: &[u8; 34] = b"23456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

/// Returns true if `c` is a valid QNTX alphabet character.
fn is_alphabet_char(c: char) -> bool {
    matches!(c, '2'..='9' | 'A'..='Z')
}

/// Map a character to the QNTX alphabet.
///
/// - `0` becomes `O`, `1` becomes `I` (visual similarity)
/// - Valid alphabet chars pass through
/// - Everything else is dropped (returns `None`)
fn map_char(c: char) -> Option<char> {
    match c {
        '0' => Some('O'),
        '1' => Some('I'),
        c if is_alphabet_char(c) => Some(c),
        _ => None,
    }
}

/// Convert a string to the QNTX custom alphabet.
///
/// Uppercases the input, maps `0→O` and `1→I`, and drops characters
/// outside the alphabet.
///
/// ```
/// use qntx_id::to_custom_alphabet;
///
/// assert_eq!(to_custom_alphabet("jd0e"), "JDOE");
/// assert_eq!(to_custom_alphabet("h3llo!"), "H3LLO");
/// ```
pub fn to_custom_alphabet(s: &str) -> String {
    s.chars()
        .flat_map(|c| c.to_uppercase())
        .filter_map(map_char)
        .collect()
}

/// Normalize user input for ID lookups.
///
/// Uppercases, maps confusing characters (`0→O`, `1→I`), and strips
/// anything outside the alphabet. Use this for case-insensitive,
/// typo-tolerant ID searches.
///
/// ```
/// use qntx_id::normalize_for_lookup;
///
/// assert_eq!(normalize_for_lookup("jd0e"), "JDOE");
/// assert_eq!(normalize_for_lookup("sbvh"), "SBVH");
/// ```
pub fn normalize_for_lookup(input: &str) -> String {
    to_custom_alphabet(input)
}

/// Normalize a Unicode string to ASCII.
///
/// Applies NFKD decomposition, strips combining marks (accents),
/// and transliterates common ligatures: `ß→ss`, `æ→ae`, `œ→oe`.
///
/// ```
/// use qntx_id::normalize_to_ascii;
///
/// assert_eq!(normalize_to_ascii("Müller"), "Muller");
/// assert_eq!(normalize_to_ascii("Straße"), "Strasse");
/// assert_eq!(normalize_to_ascii("Ægir"), "AEgir");
/// ```
pub fn normalize_to_ascii(s: &str) -> String {
    let mut result = String::with_capacity(s.len());

    for c in s.chars() {
        match c {
            'ß' => result.push_str("ss"),
            'æ' | 'Æ' => result.push_str(if c == 'Æ' { "AE" } else { "ae" }),
            'œ' | 'Œ' => result.push_str(if c == 'Œ' { "OE" } else { "oe" }),
            _ => {
                // NFKD decomposes characters: é → e + combining accent
                // We keep only non-combining (base) characters
                for decomposed in c.nfkd() {
                    if !unicode_normalization::char::is_combining_mark(decomposed) {
                        result.push(decomposed);
                    }
                }
            }
        }
    }

    result
}

/// Clean a seed string for ID generation.
///
/// 1. Normalize Unicode to ASCII
/// 2. Uppercase
/// 3. Keep only alphanumeric characters
/// 4. Collapse consecutive duplicate characters (`AABB` → `AB`)
/// 5. Strip leading digits
/// 6. Map to QNTX alphabet (`0→O`, `1→I`, drop invalid)
///
/// ```
/// use qntx_id::clean_seed;
///
/// assert_eq!(clean_seed("Müller"), "MULER");
/// assert_eq!(clean_seed("  hello!! "), "HELO");
/// assert_eq!(clean_seed("007Bond"), "BOND");
/// assert_eq!(clean_seed("aaBBcc"), "ABC");
/// ```
pub fn clean_seed(s: &str) -> String {
    let ascii = normalize_to_ascii(s);

    // Uppercase and keep only alphanumeric
    let upper: String = ascii
        .chars()
        .flat_map(|c| c.to_uppercase())
        .filter(|c| c.is_ascii_alphanumeric())
        .collect();

    // Collapse consecutive duplicates
    let mut collapsed = String::with_capacity(upper.len());
    let mut prev = None;
    for c in upper.chars() {
        if Some(c) != prev {
            collapsed.push(c);
            prev = Some(c);
        }
    }

    // Strip leading digits
    let trimmed = collapsed.trim_start_matches(|c: char| c.is_ascii_digit());

    to_custom_alphabet(trimmed)
}

#[cfg(test)]
mod tests {
    use super::*;

    // ================================================================
    // to_custom_alphabet
    // ================================================================

    #[test]
    fn alphabet_passthrough() {
        assert_eq!(to_custom_alphabet("ALICE"), "ALICE");
        assert_eq!(to_custom_alphabet("23456789"), "23456789");
    }

    #[test]
    fn alphabet_lowercases() {
        assert_eq!(to_custom_alphabet("alice"), "ALICE");
        assert_eq!(to_custom_alphabet("mixedCase"), "MIXEDCASE");
    }

    #[test]
    fn alphabet_maps_confusing_chars() {
        assert_eq!(to_custom_alphabet("0"), "O");
        assert_eq!(to_custom_alphabet("1"), "I");
        assert_eq!(to_custom_alphabet("j0hn"), "JOHN");
        assert_eq!(to_custom_alphabet("a1ice"), "AIICE");
    }

    #[test]
    fn alphabet_strips_invalid() {
        assert_eq!(to_custom_alphabet("hello world!"), "HELLOWORLD");
        assert_eq!(to_custom_alphabet("test@#$%"), "TEST");
        assert_eq!(to_custom_alphabet(""), "");
    }

    // ================================================================
    // normalize_for_lookup
    // ================================================================

    #[test]
    fn lookup_normalization() {
        assert_eq!(normalize_for_lookup("jd0e"), "JDOE");
        assert_eq!(normalize_for_lookup("SBVH"), "SBVH");
        assert_eq!(normalize_for_lookup("test-123"), "TESTI23");
    }

    // ================================================================
    // normalize_to_ascii
    // ================================================================

    #[test]
    fn ascii_accents() {
        assert_eq!(normalize_to_ascii("café"), "cafe");
        assert_eq!(normalize_to_ascii("Müller"), "Muller");
        assert_eq!(normalize_to_ascii("naïve"), "naive");
        assert_eq!(normalize_to_ascii("résumé"), "resume");
    }

    #[test]
    fn ascii_ligatures() {
        assert_eq!(normalize_to_ascii("Straße"), "Strasse");
        assert_eq!(normalize_to_ascii("æther"), "aether");
        assert_eq!(normalize_to_ascii("Æsir"), "AEsir");
        assert_eq!(normalize_to_ascii("œuvre"), "oeuvre");
        assert_eq!(normalize_to_ascii("Œuvre"), "OEuvre");
    }

    #[test]
    fn ascii_plain_passthrough() {
        assert_eq!(normalize_to_ascii("hello"), "hello");
        assert_eq!(normalize_to_ascii("ALICE"), "ALICE");
        assert_eq!(normalize_to_ascii("123"), "123");
    }

    // ================================================================
    // clean_seed
    // ================================================================

    #[test]
    fn seed_basic() {
        assert_eq!(clean_seed("Alice"), "ALICE");
        assert_eq!(clean_seed("ALICE"), "ALICE");
    }

    #[test]
    fn seed_unicode() {
        assert_eq!(clean_seed("Müller"), "MULER");
        assert_eq!(clean_seed("Straße"), "STRASE");
    }

    #[test]
    fn seed_collapses_repeats() {
        assert_eq!(clean_seed("aaBBcc"), "ABC");
        assert_eq!(clean_seed("BBBOB"), "BOB");
    }

    #[test]
    fn seed_strips_leading_digits() {
        assert_eq!(clean_seed("007Bond"), "BOND");
        assert_eq!(clean_seed("123abc"), "ABC");
        assert_eq!(clean_seed("42"), "");
    }

    #[test]
    fn seed_strips_punctuation() {
        assert_eq!(clean_seed("hello, world!"), "HELOWORLD");
        assert_eq!(clean_seed("test@email.com"), "TESTEMAILCOM");
    }

    #[test]
    fn seed_maps_alphabet() {
        assert_eq!(clean_seed("j0hn"), "JOHN");
        assert_eq!(clean_seed("a1ice"), "AIICE");
    }

    #[test]
    fn seed_empty() {
        assert_eq!(clean_seed(""), "");
        assert_eq!(clean_seed("   "), "");
        assert_eq!(clean_seed("!!!"), "");
    }
}
