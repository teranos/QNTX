//! QNTX AX Query Parser
//!
//! A high-performance parser for AX query language, designed to be called from Go via FFI.
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
//! use qntx_parser::{Parser, AxQuery};
//!
//! let input = "ALICE is author of GitHub";
//! let query = Parser::parse_str(input).unwrap();
//!
//! assert_eq!(query.subjects, vec!["ALICE"]);
//! assert_eq!(query.predicates, vec!["author"]);
//! assert_eq!(query.contexts, vec!["GitHub"]);
//! ```

pub mod ax;
pub mod ffi;

// Re-export main types
pub use ax::{
    AxQuery, DurationExpr, DurationUnit, Lexer, ParseError, Parser, TemporalClause, Token,
    TokenKind,
};

// Re-export FFI types
pub use ffi::{
    parser_parse_query, parser_result_free, parser_string_free, AxQueryResultC, DurationUnitC,
    TemporalClauseC, TemporalTypeC,
};
