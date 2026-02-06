//! Zero-copy lexer for AX query language

use super::token::{Token, TokenKind};

/// Keywords lookup (case-insensitive)
fn keyword_kind(s: &str) -> Option<TokenKind> {
    match s.to_ascii_lowercase().as_str() {
        "is" => Some(TokenKind::Is),
        "are" => Some(TokenKind::Are),
        "of" => Some(TokenKind::Of),
        "from" => Some(TokenKind::From),
        "by" => Some(TokenKind::By),
        "via" => Some(TokenKind::Via),
        "since" => Some(TokenKind::Since),
        "until" => Some(TokenKind::Until),
        "on" => Some(TokenKind::On),
        "between" => Some(TokenKind::Between),
        "over" => Some(TokenKind::Over),
        "and" => Some(TokenKind::And),
        "so" => Some(TokenKind::So),
        "therefore" => Some(TokenKind::Therefore),
        _ => None,
    }
}

/// Zero-copy lexer for AX queries
pub struct Lexer<'a> {
    input: &'a str,
    position: usize,
    done: bool,
}

impl<'a> Lexer<'a> {
    pub fn new(input: &'a str) -> Self {
        Self {
            input,
            position: 0,
            done: false,
        }
    }

    fn remaining(&self) -> &'a str {
        &self.input[self.position..]
    }

    fn peek_char(&self) -> Option<char> {
        self.remaining().chars().next()
    }

    fn advance(&mut self, n: usize) {
        self.position += n;
    }

    fn skip_whitespace(&mut self) {
        while let Some(c) = self.peek_char() {
            if c.is_whitespace() {
                self.advance(c.len_utf8());
            } else {
                break;
            }
        }
    }

    fn read_quoted_string(&mut self) -> Token<'a> {
        let start = self.position;
        let quote_char = self.peek_char().unwrap();
        self.advance(quote_char.len_utf8());

        let content_start = self.position;
        let mut content_end = content_start;

        while let Some(c) = self.peek_char() {
            if c == quote_char {
                content_end = self.position;
                self.advance(c.len_utf8());
                break;
            }
            self.advance(c.len_utf8());
            content_end = self.position;
        }

        let text = &self.input[content_start..content_end];
        Token::new(TokenKind::QuotedString, text, start)
    }

    fn read_identifier(&mut self) -> Token<'a> {
        let start = self.position;

        while let Some(c) = self.peek_char() {
            if c.is_alphanumeric() || c == '_' || c == '-' || c == '.' || c == ':' || c == '@' {
                self.advance(c.len_utf8());
            } else if !c.is_ascii() && !c.is_whitespace() {
                // Allow non-ASCII characters (Unicode identifiers)
                self.advance(c.len_utf8());
            } else {
                break;
            }
        }

        let text = &self.input[start..self.position];

        // Check if it's a keyword
        if let Some(kind) = keyword_kind(text) {
            return Token::new(kind, text, start);
        }

        // TODO: Natural predicate detection is not very sophisticated. Currently it just
        // checks for underscores or common prefixes. A more advanced implementation could:
        // - Use NLP to detect verb phrases vs nouns
        // - Support camelCase predicates (isAuthorOf)
        // - Handle multi-word predicates without underscores
        // - Detect semantic patterns like "X of Y" relationships
        //
        // NOTE: The user correctly points out this is inelegant. We're matching Go's arbitrary
        // heuristics that treat "has_experience" as a predicate just because it contains an
        // underscore. This is fragile and unprincipled. A proper redesign would have explicit
        // predicate markers or use actual linguistic analysis, not string pattern matching.
        // But for now we need bug-for-bug compatibility with the Go parser's quirks.
        let kind = if text.contains('_') || text.starts_with("has") || text.starts_with("is") {
            TokenKind::NaturalPredicate
        } else {
            TokenKind::Identifier
        };

        Token::new(kind, text, start)
    }

    fn next_token(&mut self) -> Token<'a> {
        self.skip_whitespace();

        if self.position >= self.input.len() {
            self.done = true;
            return Token::eof(self.position);
        }

        let c = self.peek_char().unwrap();

        match c {
            '\'' | '"' => self.read_quoted_string(),
            '*' | '^' | '%' | '$' | '#' => {
                // Wildcards/special characters are not supported in ax queries
                let start = self.position;
                self.advance(c.len_utf8());
                Token::new(
                    TokenKind::Wildcard,
                    &self.input[start..self.position],
                    start,
                )
            }
            _ if c.is_alphanumeric() || c == '_' || !c.is_ascii() => self.read_identifier(),
            _ => {
                // Unknown character - skip it
                let start = self.position;
                self.advance(c.len_utf8());
                Token::new(TokenKind::Unknown, &self.input[start..self.position], start)
            }
        }
    }
}

impl<'a> Iterator for Lexer<'a> {
    type Item = Token<'a>;

    fn next(&mut self) -> Option<Self::Item> {
        if self.done {
            return None;
        }

        let token = self.next_token();
        if token.kind == TokenKind::Eof {
            self.done = true;
        }
        Some(token)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn collect_tokens(input: &str) -> Vec<Token<'_>> {
        Lexer::new(input).collect()
    }

    #[test]
    fn test_empty_input() {
        let tokens = collect_tokens("");
        assert_eq!(tokens.len(), 1);
        assert_eq!(tokens[0].kind, TokenKind::Eof);
    }

    #[test]
    fn test_single_identifier() {
        let tokens = collect_tokens("ALICE");
        assert_eq!(tokens.len(), 2);
        assert_eq!(tokens[0].kind, TokenKind::Identifier);
        assert_eq!(tokens[0].text, "ALICE");
    }

    #[test]
    fn test_keywords() {
        let tokens =
            collect_tokens("is are of from by via since until on between over and so therefore");
        let kinds: Vec<_> = tokens.iter().map(|t| t.kind).collect();
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
                TokenKind::And,
                TokenKind::So,
                TokenKind::Therefore,
                TokenKind::Eof,
            ]
        );
    }

    #[test]
    fn test_quoted_string() {
        let tokens = collect_tokens("'hello world'");
        assert_eq!(tokens[0].kind, TokenKind::QuotedString);
        assert_eq!(tokens[0].text, "hello world");
    }

    #[test]
    fn test_natural_predicate() {
        let tokens = collect_tokens("is_author_of");
        assert_eq!(tokens[0].kind, TokenKind::NaturalPredicate);
        assert_eq!(tokens[0].text, "is_author_of");
    }

    #[test]
    fn test_unicode() {
        let tokens = collect_tokens("日本語 Москва");
        assert_eq!(tokens[0].kind, TokenKind::Identifier);
        assert_eq!(tokens[0].text, "日本語");
        assert_eq!(tokens[1].kind, TokenKind::Identifier);
        assert_eq!(tokens[1].text, "Москва");
    }

    #[test]
    fn test_full_query() {
        let tokens = collect_tokens("ALICE is author_of of GitHub since 2024-01-01");
        assert_eq!(tokens[0].text, "ALICE");
        assert_eq!(tokens[1].kind, TokenKind::Is);
        assert_eq!(tokens[2].text, "author_of");
        assert_eq!(tokens[3].kind, TokenKind::Of);
        assert_eq!(tokens[4].text, "GitHub");
        assert_eq!(tokens[5].kind, TokenKind::Since);
        assert_eq!(tokens[6].text, "2024-01-01");
    }
}
