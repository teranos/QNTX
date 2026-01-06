//! Lexer for AX query language
//!
//! Tokenizes AX query strings into a stream of tokens.
//!
//! # Features
//!
//! - Zero-copy tokenization (tokens reference the original input)
//! - Single-quote support for literal strings
//! - Whitespace handling
//! - Keyword recognition with case-insensitivity
//!
//! # Example
//!
//! ```rust
//! use qntx_parser::ax::Lexer;
//!
//! let input = "ALICE is author_of GitHub";
//! let tokens: Vec<_> = Lexer::new(input).collect();
//! assert_eq!(tokens.len(), 5); // ALICE, is, author_of, GitHub, EOF
//! ```

use super::keywords::lookup_keyword;
use super::token::{Token, TokenKind};

/// A lexer for AX query language
///
/// Implements `Iterator` over `Token`s, allowing for lazy tokenization.
#[derive(Debug, Clone)]
pub struct Lexer<'a> {
    /// The input string being tokenized
    input: &'a str,
    /// Current byte position in the input
    position: usize,
    /// Whether we've emitted the EOF token
    eof_emitted: bool,
}

impl<'a> Lexer<'a> {
    /// Create a new lexer for the given input
    pub fn new(input: &'a str) -> Self {
        Self {
            input,
            position: 0,
            eof_emitted: false,
        }
    }

    /// Get the remaining input (for debugging)
    pub fn remaining(&self) -> &'a str {
        &self.input[self.position..]
    }

    /// Get the current position
    pub fn position(&self) -> usize {
        self.position
    }

    /// Peek at the next character without consuming it
    fn peek(&self) -> Option<char> {
        self.remaining().chars().next()
    }

    /// Advance the position by n bytes
    fn advance(&mut self, n: usize) {
        self.position = (self.position + n).min(self.input.len());
    }

    /// Advance by one character, returning its byte length
    fn advance_char(&mut self) -> usize {
        if let Some(c) = self.peek() {
            let len = c.len_utf8();
            self.advance(len);
            len
        } else {
            0
        }
    }

    /// Skip whitespace and return the number of bytes skipped
    fn skip_whitespace(&mut self) -> usize {
        let start = self.position;
        while let Some(c) = self.peek() {
            if c.is_whitespace() {
                self.advance_char();
            } else {
                break;
            }
        }
        self.position - start
    }

    /// Check if a character is valid in an identifier
    fn is_identifier_char(c: char) -> bool {
        c.is_alphanumeric() || c == '_' || c == '-' || c == '.' || c == ':'
    }

    /// Check if a character can start an identifier
    fn is_identifier_start(c: char) -> bool {
        c.is_alphabetic() || c == '_'
    }

    /// Scan a quoted string (single quotes)
    fn scan_quoted_string(&mut self) -> Token<'a> {
        let start = self.position;

        // Skip opening quote
        self.advance(1);
        let content_start = self.position;

        // Find closing quote
        while let Some(c) = self.peek() {
            if c == '\'' {
                let content_end = self.position;
                self.advance(1); // Skip closing quote

                // Return the content without quotes
                let content = &self.input[content_start..content_end];
                return Token::quoted(TokenKind::QuotedString, content, start);
            }
            self.advance_char();
        }

        // Unclosed quote - return what we have
        let content = &self.input[content_start..self.position];
        Token::quoted(TokenKind::QuotedString, content, start)
    }

    /// Scan an identifier or keyword
    fn scan_identifier(&mut self) -> Token<'a> {
        let start = self.position;

        // Consume identifier characters
        while let Some(c) = self.peek() {
            if Self::is_identifier_char(c) {
                self.advance_char();
            } else {
                break;
            }
        }

        let text = &self.input[start..self.position];

        // Check if it's a keyword
        let kind = lookup_keyword(text).unwrap_or(TokenKind::Identifier);

        Token::new(kind, text, start)
    }

    /// Scan a number or date-like identifier (for temporal expressions like "5y", "3m", "2024-01-01")
    fn scan_number_or_date(&mut self) -> Token<'a> {
        let start = self.position;

        // Consume digits, hyphens (for dates), dots (for decimals/versions)
        while let Some(c) = self.peek() {
            if c.is_ascii_digit() || c == '-' || c == '.' {
                self.advance_char();
            } else if c.is_alphabetic() {
                // Include suffix like 'y', 'm', 'd' for temporal (only if single letter)
                let next = self.remaining().chars().nth(1);
                if next.is_none_or(|nc| nc.is_whitespace()) {
                    self.advance_char();
                }
                break;
            } else {
                break;
            }
        }

        let text = &self.input[start..self.position];
        Token::new(TokenKind::Identifier, text, start)
    }

    /// Get the next token
    fn next_token(&mut self) -> Option<Token<'a>> {
        // Skip whitespace
        self.skip_whitespace();

        // Check for EOF
        if self.position >= self.input.len() {
            if self.eof_emitted {
                return None;
            }
            self.eof_emitted = true;
            return Some(Token::new(TokenKind::Eof, "", self.position));
        }

        let c = self.peek()?;

        // Dispatch based on first character
        let token = match c {
            // Single-quoted string
            '\'' => self.scan_quoted_string(),

            // Number or date (for temporal expressions like "5y", "2024-01-01")
            '0'..='9' => self.scan_number_or_date(),

            // Identifier or keyword
            _ if Self::is_identifier_start(c) => self.scan_identifier(),

            // Unknown character - consume it
            _ => {
                let start = self.position;
                self.advance_char();
                Token::new(TokenKind::Unknown, &self.input[start..self.position], start)
            }
        };

        Some(token)
    }
}

