//! Actor credibility ranking

use serde::{Deserialize, Serialize};

/// Actor credibility levels
///
/// Higher values indicate more trustworthy sources.
/// Used for conflict resolution when claims disagree.
#[derive(
    Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize, Default,
)]
#[repr(u8)]
pub enum ActorCredibility {
    /// Unknown external sources (lowest trust)
    #[default]
    External = 0,
    /// Automated systems
    System = 1,
    /// AI-generated claims
    Llm = 2,
    /// Human-verified claims (highest trust)
    Human = 3,
}

impl ActorCredibility {
    /// Infer credibility from actor identifier
    ///
    /// # Patterns recognized:
    /// - `human:*` or `*@verified` → Human
    /// - `llm:*` or contains `gpt`/`claude` → Llm
    /// - `system:*` → System
    /// - Everything else → External
    pub fn from_actor(actor: &str) -> Self {
        let lower = actor.to_lowercase();

        if lower.starts_with("human:") || lower.ends_with("@verified") {
            Self::Human
        } else if lower.starts_with("llm:")
            || lower.contains("gpt")
            || lower.contains("claude")
            || lower.contains("anthropic")
            || lower.contains("openai")
        {
            Self::Llm
        } else if lower.starts_with("system:") || lower.starts_with("qntx:") {
            Self::System
        } else {
            Self::External
        }
    }

    /// Check if this is a human actor
    pub fn is_human(&self) -> bool {
        *self == Self::Human
    }

    /// Check if this actor should override another
    pub fn overrides(&self, other: &Self) -> bool {
        *self > *other
    }

    /// Get credibility score as float (0.0 - 1.0)
    pub fn score(&self) -> f64 {
        match self {
            Self::External => 0.25,
            Self::System => 0.50,
            Self::Llm => 0.75,
            Self::Human => 1.0,
        }
    }
}

impl std::fmt::Display for ActorCredibility {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::External => write!(f, "external"),
            Self::System => write!(f, "system"),
            Self::Llm => write!(f, "llm"),
            Self::Human => write!(f, "human"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_human_patterns() {
        assert_eq!(
            ActorCredibility::from_actor("human:alice"),
            ActorCredibility::Human
        );
        assert_eq!(
            ActorCredibility::from_actor("alice@verified"),
            ActorCredibility::Human
        );
    }

    #[test]
    fn test_llm_patterns() {
        assert_eq!(
            ActorCredibility::from_actor("llm:gpt-4"),
            ActorCredibility::Llm
        );
        assert_eq!(
            ActorCredibility::from_actor("claude-3-opus"),
            ActorCredibility::Llm
        );
        assert_eq!(
            ActorCredibility::from_actor("openai-assistant"),
            ActorCredibility::Llm
        );
    }

    #[test]
    fn test_system_patterns() {
        assert_eq!(
            ActorCredibility::from_actor("system:hr"),
            ActorCredibility::System
        );
        assert_eq!(
            ActorCredibility::from_actor("qntx:pulse"),
            ActorCredibility::System
        );
    }

    #[test]
    fn test_external_default() {
        assert_eq!(
            ActorCredibility::from_actor("unknown-source"),
            ActorCredibility::External
        );
        assert_eq!(
            ActorCredibility::from_actor("api.example.com"),
            ActorCredibility::External
        );
    }

    #[test]
    fn test_ordering() {
        assert!(ActorCredibility::Human > ActorCredibility::Llm);
        assert!(ActorCredibility::Llm > ActorCredibility::System);
        assert!(ActorCredibility::System > ActorCredibility::External);
    }

    #[test]
    fn test_overrides() {
        assert!(ActorCredibility::Human.overrides(&ActorCredibility::Llm));
        assert!(!ActorCredibility::System.overrides(&ActorCredibility::Human));
    }
}
