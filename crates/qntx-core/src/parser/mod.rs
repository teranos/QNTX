//! AX Query Parser
//!
//! Zero-copy parser for the QNTX AX query language.
//!
//! # Grammar
//!
//! ```text
//! query ::= [subjects] [predicate_clause] [context_clause] [actor_clause] [temporal_clause] [action_clause]
//!
//! subjects         ::= IDENTIFIER+
//! predicate_clause ::= ("is" | "are") predicates
//! context_clause   ::= ("of" | "from") contexts
//! actor_clause     ::= ("by" | "via") actors
//! temporal_clause  ::= temporal_keyword temporal_expr
//! action_clause    ::= ("so" | "therefore") actions
//! ```
//!
//! # Example
//!
//! ```rust
//! use qntx_core::parser::Parser;
//!
//! let query = Parser::parse("ALICE is author_of of GitHub since 2024-01-01").unwrap();
//! assert_eq!(query.subjects, vec!["ALICE"]);
//! assert_eq!(query.predicates, vec!["author_of"]);
//! assert_eq!(query.contexts, vec!["GitHub"]);
//! ```

mod ast;
mod lexer;
mod token;

pub use ast::{AxQuery, DurationExpr, DurationUnit, TemporalClause};
pub use lexer::Lexer;
use token::{Token, TokenKind};

use thiserror::Error;

/// Parser errors
#[derive(Debug, Error, Clone, PartialEq)]
pub enum ParseError {
    #[error("expected {expected} at position {position}, found {found}")]
    UnexpectedToken {
        expected: String,
        found: String,
        position: usize,
    },

    #[error("missing {element} after '{keyword}' at position {position}")]
    MissingElement {
        keyword: String,
        element: String,
        position: usize,
    },

    #[error("missing 'and' in 'between' clause at position {position}")]
    MissingAnd { position: usize },

    #[error("empty query")]
    EmptyQuery,

    #[error("wildcard '*' is not supported in ax queries - use specific {field} names")]
    WildcardNotSupported { field: String },
}

/// Parser state machine states
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ParserState {
    Start,
    Subjects,
    Predicates,
    Contexts,
    Actors,
    Temporal,
    Actions,
    Done,
}

/// AX query parser using a state machine
pub struct Parser<'a> {
    lexer: std::iter::Peekable<Lexer<'a>>,
    state: ParserState,
    query: AxQuery<'a>,
    current_position: usize,
}

impl<'a> Parser<'a> {
    /// Create a new parser from input string
    pub fn new(input: &'a str) -> Self {
        Self {
            lexer: Lexer::new(input).peekable(),
            state: ParserState::Start,
            query: AxQuery::new(),
            current_position: 0,
        }
    }

    /// Parse input string directly
    pub fn parse(input: &'a str) -> Result<AxQuery<'a>, ParseError> {
        Parser::new(input).run()
    }

    /// Run the parser
    pub fn run(mut self) -> Result<AxQuery<'a>, ParseError> {
        while self.state != ParserState::Done {
            self.step()?;
        }
        self.validate()?;
        Ok(self.query)
    }

    /// Validate the parsed query
    fn validate(&self) -> Result<(), ParseError> {
        // Block wildcard '*' - not part of ax specification
        for subject in &self.query.subjects {
            if *subject == "*" {
                return Err(ParseError::WildcardNotSupported {
                    field: "subject".to_string(),
                });
            }
        }
        for predicate in &self.query.predicates {
            if *predicate == "*" {
                return Err(ParseError::WildcardNotSupported {
                    field: "predicate".to_string(),
                });
            }
        }
        for context in &self.query.contexts {
            if *context == "*" {
                return Err(ParseError::WildcardNotSupported {
                    field: "context".to_string(),
                });
            }
        }
        for actor in &self.query.actors {
            if *actor == "*" {
                return Err(ParseError::WildcardNotSupported {
                    field: "actor".to_string(),
                });
            }
        }
        Ok(())
    }