impl<'a> Iterator for Lexer<'a> {
    type Item = Token<'a>;

    fn next(&mut self) -> Option<Self::Item> {
        self.next_token()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tokenize(input: &str) -> Vec<Token<'_>> {
        Lexer::new(input).collect()
    }

    fn token_kinds(input: &str) -> Vec<TokenKind> {
        Lexer::new(input).map(|t| t.kind).collect()
    }

    fn token_texts(input: &str) -> Vec<&str> {
        Lexer::new(input)
            .filter(|t| t.kind != TokenKind::Eof)
            .map(|t| t.text)
            .collect()
    }

    #[test]
    fn test_empty_input() {
        let tokens = tokenize("");
        assert_eq!(tokens.len(), 1);
        assert_eq!(tokens[0].kind, TokenKind::Eof);
    }

    #[test]
    fn test_whitespace_only() {
        let tokens = tokenize("   \t\n  ");
        assert_eq!(tokens.len(), 1);
        assert_eq!(tokens[0].kind, TokenKind::Eof);
    }

    #[test]
    fn test_single_identifier() {
        let tokens = token_kinds("ALICE");
        assert_eq!(tokens, vec![TokenKind::Identifier, TokenKind::Eof]);
    }

    #[test]
    fn test_multiple_identifiers() {
        let texts = token_texts("ALICE BOB CHARLIE");
        assert_eq!(texts, vec!["ALICE", "BOB", "CHARLIE"]);
    }

