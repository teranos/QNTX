//! Token types for the AX lexer

use std::fmt;

/// Token kinds in the AX query language
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TokenKind {
    // Identifiers and literals
    Identifier,
    QuotedString,
    NaturalPredicate,

    // Clause keywords
    Is,
    Are,
    Of,
    From,
    By,
    Via,

    // Temporal keywords
    Since,
    Until,
    On,
    Between,
    Over,

    // Connectors
    And,

    // Action keywords
    So,
    Therefore,

    // Special
    Eof,
    Unknown,
    Wildcard, // Explicit wildcard token for rejecting '*'
    Pipe,     // Explicit pipe token for rejecting '|' (claim key separator)

}

impl TokenKind {
    /// Check if this is a clause keyword
    pub fn is_clause_keyword(&self) -> bool {
        matches!(
            self,
            TokenKind::Is
                | TokenKind::Are
                | TokenKind::Of
                | TokenKind::From
                | TokenKind::By
                | TokenKind::Via
        )
    }

    /// Check if this is a temporal keyword
    pub fn is_temporal_keyword(&self) -> bool {
        matches!(
            self,
            TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over
        )
    }
}

impl fmt::Display for TokenKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TokenKind::Identifier => write!(f, "identifier"),
            TokenKind::QuotedString => write!(f, "quoted string"),
            TokenKind::NaturalPredicate => write!(f, "natural predicate"),
            TokenKind::Is => write!(f, "'is'"),
            TokenKind::Are => write!(f, "'are'"),
            TokenKind::Of => write!(f, "'of'"),
            TokenKind::From => write!(f, "'from'"),
            TokenKind::By => write!(f, "'by'"),
            TokenKind::Via => write!(f, "'via'"),
            TokenKind::Since => write!(f, "'since'"),
            TokenKind::Until => write!(f, "'until'"),
            TokenKind::On => write!(f, "'on'"),
            TokenKind::Between => write!(f, "'between'"),
            TokenKind::Over => write!(f, "'over'"),
            TokenKind::And => write!(f, "'and'"),
            TokenKind::So => write!(f, "'so'"),
            TokenKind::Therefore => write!(f, "'therefore'"),
            TokenKind::Eof => write!(f, "end of input"),
            TokenKind::Unknown => write!(f, "unknown"),
            TokenKind::Wildcard => write!(f, "wildcard '*'"),
            TokenKind::Pipe => write!(f, "pipe '|'"),
        }
    }
}

/// A token with position information
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Token<'a> {
    pub kind: TokenKind,
    pub text: &'a str,
    pub offset: usize,
}

impl<'a> Token<'a> {
    pub fn new(kind: TokenKind, text: &'a str, offset: usize) -> Self {
        Self { kind, text, offset }
    }

    pub fn eof(offset: usize) -> Self {
        Self {
            kind: TokenKind::Eof,
            text: "",
            offset,
        }
    }
}
