//! Keyword classification for AX query lexer
//!
//! Uses compile-time perfect hashing (phf) for O(1) keyword lookup.

// Allow dead_code - these functions will be used by the parser in Phase 2
#![allow(dead_code)]

use phf::phf_map;

use super::TokenKind;

/// Keyword classification result
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KeywordKind {
    /// Grammatical connector (is, are)
    Grammatical,
    /// Context transition (of, from)
    ContextTransition,
    /// Actor transition (by, via)
    ActorTransition,
    /// Temporal keyword (since, until, on, between, over)
    Temporal,
    /// Action keyword (so, therefore)
    Action,
    /// Natural language predicate (speaks, knows, works, etc.)
    NaturalPredicate,
    /// Conjunction (and)
    Conjunction,
}

/// Static map of keywords to their token kinds
/// Uses compile-time perfect hashing for O(1) lookup
static KEYWORDS: phf::Map<&'static str, TokenKind> = phf_map! {
    // Grammatical connectors
    "is" => TokenKind::Is,
    "are" => TokenKind::Are,

    // Context transitions
    "of" => TokenKind::Of,
    "from" => TokenKind::From,

    // Actor transitions
    "by" => TokenKind::By,
    "via" => TokenKind::Via,

    // Temporal keywords
    "since" => TokenKind::Since,
    "until" => TokenKind::Until,
    "on" => TokenKind::On,
    "between" => TokenKind::Between,
    "over" => TokenKind::Over,

    // Conjunction
    "and" => TokenKind::And,

    // Action keywords
    "so" => TokenKind::So,
    "therefore" => TokenKind::Therefore,

    // Natural language predicates (singular and plural forms)
    "speak" => TokenKind::NaturalPredicate,
    "speaks" => TokenKind::NaturalPredicate,
    "know" => TokenKind::NaturalPredicate,
    "knows" => TokenKind::NaturalPredicate,
    "work" => TokenKind::NaturalPredicate,
    "works" => TokenKind::NaturalPredicate,
    "worked" => TokenKind::NaturalPredicate,
    "study" => TokenKind::NaturalPredicate,
    "studied" => TokenKind::NaturalPredicate,
    "has" => TokenKind::NaturalPredicate,
    "have" => TokenKind::NaturalPredicate,
    "has_experience" => TokenKind::NaturalPredicate,
    "occupation" => TokenKind::NaturalPredicate,
};

/// Static map for keyword kind classification
static KEYWORD_KINDS: phf::Map<&'static str, KeywordKind> = phf_map! {
    // Grammatical connectors
    "is" => KeywordKind::Grammatical,
    "are" => KeywordKind::Grammatical,

    // Context transitions
    "of" => KeywordKind::ContextTransition,
    "from" => KeywordKind::ContextTransition,

    // Actor transitions
    "by" => KeywordKind::ActorTransition,
    "via" => KeywordKind::ActorTransition,

    // Temporal keywords
    "since" => KeywordKind::Temporal,
    "until" => KeywordKind::Temporal,
    "on" => KeywordKind::Temporal,
    "between" => KeywordKind::Temporal,
    "over" => KeywordKind::Temporal,

    // Conjunction
    "and" => KeywordKind::Conjunction,

    // Action keywords
    "so" => KeywordKind::Action,
    "therefore" => KeywordKind::Action,

    // Natural language predicates
    "speak" => KeywordKind::NaturalPredicate,
    "speaks" => KeywordKind::NaturalPredicate,
    "know" => KeywordKind::NaturalPredicate,
    "knows" => KeywordKind::NaturalPredicate,
    "work" => KeywordKind::NaturalPredicate,
    "works" => KeywordKind::NaturalPredicate,
    "worked" => KeywordKind::NaturalPredicate,
    "study" => KeywordKind::NaturalPredicate,
    "studied" => KeywordKind::NaturalPredicate,
    "has" => KeywordKind::NaturalPredicate,
    "have" => KeywordKind::NaturalPredicate,
    "has_experience" => KeywordKind::NaturalPredicate,
    "occupation" => KeywordKind::NaturalPredicate,
};

/// Context keywords for natural language splitting
static CONTEXT_KEYWORDS: phf::Set<&'static str> = phf::phf_set! {
    "of", "from", "by", "via", "at", "in", "for", "with"
};

