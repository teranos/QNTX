//! Types for bounded storage enforcement

use serde::{Deserialize, Serialize};

/// Configuration for enforcement limits (16/64/64 strategy)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnforcementConfig {
    /// Max attestations per (actor, context) pair (default: 16)
    pub actor_context_limit: usize,
    /// Max contexts per actor (default: 64)
    pub actor_contexts_limit: usize,
    /// Max actors per entity/subject (default: 64)
    pub entity_actors_limit: usize,
}

impl Default for EnforcementConfig {
    fn default() -> Self {
        Self {
            actor_context_limit: 16,
            actor_contexts_limit: 64,
            entity_actors_limit: 64,
        }
    }
}

/// Details about what was evicted during enforcement
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EvictionDetails {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub evicted_actors: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub evicted_contexts: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub sample_predicates: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub sample_subjects: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_seen: Option<String>,
}

/// An enforcement event produced when limits are enforced
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnforcementEvent {
    pub event_type: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub actor: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub context: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub entity: Option<String>,
    pub deleted_count: usize,
    pub limit_value: usize,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub eviction_details: Option<EvictionDetails>,
}

/// Input for the enforce_limits FFI call
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnforcementInput {
    pub actors: Vec<String>,
    pub contexts: Vec<String>,
    pub subjects: Vec<String>,
    pub config: EnforcementConfig,
}
