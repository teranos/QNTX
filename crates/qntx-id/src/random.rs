//! Random ID generation using the QNTX alphabet.
//!
//! For non-content-addressed uses: embedding IDs, run IDs, and other cases
//! where uniqueness comes from randomness rather than content.

use crate::alphabet::ALPHABET;

/// Generate a random ID of `len` characters from the QNTX alphabet.
///
/// Uses the provided random bytes as entropy. Each output character consumes
/// one byte from the input, mapped modulo the alphabet size (34 characters).
///
/// # Panics
///
/// Panics if `random_bytes.len() < len`.
///
/// ```
/// use qntx_id::random_id_from_bytes;
///
/// let bytes = [0u8, 33, 17, 8, 25, 5, 10, 30];
/// let id = random_id_from_bytes(4, &bytes);
/// assert_eq!(id.len(), 4);
/// assert!(id.chars().all(|c| matches!(c, '2'..='9' | 'A'..='Z')));
/// ```
pub fn random_id_from_bytes(len: usize, random_bytes: &[u8]) -> String {
    assert!(
        random_bytes.len() >= len,
        "need at least {} random bytes, got {}",
        len,
        random_bytes.len()
    );

    random_bytes[..len]
        .iter()
        .map(|&b| ALPHABET[(b as usize) % ALPHABET.len()] as char)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deterministic_from_bytes() {
        let bytes = [0, 1, 2, 33, 34, 68];
        let id1 = random_id_from_bytes(6, &bytes);
        let id2 = random_id_from_bytes(6, &bytes);
        assert_eq!(id1, id2);
    }

    #[test]
    fn correct_length() {
        let bytes = [0u8; 20];
        assert_eq!(random_id_from_bytes(1, &bytes).len(), 1);
        assert_eq!(random_id_from_bytes(8, &bytes).len(), 8);
        assert_eq!(random_id_from_bytes(20, &bytes).len(), 20);
    }

    #[test]
    fn all_chars_in_alphabet() {
        // Use all possible byte values to ensure we only produce alphabet chars
        let bytes: Vec<u8> = (0..=255).collect();
        let id = random_id_from_bytes(256, &bytes);
        for c in id.chars() {
            assert!(
                matches!(c, '2'..='9' | 'A'..='Z'),
                "unexpected character: {}",
                c
            );
        }
    }

    #[test]
    fn wraps_modulo() {
        // Byte 0 and byte 34 should produce the same character (mod 34)
        let id1 = random_id_from_bytes(1, &[0]);
        let id2 = random_id_from_bytes(1, &[34]);
        assert_eq!(id1, id2);
    }

    #[test]
    #[should_panic(expected = "need at least")]
    fn panics_insufficient_bytes() {
        random_id_from_bytes(5, &[0, 1, 2]);
    }
}
