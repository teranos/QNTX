//! Token types for the AX query lexer

use std::fmt;

/// A token produced by the lexer
#[derive(Debug, Clone, PartialEq)]
pub struct Token<'a> {
    /// The kind of token
    pub kind: TokenKind,
    /// The raw text of the token (slice into original input)
    pub text: &'a str,
    /// Byte offset in the original input
    pub offset: usize,
    /// Whether this token was quoted (single quotes)
    pub quoted: bool,
}

impl<'a> Token<'a> {
    /// Create a new token
    pub fn new(kind: TokenKind, text: &'a str, offset: usize) -> Self {
        Self {
            kind,
            text,
            offset,
            quoted: false,
        }
    }

    /// Create a quoted token
    pub fn quoted(kind: TokenKind, text: &'a str, offset: usize) -> Self {
        Self {
            kind,
            text,
            offset,
            quoted: true,
        }
    }

    /// Get the text value, with quotes stripped if it was quoted
    pub fn value(&self) -> &'a str {
        self.text
    }

    /// Get the length of this token in bytes
    pub fn len(&self) -> usize {
        self.text.len()
    }

    /// Check if token is empty
    pub fn is_empty(&self) -> bool {
        self.text.is_empty()
    }
}

impl fmt::Display for Token<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.quoted {
            write!(f, "'{}'", self.text)
        } else {
            write!(f, "{}", self.text)
        }
    }
}

/// The kind of token
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TokenKind {
    // === Grammatical connectors ===
    /// "is" - singular grammatical connector
    Is,
    /// "are" - plural grammatical connector
    Are,

    // === Context transition keywords ===
    /// "of" - context transition
    Of,
    /// "from" - context transition
    From,

    // === Actor transition keywords ===
    /// "by" - actor transition
    By,
    /// "via" - actor transition
    Via,

    // === Temporal keywords ===
    /// "since" - temporal start
    Since,
    /// "until" - temporal end
    Until,
    /// "on" - specific date
    On,
    /// "between" - date range
    Between,
    /// "over" - duration comparison
    Over,
    /// "and" - used in "between X and Y"
    And,

    // === Action keywords ===
    /// "so" - action transition
    So,
    /// "therefore" - action transition
    Therefore,

    // === Natural language predicates ===
    /// Natural language predicates like "speaks", "knows", "works"
    NaturalPredicate,

    // === Literals ===
    /// An identifier (subject, predicate, context, etc.)
    Identifier,
    /// A quoted string (single quotes)
    QuotedString,

    // === Special ===
    /// End of input
    Eof,
    /// Unknown/invalid token
    Unknown,
}

impl TokenKind {
    /// Check if this is a grammatical connector (is/are)
    pub fn is_grammatical(&self) -> bool {
        matches!(self, TokenKind::Is | TokenKind::Are)
    }

    /// Check if this is a context transition keyword (of/from)
    pub fn is_context_transition(&self) -> bool {
        matches!(self, TokenKind::Of | TokenKind::From)
    }

    /// Check if this is an actor transition keyword (by/via)
    pub fn is_actor_transition(&self) -> bool {
        matches!(self, TokenKind::By | TokenKind::Via)
    }

    /// Check if this is a temporal keyword
    pub fn is_temporal(&self) -> bool {
        matches!(
            self,
            TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over
        )
    }

    /// Check if this is an action keyword (so/therefore)
    pub fn is_action(&self) -> bool {
        matches!(self, TokenKind::So | TokenKind::Therefore)
    }

    /// Check if this is any keyword (not an identifier)
    pub fn is_keyword(&self) -> bool {
        !matches!(
            self,
            TokenKind::Identifier | TokenKind::QuotedString | TokenKind::Eof | TokenKind::Unknown
        )
    }
}

impl fmt::Display for TokenKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TokenKind::Is => write!(f, "is"),
            TokenKind::Are => write!(f, "are"),
            TokenKind::Of => write!(f, "of"),
            TokenKind::From => write!(f, "from"),
            TokenKind::By => write!(f, "by"),
            TokenKind::Via => write!(f, "via"),
            TokenKind::Since => write!(f, "since"),
            TokenKind::Until => write!(f, "until"),
            TokenKind::On => write!(f, "on"),
            TokenKind::Between => write!(f, "between"),
            TokenKind::Over => write!(f, "over"),
            TokenKind::And => write!(f, "and"),
            TokenKind::So => write!(f, "so"),
            TokenKind::Therefore => write!(f, "therefore"),
            TokenKind::NaturalPredicate => write!(f, "<natural-predicate>"),
            TokenKind::Identifier => write!(f, "<identifier>"),
            TokenKind::QuotedString => write!(f, "<quoted-string>"),
            TokenKind::Eof => write!(f, "<eof>"),
            TokenKind::Unknown => write!(f, "<unknown>"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_token_kind_categories() {
        assert!(TokenKind::Is.is_grammatical());
        assert!(TokenKind::Are.is_grammatical());
        assert!(!TokenKind::Of.is_grammatical());

        assert!(TokenKind::Of.is_context_transition());
        assert!(TokenKind::From.is_context_transition());

        assert!(TokenKind::By.is_actor_transition());
        assert!(TokenKind::Via.is_actor_transition());

        assert!(TokenKind::Since.is_temporal());
        assert!(TokenKind::Until.is_temporal());
        assert!(TokenKind::On.is_temporal());
        assert!(TokenKind::Between.is_temporal());
        assert!(TokenKind::Over.is_temporal());

        assert!(TokenKind::So.is_action());
        assert!(TokenKind::Therefore.is_action());

        assert!(TokenKind::Is.is_keyword());
        assert!(!TokenKind::Identifier.is_keyword());
        assert!(!TokenKind::QuotedString.is_keyword());
    }

    #[test]
    fn test_token_display() {
        let token = Token::new(TokenKind::Identifier, "ALICE", 0);
        assert_eq!(token.to_string(), "ALICE");

        let quoted = Token::quoted(TokenKind::QuotedString, "hello world", 0);
        assert_eq!(quoted.to_string(), "'hello world'");
    }
}
