//! Claim Classification & Conflict Resolution
//!
//! Analyzes relationships between attestation claims to determine:
//!
//! - **Evolution**: Same actor updated their claim over time
//! - **Verification**: Multiple sources agreeing (strengthens claim)
//! - **Coexistence**: Different contexts, both valid
//! - **Supersession**: Higher authority overrides lower
//! - **Conflict**: Genuine disagreement requiring review
//!
//! # Credibility Hierarchy
//!
//! ```text
//! Human (3) > LLM (2) > System (1) > External (0)
//! ```
//!
//! # Example
//!
//! ```rust
//! use qntx_core::classify::{ActorCredibility, ConflictType};
//!
//! let cred = ActorCredibility::from_actor("human:alice@verified");
//! assert_eq!(cred, ActorCredibility::Human);
//! ```

mod credibility;
mod types;

pub use credibility::ActorCredibility;
pub use types::{ClassificationResult, ConflictType};
