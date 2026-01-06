//! State-machine parser for AX query language
//!
//! Converts a token stream from the lexer into an AST representation.
//!
//! # Grammar
//!
//! ```text
//! ax_query ::= [subjects] [predicate_clause] [context_clause] [actor_clause] [temporal_clause] [so_clause]
//!
//! subjects         ::= IDENTIFIER+
//! predicate_clause ::= ("is" | "are") predicates
//! context_clause   ::= ("of" | "from") contexts
//! actor_clause     ::= ("by" | "via") actors
//! temporal_clause  ::= temporal_keyword temporal_expr
//! so_clause        ::= ("so" | "therefore") actions
//!
//! temporal_keyword ::= "since" | "until" | "on" | "between" | "over"
//! ```
//!
//! # Example
//!
//! ```rust
//! use qntx_parser::ax::{Lexer, Parser};
//!
//! let input = "ALICE is author_of of GitHub";
//! let lexer = Lexer::new(input);
//! let query = Parser::new(lexer).parse().unwrap();
//!
//! assert_eq!(query.subjects, vec!["ALICE"]);
//! assert_eq!(query.predicates, vec!["author_of"]);
//! assert_eq!(query.contexts, vec!["GitHub"]);
//! ```

use super::ast::{AxQuery, DurationExpr, TemporalClause};
use super::lexer::Lexer;
use super::token::{Token, TokenKind};
use thiserror::Error;

/// Parser errors
#[derive(Debug, Error, Clone, PartialEq)]
pub enum ParseError {
    /// Expected a specific token kind
    #[error("expected {expected} at position {position}, found {found}")]
    UnexpectedToken {
        expected: String,
        found: String,
        position: usize,
    },

    /// Missing required element after keyword
    #[error("missing {element} after '{keyword}' at position {position}")]
    MissingElement {
        keyword: String,
        element: String,
        position: usize,
    },

    /// Missing 'and' in between clause
    #[error("missing 'and' in 'between' clause at position {position}")]
    MissingAnd { position: usize },

    /// Empty query
    #[error("empty query")]
    EmptyQuery,
}

/// Parser state machine states
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ParserState {
    /// Initial state - expecting subjects or clause keyword
    Start,
    /// Parsing subjects (identifiers before any keyword)
    Subjects,
    /// After is/are - expecting predicates
    Predicates,
    /// After of/from - expecting contexts
    Contexts,
    /// After by/via - expecting actors
    Actors,
    /// After temporal keyword - expecting temporal expression
    Temporal,
    /// After so/therefore - expecting actions
    Actions,
    /// Parsing complete
    Done,
}

/// A parser for AX query language
///
/// Uses a state machine to track which clause is being parsed.
pub struct Parser<'a> {
    /// The lexer providing tokens
    lexer: std::iter::Peekable<Lexer<'a>>,
    /// Current parser state
    state: ParserState,
    /// The query being built
    query: AxQuery<'a>,
    /// Current token for error reporting
    current_position: usize,
}

impl<'a> Parser<'a> {
    /// Create a new parser from a lexer
    pub fn new(lexer: Lexer<'a>) -> Self {
        Self {
            lexer: lexer.peekable(),
            state: ParserState::Start,
            query: AxQuery::new(),
            current_position: 0,
        }
    }

    /// Parse from a string directly
    pub fn parse_str(input: &'a str) -> Result<AxQuery<'a>, ParseError> {
        Parser::new(Lexer::new(input)).parse()
    }

    /// Parse the token stream into an AxQuery
    pub fn parse(mut self) -> Result<AxQuery<'a>, ParseError> {
        while self.state != ParserState::Done {
            self.step()?;
        }
        Ok(self.query)
    }