    fn peek(&mut self) -> Option<&Token<'a>> {
        self.lexer.peek()
    }

    fn next(&mut self) -> Option<Token<'a>> {
        let token = self.lexer.next();
        if let Some(ref t) = token {
            self.current_position = t.offset;
        }
        token
    }

    fn at_eof(&mut self) -> bool {
        self.peek()
            .map(|t| t.kind == TokenKind::Eof)
            .unwrap_or(true)
    }

    fn step(&mut self) -> Result<(), ParseError> {
        match self.state {
            ParserState::Start => self.parse_start(),
            ParserState::Subjects => self.parse_subjects(),
            ParserState::Predicates => self.parse_predicates(),
            ParserState::Contexts => self.parse_contexts(),
            ParserState::Actors => self.parse_actors(),
            ParserState::Temporal => self.parse_temporal(),
            ParserState::Actions => self.parse_actions(),
            ParserState::Done => Ok(()),
        }
    }

    fn parse_start(&mut self) -> Result<(), ParseError> {
        if self.at_eof() {
            self.state = ParserState::Done;
            return Ok(());
        }

        let token = match self.peek() {
            Some(t) => t,
            None => {
                self.state = ParserState::Done;
                return Ok(());
            }
        };

        match token.kind {
            TokenKind::Eof => self.state = ParserState::Done,
            TokenKind::Is | TokenKind::Are => self.state = ParserState::Predicates,
            TokenKind::Of | TokenKind::From => self.state = ParserState::Contexts,
            TokenKind::By | TokenKind::Via => self.state = ParserState::Actors,
            TokenKind::Since
            | TokenKind::Until
            | TokenKind::On
            | TokenKind::Between
            | TokenKind::Over => self.state = ParserState::Temporal,
            TokenKind::So | TokenKind::Therefore => self.state = ParserState::Actions,
            // NOTE: User dissatisfaction - this is terrible design. We're routing tokens to
            // different states based on fragile string patterns. A bare word like "has_experience"
            // becomes a predicate just because it has an underscore, while "ALICE" is a subject.
            // A proper parser would have explicit syntax or use actual NLP, not these hacks.
            TokenKind::NaturalPredicate => {
                // Bare natural predicates (has_experience, is_member) go directly to predicates
                let token = self.next().unwrap();
                self.query.predicates.push(token.text);
                self.state = ParserState::Start;
            }
            TokenKind::Identifier | TokenKind::QuotedString => self.state = ParserState::Subjects,
            TokenKind::Unknown | TokenKind::And => {
                self.next();
            }
        }
        Ok(())
    }

    fn parse_subjects(&mut self) -> Result<(), ParseError> {
        loop {
            if self.at_eof() {
                self.state = ParserState::Done;
                return Ok(());
            }

            let token = match self.peek() {
                Some(t) => t,
                None => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            };

            match token.kind {
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.subjects.push(t.text);
                }
                TokenKind::Is | TokenKind::Are => {
                    self.state = ParserState::Predicates;
                    return Ok(());
                }
                TokenKind::Of | TokenKind::From => {
                    self.state = ParserState::Contexts;
                    return Ok(());
                }
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                TokenKind::Unknown | TokenKind::And => {
                    self.next();
                }
            }
        }
    }

    fn parse_predicates(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("is");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        let mut found = false;
        loop {
            if self.at_eof() {
                if !found {
                    return Err(ParseError::MissingElement {
                        keyword: keyword.to_string(),
                        element: "predicate".to_string(),
                        position: keyword_pos,
                    });
                }
                self.state = ParserState::Done;
                return Ok(());
            }

            let token = match self.peek() {
                Some(t) => t,
                None => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            };

            match token.kind {
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.predicates.push(t.text);
                    found = true;
                }
                TokenKind::Of | TokenKind::From => {
                    self.state = ParserState::Contexts;
                    return Ok(());
                }
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                TokenKind::Is | TokenKind::Are => {
                    self.state = ParserState::Predicates;
                    return Ok(());
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                TokenKind::Unknown | TokenKind::And => {
                    self.next();
                }
            }
        }
    }

    fn parse_contexts(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("of");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        let mut found = false;
        loop {
            if self.at_eof() {
                if !found {
                    return Err(ParseError::MissingElement {
                        keyword: keyword.to_string(),
                        element: "context".to_string(),
                        position: keyword_pos,
                    });
                }
                self.state = ParserState::Done;
                return Ok(());
            }

            let token = match self.peek() {
                Some(t) => t,
                None => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            };

            match token.kind {
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.contexts.push(t.text);
                    found = true;
                }
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                TokenKind::Of | TokenKind::From => {
                    self.next();
                }
                TokenKind::Is | TokenKind::Are => {
                    self.state = ParserState::Predicates;
                    return Ok(());
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                TokenKind::Unknown | TokenKind::And => {
                    self.next();
                }
            }
        }
    }

    fn parse_actors(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("by");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        let mut found = false;
        loop {
            if self.at_eof() {
                if !found {
                    return Err(ParseError::MissingElement {
                        keyword: keyword.to_string(),
                        element: "actor".to_string(),
                        position: keyword_pos,
                    });
                }
                self.state = ParserState::Done;
                return Ok(());
            }

            let token = match self.peek() {
                Some(t) => t,
                None => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            };

            match token.kind {
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.actors.push(t.text);
                    found = true;
                }
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                TokenKind::By | TokenKind::Via => {
                    self.next();
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                TokenKind::Is | TokenKind::Are => {
                    self.state = ParserState::Predicates;
                    return Ok(());
                }
                TokenKind::Of | TokenKind::From => {
                    self.state = ParserState::Contexts;
                    return Ok(());
                }
                TokenKind::Unknown | TokenKind::And => {
                    self.next();
                }
            }
        }
    }

    fn parse_temporal(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.kind);
        let keyword_text = keyword_token.as_ref().map(|t| t.text).unwrap_or("");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        let expr = self.collect_temporal_expr()?;

        if expr.is_empty() {
            return Err(ParseError::MissingElement {
                keyword: keyword_text.to_string(),
                element: "date or duration".to_string(),
                position: keyword_pos,
            });
        }

        let temporal = match keyword {
            Some(TokenKind::Since) => TemporalClause::Since(expr),
            Some(TokenKind::Until) => TemporalClause::Until(expr),
            Some(TokenKind::On) => TemporalClause::On(expr),
            Some(TokenKind::Over) => TemporalClause::Over(DurationExpr::parse(expr)),
            Some(TokenKind::Between) => {
                let end_expr = self.collect_between_end(keyword_pos)?;
                TemporalClause::Between(expr, end_expr)
            }
            _ => TemporalClause::Since(expr),
        };

        self.query.temporal = Some(temporal);
        self.transition_after_temporal()
    }

    fn collect_temporal_expr(&mut self) -> Result<&'a str, ParseError> {
        if self.at_eof() {
            return Ok("");
        }

        let token = match self.peek() {
            Some(t) => t,
            None => return Ok(""),
        };

        match token.kind {
            TokenKind::Identifier | TokenKind::QuotedString => {
                let t = self.next().unwrap();
                Ok(t.text)
            }
            _ => Ok(""),
        }
    }

    fn collect_between_end(&mut self, keyword_pos: usize) -> Result<&'a str, ParseError> {
        if self.at_eof() {
            return Err(ParseError::MissingAnd {
                position: keyword_pos,
            });
        }

        let and_token = self.peek();
        if and_token.map(|t| t.kind) != Some(TokenKind::And) {
            return Err(ParseError::MissingAnd {
                position: self.current_position,
            });
        }
        self.next();

        self.collect_temporal_expr()
    }

    fn transition_after_temporal(&mut self) -> Result<(), ParseError> {
        if self.at_eof() {
            self.state = ParserState::Done;
            return Ok(());
        }

        let token = match self.peek() {
            Some(t) => t,
            None => {
                self.state = ParserState::Done;
                return Ok(());
            }
        };

        match token.kind {
            TokenKind::So | TokenKind::Therefore => self.state = ParserState::Actions,
            TokenKind::Eof => self.state = ParserState::Done,
            TokenKind::Since
            | TokenKind::Until
            | TokenKind::On
            | TokenKind::Between
            | TokenKind::Over => self.state = ParserState::Temporal,
            _ => self.state = ParserState::Done,
        }

        Ok(())
    }

    fn parse_actions(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("so");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        let mut found = false;
        loop {
            if self.at_eof() {
                if !found {
                    return Err(ParseError::MissingElement {
                        keyword: keyword.to_string(),
                        element: "action".to_string(),
                        position: keyword_pos,
                    });
                }
                self.state = ParserState::Done;
                return Ok(());
            }

            let token = match self.peek() {
                Some(t) => t,
                None => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            };

            match token.kind {
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.actions.push(t.text);
                    found = true;
                }
                TokenKind::So | TokenKind::Therefore => {
                    self.next();
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                _ => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_empty_query() {
        let query = Parser::parse("").unwrap();
        assert!(query.is_empty());
    }

    #[test]
    fn test_subjects_only() {
        let query = Parser::parse("ALICE BOB CHARLIE").unwrap();
        assert_eq!(query.subjects, vec!["ALICE", "BOB", "CHARLIE"]);
    }

    #[test]
    fn test_subject_with_predicate() {
        let query = Parser::parse("ALICE is author").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author"]);
    }

    #[test]
    fn test_full_query() {
        let query = Parser::parse(
            "ALICE BOB is author_of of GitHub Linux by CHARLIE since 2024-01-01 so notify",
        )
        .unwrap();
        assert_eq!(query.subjects, vec!["ALICE", "BOB"]);
        assert_eq!(query.predicates, vec!["author_of"]);
        assert_eq!(query.contexts, vec!["GitHub", "Linux"]);
        assert_eq!(query.actors, vec!["CHARLIE"]);
        assert_eq!(query.temporal, Some(TemporalClause::Since("2024-01-01")));
        assert_eq!(query.actions, vec!["notify"]);
    }

    #[test]
    fn test_temporal_between() {
        let query = Parser::parse("ALICE is author between 2024-01-01 and 2024-12-31").unwrap();
        assert_eq!(
            query.temporal,
            Some(TemporalClause::Between("2024-01-01", "2024-12-31"))
        );
    }

    #[test]
    fn test_temporal_over() {
        let query = Parser::parse("ALICE is experienced over 5y").unwrap();
        if let Some(TemporalClause::Over(dur)) = query.temporal {
            assert_eq!(dur.value, Some(5.0));
            assert_eq!(dur.unit, Some(DurationUnit::Years));
        } else {
            panic!("Expected Over clause");
        }
    }

    #[test]
    fn test_quoted_strings() {
        let query = Parser::parse("'John Doe' is 'senior developer' of 'ACME Corp'").unwrap();
        assert_eq!(query.subjects, vec!["John Doe"]);
        assert_eq!(query.predicates, vec!["senior developer"]);
        assert_eq!(query.contexts, vec!["ACME Corp"]);
    }
}
