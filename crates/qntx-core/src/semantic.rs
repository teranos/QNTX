//! Semantic token classification for AX queries
//!
//! Mirrors the parser state machine but instead of building an AST,
//! emits per-token semantic classifications with source positions.
//! This is the Rust equivalent of Go's `ats/parser/semantic.go`.
//!
//! Token types match the LSP SemanticTokensLegend order:
//! 0=keyword, 1=variable(subject), 2=function(predicate), 3=namespace(context),
//! 4=class(actor), 5=number(temporal), 6=operator(symbol), 7=string, 8=comment(url), 9=type(unknown)

use serde::Serialize;

use crate::parser::{Lexer, Token, TokenKind};

/// Semantic token types — indices match the LSP legend
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(into = "u32")]
pub enum SemanticTokenType {
    Keyword = 0,
    Subject = 1,
    Predicate = 2,
    Context = 3,
    Actor = 4,
    Temporal = 5,
    Operator = 6,
    String = 7,
    Url = 8,
    Unknown = 9,
}

impl From<SemanticTokenType> for u32 {
    fn from(t: SemanticTokenType) -> u32 {
        t as u32
    }
}

/// A classified token with source position
#[derive(Debug, Clone, Serialize)]
pub struct SemanticToken {
    pub text: String,
    #[serde(rename = "type")]
    pub token_type: SemanticTokenType,
    pub offset: usize,
    pub length: usize,
    pub is_quoted: bool,
}

/// Parser state for classification (mirrors parser::ParserState)
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ClassifyState {
    Subjects,
    Predicates,
    Contexts,
    Actors,
    Temporal,
    Actions,
}

/// Classify all tokens in an AX query string, returning semantic tokens with positions.
///
/// This runs the lexer and tracks parser state transitions to assign each token
/// its grammatical role. Unlike the full parser, this never fails — it classifies
/// whatever tokens it can, making it suitable for real-time editor feedback on
/// incomplete or invalid queries.
pub fn classify_tokens(input: &str) -> Vec<SemanticToken> {
    let lexer = Lexer::new(input);
    let mut state = ClassifyState::Subjects;
    let mut tokens = Vec::new();

    for token in lexer {
        if token.kind == TokenKind::Eof {
            break;
        }

        let (token_type, new_state) = classify_one(&token, state, input);
        state = new_state;

        // For quoted strings, offset points to the opening quote — include the quotes in length
        let (offset, length, is_quoted) = if token.kind == TokenKind::QuotedString {
            // token.offset is the opening quote, token.text is the content without quotes
            // The full span is quote + content + quote
            (token.offset, token.text.len() + 2, true)
        } else {
            (token.offset, token.text.len(), false)
        };

        tokens.push(SemanticToken {
            text: token.text.to_string(),
            token_type,
            offset,
            length,
            is_quoted,
        });
    }

    tokens
}

/// Classify a single token and return the (type, next_state) pair.
fn classify_one(token: &Token<'_>, state: ClassifyState, _input: &str) -> (SemanticTokenType, ClassifyState) {
    // Keywords always classify as Keyword and trigger state transitions
    match token.kind {
        TokenKind::Is | TokenKind::Are => {
            return (SemanticTokenType::Keyword, ClassifyState::Predicates);
        }
        TokenKind::Of | TokenKind::From => {
            return (SemanticTokenType::Keyword, ClassifyState::Contexts);
        }
        TokenKind::By | TokenKind::Via => {
            return (SemanticTokenType::Keyword, ClassifyState::Actors);
        }
        TokenKind::Since | TokenKind::Until | TokenKind::On | TokenKind::Between | TokenKind::Over => {
            return (SemanticTokenType::Keyword, ClassifyState::Temporal);
        }
        TokenKind::And => {
            // 'and' in temporal context (between X and Y) stays temporal
            return (SemanticTokenType::Keyword, state);
        }
        TokenKind::So | TokenKind::Therefore => {
            return (SemanticTokenType::Keyword, ClassifyState::Actions);
        }
        TokenKind::Wildcard | TokenKind::Pipe | TokenKind::Unknown => {
            return (SemanticTokenType::Unknown, state);
        }
        _ => {}
    }

    // Check for Unicode symbols (single non-ASCII non-alphanumeric codepoint like ⋈, ∈, ⌬)
    if token.kind == TokenKind::Identifier {
        let mut chars = token.text.chars();
        if let Some(first) = chars.next() {
            if chars.next().is_none() && !first.is_ascii() && !first.is_alphanumeric() {
                return (SemanticTokenType::Operator, state);
            }
        }
    }

    // Quoted strings are always String type
    if token.kind == TokenKind::QuotedString {
        return (SemanticTokenType::String, state);
    }

    // Classify by current parser state
    let token_type = match state {
        ClassifyState::Subjects => SemanticTokenType::Subject,
        ClassifyState::Predicates => SemanticTokenType::Predicate,
        ClassifyState::Contexts => SemanticTokenType::Context,
        ClassifyState::Actors => SemanticTokenType::Actor,
        ClassifyState::Temporal => SemanticTokenType::Temporal,
        ClassifyState::Actions => SemanticTokenType::Unknown, // actions don't have a dedicated LSP type
    };

    (token_type, state)
}