    /// Peek at the next token without consuming it
    fn peek(&mut self) -> Option<&Token<'a>> {
        self.lexer.peek()
    }

    /// Consume and return the next token
    fn next(&mut self) -> Option<Token<'a>> {
        let token = self.lexer.next();
        if let Some(ref t) = token {
            self.current_position = t.offset;
        }
        token
    }

    /// Check if we've reached EOF
    fn at_eof(&mut self) -> bool {
        self.peek()
            .map(|t| t.kind == TokenKind::Eof)
            .unwrap_or(true)
    }

    /// Perform one step of the state machine
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

    /// Initial state - determine what we're starting with
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

        // Determine initial state based on first token
        match token.kind {
            TokenKind::Eof => {
                self.state = ParserState::Done;
            }
            TokenKind::Is | TokenKind::Are => {
                self.state = ParserState::Predicates;
            }
            TokenKind::Of | TokenKind::From => {
                self.state = ParserState::Contexts;
            }
            TokenKind::By | TokenKind::Via => {
                self.state = ParserState::Actors;
            }
            TokenKind::Since
            | TokenKind::Until
            | TokenKind::On
            | TokenKind::Between
            | TokenKind::Over => {
                self.state = ParserState::Temporal;
            }
            TokenKind::So | TokenKind::Therefore => {
                self.state = ParserState::Actions;
            }
            // Any identifier starts subjects
            TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                self.state = ParserState::Subjects;
            }
            TokenKind::Unknown | TokenKind::And => {
                // Skip unknown tokens
                self.next();
            }
        }

        Ok(())
    }

    /// Parse subject identifiers
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
                // Continue collecting subjects
                TokenKind::Identifier | TokenKind::QuotedString => {
                    let t = self.next().unwrap();
                    self.query.subjects.push(t.text);
                }
                // Natural predicates in subject position are treated as subjects
                TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.subjects.push(t.text);
                }
                // Transition to predicate clause
                TokenKind::Is | TokenKind::Are => {
                    self.state = ParserState::Predicates;
                    return Ok(());
                }
                // Transition to context clause
                TokenKind::Of | TokenKind::From => {
                    self.state = ParserState::Contexts;
                    return Ok(());
                }
                // Transition to actor clause
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                // Transition to temporal clause
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                // Transition to action clause
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                TokenKind::Unknown | TokenKind::And => {
                    // Skip unknown tokens
                    self.next();
                }
            }
        }
    }

    /// Parse predicates after is/are
    fn parse_predicates(&mut self) -> Result<(), ParseError> {
        // Consume the is/are keyword
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("is");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        // Collect predicates until we hit another clause keyword
        let mut found_predicate = false;
        loop {
            if self.at_eof() {
                if !found_predicate {
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
                // Collect predicates
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.predicates.push(t.text);
                    found_predicate = true;
                }
                // Transition to context clause
                TokenKind::Of | TokenKind::From => {
                    self.state = ParserState::Contexts;
                    return Ok(());
                }
                // Transition to actor clause
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                // Transition to temporal clause
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                // Transition to action clause
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                // Another is/are - error or continue
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

    /// Parse contexts after of/from
    fn parse_contexts(&mut self) -> Result<(), ParseError> {
        // Consume the of/from keyword
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("of");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        // Collect contexts
        let mut found_context = false;
        loop {
            if self.at_eof() {
                if !found_context {
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
                // Collect contexts
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.contexts.push(t.text);
                    found_context = true;
                }
                // Transition to actor clause
                TokenKind::By | TokenKind::Via => {
                    self.state = ParserState::Actors;
                    return Ok(());
                }
                // Transition to temporal clause
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                // Transition to action clause
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                // Another of/from - continue collecting
                TokenKind::Of | TokenKind::From => {
                    self.next();
                }
                // is/are in context position - likely an error but be lenient
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

    /// Parse actors after by/via
    fn parse_actors(&mut self) -> Result<(), ParseError> {
        // Consume the by/via keyword
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("by");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        // Collect actors
        let mut found_actor = false;
        loop {
            if self.at_eof() {
                if !found_actor {
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
                // Collect actors
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.actors.push(t.text);
                    found_actor = true;
                }
                // Transition to temporal clause
                TokenKind::Since
                | TokenKind::Until
                | TokenKind::On
                | TokenKind::Between
                | TokenKind::Over => {
                    self.state = ParserState::Temporal;
                    return Ok(());
                }
                // Transition to action clause
                TokenKind::So | TokenKind::Therefore => {
                    self.state = ParserState::Actions;
                    return Ok(());
                }
                // Another by/via - continue collecting
                TokenKind::By | TokenKind::Via => {
                    self.next();
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                // Other keywords - unusual but handle gracefully
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

    /// Parse temporal clause
    fn parse_temporal(&mut self) -> Result<(), ParseError> {
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.kind);
        let keyword_text = keyword_token.as_ref().map(|t| t.text).unwrap_or("");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        // Get the temporal expression
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
                // Need to find 'and' and second date
                let end_expr = self.collect_between_end(keyword_pos)?;
                TemporalClause::Between(expr, end_expr)
            }
            _ => TemporalClause::Since(expr), // Fallback
        };

        self.query.temporal = Some(temporal);

        // Check what comes next
        self.transition_after_temporal()
    }

    /// Collect temporal expression (date or duration)
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

    /// Collect the end expression for 'between X and Y'
    fn collect_between_end(&mut self, keyword_pos: usize) -> Result<&'a str, ParseError> {
        // Expect 'and' keyword
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
        self.next(); // consume 'and'

        // Get the end date
        self.collect_temporal_expr()
    }

    /// Transition to next state after temporal clause
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
            TokenKind::So | TokenKind::Therefore => {
                self.state = ParserState::Actions;
            }
            TokenKind::Eof => {
                self.state = ParserState::Done;
            }
            // Other temporal keywords - additional constraint
            TokenKind::Since
            | TokenKind::Until
            | TokenKind::On
            | TokenKind::Between
            | TokenKind::Over => {
                // Allow chained temporal constraints by parsing another
                // For now, we'll overwrite - could extend to support multiple
                self.state = ParserState::Temporal;
            }
            _ => {
                self.state = ParserState::Done;
            }
        }

        Ok(())
    }

    /// Parse actions after so/therefore
    fn parse_actions(&mut self) -> Result<(), ParseError> {
        // Consume the so/therefore keyword
        let keyword_token = self.next();
        let keyword = keyword_token.as_ref().map(|t| t.text).unwrap_or("so");
        let keyword_pos = keyword_token.as_ref().map(|t| t.offset).unwrap_or(0);

        // Collect actions
        let mut found_action = false;
        loop {
            if self.at_eof() {
                if !found_action {
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
                // Collect actions
                TokenKind::Identifier | TokenKind::QuotedString | TokenKind::NaturalPredicate => {
                    let t = self.next().unwrap();
                    self.query.actions.push(t.text);
                    found_action = true;
                }
                // Another so/therefore - continue
                TokenKind::So | TokenKind::Therefore => {
                    self.next();
                }
                TokenKind::Eof => {
                    self.state = ParserState::Done;
                    return Ok(());
                }
                _ => {
                    // Any other keyword ends actions
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

    fn parse(input: &str) -> Result<AxQuery<'_>, ParseError> {
        Parser::parse_str(input)
    }

    #[test]
    fn test_empty_query() {
        let query = parse("").unwrap();
        assert!(query.is_empty());
    }

    #[test]
    fn test_subjects_only() {
        let query = parse("ALICE BOB CHARLIE").unwrap();
        assert_eq!(query.subjects, vec!["ALICE", "BOB", "CHARLIE"]);
        assert!(query.predicates.is_empty());
    }

    #[test]
    fn test_subject_with_predicate() {
        let query = parse("ALICE is author").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author"]);
    }

    #[test]
    fn test_multiple_predicates() {
        let query = parse("ALICE is author developer").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author", "developer"]);
    }

    #[test]
    fn test_subject_predicate_context() {
        let query = parse("ALICE is author_of of GitHub").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author_of"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
    }

    #[test]
    fn test_with_actor() {
        let query = parse("ALICE is author_of of GitHub by BOB").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author_of"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
        assert_eq!(query.actors, vec!["BOB"]);
    }

    #[test]
    fn test_temporal_since() {
        let query = parse("ALICE is author since 2024-01-01").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author"]);
        assert_eq!(query.temporal, Some(TemporalClause::Since("2024-01-01")));
    }

    #[test]
    fn test_temporal_until() {
        let query = parse("ALICE is author until 2024-12-31").unwrap();
        assert_eq!(query.temporal, Some(TemporalClause::Until("2024-12-31")));
    }

    #[test]
    fn test_temporal_on() {
        let query = parse("ALICE is author on 2024-06-15").unwrap();
        assert_eq!(query.temporal, Some(TemporalClause::On("2024-06-15")));
    }

    #[test]
    fn test_temporal_between() {
        let query = parse("ALICE is author between 2024-01-01 and 2024-12-31").unwrap();
        assert_eq!(
            query.temporal,
            Some(TemporalClause::Between("2024-01-01", "2024-12-31"))
        );
    }

    #[test]
    fn test_temporal_over() {
        let query = parse("ALICE is experienced over 5y").unwrap();
        let temporal = query.temporal.unwrap();
        if let TemporalClause::Over(dur) = temporal {
            assert_eq!(dur.value, Some(5.0));
            assert_eq!(dur.unit, Some(super::super::ast::DurationUnit::Years));
        } else {
            panic!("Expected Over clause");
        }
    }

    #[test]
    fn test_so_action() {
        let query = parse("ALICE is author so notify").unwrap();
        assert_eq!(query.actions, vec!["notify"]);
    }

    #[test]
    fn test_therefore_action() {
        let query = parse("ALICE is author therefore send_email").unwrap();
        assert_eq!(query.actions, vec!["send_email"]);
    }

    #[test]
    fn test_full_query() {
        let query =
            parse("ALICE BOB is author_of of GitHub Linux by CHARLIE since 2024-01-01 so notify")
                .unwrap();
        assert_eq!(query.subjects, vec!["ALICE", "BOB"]);
        assert_eq!(query.predicates, vec!["author_of"]);
        assert_eq!(query.contexts, vec!["GitHub", "Linux"]);
        assert_eq!(query.actors, vec!["CHARLIE"]);
        assert_eq!(query.temporal, Some(TemporalClause::Since("2024-01-01")));
        assert_eq!(query.actions, vec!["notify"]);
    }

    #[test]
    fn test_quoted_strings() {
        let query = parse("'John Doe' is 'senior developer' of 'ACME Corp'").unwrap();
        assert_eq!(query.subjects, vec!["John Doe"]);
        assert_eq!(query.predicates, vec!["senior developer"]);
        assert_eq!(query.contexts, vec!["ACME Corp"]);
    }

    #[test]
    fn test_context_without_predicate() {
        let query = parse("ALICE of GitHub").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
        assert!(query.predicates.is_empty());
    }

    #[test]
    fn test_actor_without_predicate() {
        let query = parse("ALICE by BOB").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.actors, vec!["BOB"]);
    }

    #[test]
    fn test_from_context() {
        let query = parse("ALICE from GitHub").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
    }

    #[test]
    fn test_via_actor() {
        let query = parse("ALICE via BOB").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.actors, vec!["BOB"]);
    }

    #[test]
    fn test_are_plural() {
        let query = parse("ALICE BOB are authors").unwrap();
        assert_eq!(query.subjects, vec!["ALICE", "BOB"]);
        assert_eq!(query.predicates, vec!["authors"]);
    }

    #[test]
    fn test_missing_predicate_error() {
        let result = parse("ALICE is");
        assert!(result.is_err());
        if let Err(ParseError::MissingElement { element, .. }) = result {
            assert_eq!(element, "predicate");
        }
    }

    #[test]
    fn test_missing_context_error() {
        let result = parse("ALICE of");
        assert!(result.is_err());
        if let Err(ParseError::MissingElement { element, .. }) = result {
            assert_eq!(element, "context");
        }
    }

    #[test]
    fn test_missing_actor_error() {
        let result = parse("ALICE by");
        assert!(result.is_err());
        if let Err(ParseError::MissingElement { element, .. }) = result {
            assert_eq!(element, "actor");
        }
    }

    #[test]
    fn test_missing_and_in_between() {
        let result = parse("ALICE between 2024-01-01 2024-12-31");
        assert!(result.is_err());
    }

    #[test]
    fn test_unicode_subjects() {
        let query = parse("日本語 Москва is author").unwrap();
        assert_eq!(query.subjects, vec!["日本語", "Москва"]);
    }

    #[test]
    fn test_hyphenated_predicates() {
        let query = parse("ALICE is senior-developer").unwrap();
        assert_eq!(query.predicates, vec!["senior-developer"]);
    }

    #[test]
    fn test_underscore_predicates() {
        let query = parse("ALICE is is_author_of of GitHub").unwrap();
        assert_eq!(query.predicates, vec!["is_author_of"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
    }
}
