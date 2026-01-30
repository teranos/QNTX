//! AX Query Language Lexer and Parser
//!
//! This module provides tokenization and parsing for the AX query language.
//!
//! # Modules
//!
//! - `lexer` - Zero-copy tokenizer
//! - `parser` - State-machine parser
//! - `ast` - Abstract syntax tree types
//!
//! # Example
//!
//! ```rust
//! use qntx_parser::ax::{Lexer, Parser, AxQuery};
//!
//! let input = "ALICE is author of GitHub";
//! let query = Parser::parse_str(input).unwrap();
//!
//! assert_eq!(query.subjects, vec!["ALICE"]);
//! assert_eq!(query.predicates, vec!["author"]);
//! assert_eq!(query.contexts, vec!["GitHub"]);
//! ```

mod ast;
mod keywords;
mod lexer;
mod parser;
mod token;

pub use ast::{AxQuery, DurationExpr, DurationUnit, TemporalClause};
pub use keywords::KeywordKind;
pub use lexer::Lexer;
pub use parser::{ParseError, Parser};
pub use token::{Token, TokenKind};
