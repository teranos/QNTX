//! WASI (server-side) entry point for QNTX verification
//!
//! This allows the SAME WASM binary to run on servers via:
//! - Wasmtime (Rust runtime)
//! - Wasmer (Multiple languages)
//! - Node.js (with WASI support)
//! - Deno
//! - Cloudflare Workers (WASI preview)

use serde::{Deserialize, Serialize};
use std::io::{self, Read, Write};

// Use the same attestation structure
#[derive(Serialize, Deserialize, Debug)]
pub struct Attestation {
    pub id: String,
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    pub timestamp: i64,
    pub source: String,
    pub attributes: serde_json::Value,
}

impl Attestation {
    /// Verify that this attestation matches the given patterns
    pub fn verify(
        &self,
        subject_pattern: Option<&str>,
        predicate_pattern: Option<&str>,
        context_pattern: Option<&str>,
        actor_pattern: Option<&str>,
    ) -> bool {
        // Check subject pattern
        if let Some(pattern) = subject_pattern {
            if !self.subjects.iter().any(|s| s.contains(pattern)) {
                return false;
            }
        }

        // Check predicate pattern
        if let Some(pattern) = predicate_pattern {
            if !self.predicates.iter().any(|p| p.contains(pattern)) {
                return false;
            }
        }

        // Check context pattern
        if let Some(pattern) = context_pattern {
            if !self.contexts.iter().any(|c| c.contains(pattern)) {
                return false;
            }
        }

        // Check actor pattern
        if let Some(pattern) = actor_pattern {
            if !self.actors.iter().any(|a| a.contains(pattern)) {
                return false;
            }
        }

        true
    }
}

/// Command for the WASI CLI
#[derive(Serialize, Deserialize, Debug)]
#[serde(tag = "cmd")]
pub enum Command {
    Verify {
        attestation: Attestation,
        subject: Option<String>,
        predicate: Option<String>,
        context: Option<String>,
        actor: Option<String>,
    },
    Filter {
        attestations: Vec<Attestation>,
        subject: Option<String>,
        predicate: Option<String>,
        context: Option<String>,
        actor: Option<String>,
    },
}

/// Response from WASI execution
#[derive(Serialize, Deserialize, Debug)]
#[serde(tag = "status")]
pub enum Response {
    Success { result: serde_json::Value },
    Error { message: String },
}

fn main() {
    // Read JSON command from stdin
    let mut input = String::new();
    if let Err(e) = io::stdin().read_to_string(&mut input) {
        let response = Response::Error {
            message: format!("Failed to read input: {}", e),
        };
        println!("{}", serde_json::to_string(&response).unwrap());
        return;
    }

    // Parse command
    let command: Command = match serde_json::from_str(&input) {
        Ok(cmd) => cmd,
        Err(e) => {
            let response = Response::Error {
                message: format!("Failed to parse command: {}", e),
            };
            println!("{}", serde_json::to_string(&response).unwrap());
            return;
        }
    };

    // Execute command
    let response = match command {
        Command::Verify {
            attestation,
            subject,
            predicate,
            context,
            actor,
        } => {
            let is_valid = attestation.verify(
                subject.as_deref(),
                predicate.as_deref(),
                context.as_deref(),
                actor.as_deref(),
            );
            Response::Success {
                result: serde_json::json!({
                    "valid": is_valid,
                    "attestation_id": attestation.id
                }),
            }
        }
        Command::Filter {
            attestations,
            subject,
            predicate,
            context,
            actor,
        } => {
            let filtered: Vec<&Attestation> = attestations
                .iter()
                .filter(|att| {
                    att.verify(
                        subject.as_deref(),
                        predicate.as_deref(),
                        context.as_deref(),
                        actor.as_deref(),
                    )
                })
                .collect();

            Response::Success {
                result: serde_json::json!({
                    "count": filtered.len(),
                    "attestations": filtered
                }),
            }
        }
    };

    // Write response to stdout
    println!("{}", serde_json::to_string(&response).unwrap());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_attestation_verify() {
        let att = Attestation {
            id: "test-001".to_string(),
            subjects: vec!["user:alice".to_string(), "project:qntx".to_string()],
            predicates: vec!["created".to_string()],
            contexts: vec!["dev".to_string()],
            actors: vec!["system".to_string()],
            timestamp: 1704067200,
            source: "test".to_string(),
            attributes: serde_json::json!({}),
        };

        assert!(att.verify(Some("alice"), None, None, None));
        assert!(att.verify(None, Some("created"), None, None));
        assert!(!att.verify(Some("bob"), None, None, None));
    }
}