    #[test]
    fn test_keywords() {
        let kinds = token_kinds("is are of from by via since until on between over so therefore");
        assert_eq!(
            kinds,
            vec![
                TokenKind::Is,
                TokenKind::Are,
                TokenKind::Of,
                TokenKind::From,
                TokenKind::By,
                TokenKind::Via,
                TokenKind::Since,
                TokenKind::Until,
                TokenKind::On,
                TokenKind::Between,
                TokenKind::Over,
                TokenKind::So,
                TokenKind::Therefore,
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_keywords_case_insensitive() {
        let kinds = token_kinds("IS Are OF FROM");
        assert_eq!(
            kinds,
            vec![
                TokenKind::Is,
                TokenKind::Are,
                TokenKind::Of,
                TokenKind::From,
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_natural_predicates() {
        let kinds = token_kinds("speaks knows works studied");
        assert_eq!(
            kinds,
            vec![
                TokenKind::NaturalPredicate,
                TokenKind::NaturalPredicate,
                TokenKind::NaturalPredicate,
                TokenKind::NaturalPredicate,
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_quoted_string() {
        let tokens = tokenize("'hello world'");
        assert_eq!(tokens.len(), 2); // quoted string + EOF
        assert_eq!(tokens[0].kind, TokenKind::QuotedString);
        assert_eq!(tokens[0].text, "hello world");
        assert!(tokens[0].quoted);
    }

    #[test]
    fn test_quoted_keyword() {
        // Quoted keywords should be treated as quoted strings, not keywords
        let tokens = tokenize("'is'");
        assert_eq!(tokens[0].kind, TokenKind::QuotedString);
        assert_eq!(tokens[0].text, "is");
        assert!(tokens[0].quoted);
    }

    #[test]
    fn test_mixed_query() {
        let kinds = token_kinds("ALICE is author_of GitHub");
        assert_eq!(
            kinds,
            vec![
                TokenKind::Identifier, // ALICE
                TokenKind::Is,         // is
                TokenKind::Identifier, // author_of
                TokenKind::Identifier, // GitHub
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_full_query() {
        let input = "ALICE is author_of of GitHub by BOB since 2024-01-01";
        let tokens = tokenize(input);

        let kinds: Vec<_> = tokens.iter().map(|t| t.kind).collect();
        assert_eq!(
            kinds,
            vec![
                TokenKind::Identifier, // ALICE
                TokenKind::Is,         // is
                TokenKind::Identifier, // author_of
                TokenKind::Of,         // of
                TokenKind::Identifier, // GitHub
                TokenKind::By,         // by
                TokenKind::Identifier, // BOB
                TokenKind::Since,      // since
                TokenKind::Identifier, // 2024-01-01
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_underscore_identifier() {
        let texts = token_texts("is_author_of has_experience_in");
        assert_eq!(texts, vec!["is_author_of", "has_experience_in"]);
    }

    #[test]
    fn test_hyphenated_identifier() {
        let texts = token_texts("pre-processing re-implementation");
        assert_eq!(texts, vec!["pre-processing", "re-implementation"]);
    }

    #[test]
    fn test_temporal_expression() {
        let texts = token_texts("over 5y since 3m");
        assert_eq!(texts, vec!["over", "5y", "since", "3m"]);
    }

    #[test]
    fn test_between_and() {
        let kinds = token_kinds("between 2024-01-01 and 2024-12-31");
        assert_eq!(
            kinds,
            vec![
                TokenKind::Between,
                TokenKind::Identifier, // 2024-01-01
                TokenKind::And,
                TokenKind::Identifier, // 2024-12-31
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_token_offsets() {
        let tokens = tokenize("ALICE is author");
        assert_eq!(tokens[0].offset, 0); // ALICE
        assert_eq!(tokens[1].offset, 6); // is
        assert_eq!(tokens[2].offset, 9); // author
    }

    #[test]
    fn test_unicode_identifiers() {
        let texts = token_texts("日本語 Москва Αθήνα");
        assert_eq!(texts, vec!["日本語", "Москва", "Αθήνα"]);
    }

    #[test]
    fn test_special_characters_in_identifier() {
        // Colon and dot are valid in identifiers (for URLs, namespaces)
        let texts = token_texts("github.com user:alice");
        assert_eq!(texts, vec!["github.com", "user:alice"]);
    }

    #[test]
    fn test_quoted_with_spaces() {
        let tokens = tokenize("'hello world' 'foo bar baz'");
        assert_eq!(tokens[0].text, "hello world");
        assert_eq!(tokens[1].text, "foo bar baz");
    }

    #[test]
    fn test_so_therefore_action() {
        let kinds = token_kinds("so notify therefore send_email");
        assert_eq!(
            kinds,
            vec![
                TokenKind::So,
                TokenKind::Identifier, // notify
                TokenKind::Therefore,
                TokenKind::Identifier, // send_email
                TokenKind::Eof,
            ]
        );
    }
}