/// Encode semantic tokens into LSP delta format: array of 5-tuples
/// (deltaLine, deltaStart, length, tokenType, tokenModifiers).
///
/// For single-line queries (which AX queries always are), deltaLine is always 0
/// and deltaStart is the character offset delta from the previous token.
pub fn encode_lsp_tokens(tokens: &[SemanticToken]) -> Vec<u32> {
    let mut data = Vec::with_capacity(tokens.len() * 5);
    let mut prev_offset: u32 = 0;

    for token in tokens {
        let offset = token.offset as u32;
        let delta_start = offset - prev_offset;

        data.push(0); // deltaLine (always 0 for single-line queries)
        data.push(delta_start);
        data.push(token.length as u32);
        data.push(token.token_type as u32);
        data.push(0); // modifiers

        prev_offset = offset;
    }

    data
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_classify_subjects_only() {
        let tokens = classify_tokens("ALICE BOB");
        assert_eq!(tokens.len(), 2);
        assert_eq!(tokens[0].text, "ALICE");
        assert_eq!(tokens[0].token_type, SemanticTokenType::Subject);
        assert_eq!(tokens[0].offset, 0);
        assert_eq!(tokens[0].length, 5);
        assert_eq!(tokens[1].text, "BOB");
        assert_eq!(tokens[1].token_type, SemanticTokenType::Subject);
        assert_eq!(tokens[1].offset, 6);
    }

    #[test]
    fn test_classify_full_query() {
        let tokens = classify_tokens("ALICE is author of GitHub by CHARLIE since 2024-01-01");
        let types: Vec<_> = tokens.iter().map(|t| t.token_type).collect();
        assert_eq!(types, vec![
            SemanticTokenType::Subject,   // ALICE
            SemanticTokenType::Keyword,   // is
            SemanticTokenType::Predicate, // author
            SemanticTokenType::Keyword,   // of
            SemanticTokenType::Context,   // GitHub
            SemanticTokenType::Keyword,   // by
            SemanticTokenType::Actor,     // CHARLIE
            SemanticTokenType::Keyword,   // since
            SemanticTokenType::Temporal,  // 2024-01-01
        ]);
    }

    #[test]
    fn test_classify_quoted_string() {
        let tokens = classify_tokens("'John Doe' is 'senior dev'");
        assert_eq!(tokens[0].text, "John Doe");
        assert_eq!(tokens[0].token_type, SemanticTokenType::String);
        assert_eq!(tokens[0].is_quoted, true);
        assert_eq!(tokens[0].offset, 0);
        assert_eq!(tokens[0].length, 10); // 'John Doe' = 10 chars
        assert_eq!(tokens[1].token_type, SemanticTokenType::Keyword); // is
        assert_eq!(tokens[2].text, "senior dev");
        assert_eq!(tokens[2].token_type, SemanticTokenType::String);
    }

    #[test]
    fn test_classify_temporal_between() {
        let tokens = classify_tokens("ALICE between 2024-01-01 and 2024-12-31");
        let types: Vec<_> = tokens.iter().map(|t| t.token_type).collect();
        assert_eq!(types, vec![
            SemanticTokenType::Subject,  // ALICE
            SemanticTokenType::Keyword,  // between
            SemanticTokenType::Temporal, // 2024-01-01
            SemanticTokenType::Keyword,  // and
            SemanticTokenType::Temporal, // 2024-12-31
        ]);
    }

    #[test]
    fn test_classify_empty_input() {
        let tokens = classify_tokens("");
        assert!(tokens.is_empty());
    }

    #[test]
    fn test_classify_whitespace_only() {
        let tokens = classify_tokens("   ");
        assert!(tokens.is_empty());
    }

    #[test]
    fn test_classify_url_prefix() {
        // The lexer splits URLs at '/' — URL detection works for the "https:" token
        let tokens = classify_tokens("ALICE is author of https://github.com");
        // "https:" is classified as Context (state is contexts after "of")
        // since the lexer can't keep URLs together. Full URL detection
        // requires lexer-level changes (future enhancement).
        assert_eq!(tokens[4].text, "https:");
        assert_eq!(tokens[4].token_type, SemanticTokenType::Context);
    }

    #[test]
    fn test_classify_unknown_chars() {
        let tokens = classify_tokens("ALICE * BOB");
        assert_eq!(tokens[1].token_type, SemanticTokenType::Unknown);
    }

    #[test]
    fn test_encode_lsp_tokens() {
        let tokens = classify_tokens("ALICE is author");
        let encoded = encode_lsp_tokens(&tokens);
        // 3 tokens × 5 = 15 values
        assert_eq!(encoded.len(), 15);
        // First token: delta_line=0, delta_start=0, length=5, type=1(subject), mod=0
        assert_eq!(&encoded[0..5], &[0, 0, 5, 1, 0]);
        // Second token: delta_line=0, delta_start=6, length=2, type=0(keyword), mod=0
        assert_eq!(&encoded[5..10], &[0, 6, 2, 0, 0]);
        // Third token: delta_line=0, delta_start=3, length=6, type=2(predicate), mod=0
        assert_eq!(&encoded[10..15], &[0, 3, 6, 2, 0]);
    }

    #[test]
    fn test_offsets_are_correct() {
        let input = "ALICE is author of GitHub";
        let tokens = classify_tokens(input);
        for token in &tokens {
            // Verify each token's offset points to the right place in the input
            if !token.is_quoted {
                assert_eq!(&input[token.offset..token.offset + token.length], token.text);
            }
        }
    }
}
