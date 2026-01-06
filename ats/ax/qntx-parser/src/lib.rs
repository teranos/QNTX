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
//! use qntx_parser::ax::Lexer;
//!
//! let input = "ALICE is author of GitHub";
//! let lexer = Lexer::new(input);
//! let tokens: Vec<_> = lexer.collect();
//! ```

pub mod ax;

// Re-export main types
pub use ax::{Lexer, Token, TokenKind};