/// Look up a keyword and return its TokenKind
/// Returns None if the word is not a keyword
#[inline]
pub fn lookup_keyword(word: &str) -> Option<TokenKind> {
    // Keywords are case-insensitive, so we need to lowercase
    // For performance, check common lengths first
    let lower = word.to_ascii_lowercase();
    KEYWORDS.get(lower.as_str()).copied()
}

/// Classify a keyword and return its KeywordKind
/// Returns None if the word is not a keyword
#[inline]
pub fn classify_keyword(word: &str) -> Option<KeywordKind> {
    let lower = word.to_ascii_lowercase();
    KEYWORD_KINDS.get(lower.as_str()).copied()
}

/// Check if a word is a context keyword (for natural language splitting)
#[inline]
pub fn is_context_keyword(word: &str) -> bool {
    let lower = word.to_ascii_lowercase();
    CONTEXT_KEYWORDS.contains(lower.as_str())
}

/// Check if a word is any AX keyword
#[inline]
pub fn is_keyword(word: &str) -> bool {
    let lower = word.to_ascii_lowercase();
    KEYWORDS.contains_key(lower.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_keyword_lookup() {
        assert_eq!(lookup_keyword("is"), Some(TokenKind::Is));
        assert_eq!(lookup_keyword("IS"), Some(TokenKind::Is));
        assert_eq!(lookup_keyword("Is"), Some(TokenKind::Is));
        assert_eq!(lookup_keyword("are"), Some(TokenKind::Are));
        assert_eq!(lookup_keyword("of"), Some(TokenKind::Of));
        assert_eq!(lookup_keyword("from"), Some(TokenKind::From));
        assert_eq!(lookup_keyword("by"), Some(TokenKind::By));
        assert_eq!(lookup_keyword("via"), Some(TokenKind::Via));
        assert_eq!(lookup_keyword("since"), Some(TokenKind::Since));
        assert_eq!(lookup_keyword("until"), Some(TokenKind::Until));
        assert_eq!(lookup_keyword("on"), Some(TokenKind::On));
        assert_eq!(lookup_keyword("between"), Some(TokenKind::Between));
        assert_eq!(lookup_keyword("over"), Some(TokenKind::Over));
        assert_eq!(lookup_keyword("so"), Some(TokenKind::So));
        assert_eq!(lookup_keyword("therefore"), Some(TokenKind::Therefore));
        assert_eq!(lookup_keyword("speaks"), Some(TokenKind::NaturalPredicate));
        assert_eq!(lookup_keyword("knows"), Some(TokenKind::NaturalPredicate));
        assert_eq!(lookup_keyword("notakeyword"), None);
        assert_eq!(lookup_keyword("ALICE"), None);
    }

    #[test]
    fn test_keyword_classification() {
        assert_eq!(classify_keyword("is"), Some(KeywordKind::Grammatical));
        assert_eq!(classify_keyword("are"), Some(KeywordKind::Grammatical));
        assert_eq!(classify_keyword("of"), Some(KeywordKind::ContextTransition));
        assert_eq!(
            classify_keyword("from"),
            Some(KeywordKind::ContextTransition)
        );
        assert_eq!(classify_keyword("by"), Some(KeywordKind::ActorTransition));
        assert_eq!(classify_keyword("via"), Some(KeywordKind::ActorTransition));
        assert_eq!(classify_keyword("since"), Some(KeywordKind::Temporal));
        assert_eq!(classify_keyword("over"), Some(KeywordKind::Temporal));
        assert_eq!(classify_keyword("so"), Some(KeywordKind::Action));
        assert_eq!(
            classify_keyword("speaks"),
            Some(KeywordKind::NaturalPredicate)
        );
    }

    #[test]
    fn test_context_keywords() {
        assert!(is_context_keyword("of"));
        assert!(is_context_keyword("from"));
        assert!(is_context_keyword("by"));
        assert!(is_context_keyword("via"));
        assert!(is_context_keyword("at"));
        assert!(is_context_keyword("in"));
        assert!(is_context_keyword("for"));
        assert!(is_context_keyword("with"));
        assert!(is_context_keyword("OF")); // Case insensitive
        assert!(!is_context_keyword("is"));
        assert!(!is_context_keyword("author"));
    }

    #[test]
    fn test_is_keyword() {
        assert!(is_keyword("is"));
        assert!(is_keyword("ARE"));
        assert!(is_keyword("speaks"));
        assert!(!is_keyword("ALICE"));
        assert!(!is_keyword("author_of"));
    }
}
