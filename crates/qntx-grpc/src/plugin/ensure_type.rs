//! Convenience helper for attesting type definitions on plugin startup.
//!
//! Mirrors the Go-side `types.EnsureTypes` pattern: a type definition is just an
//! attestation with `subjects: [name]`, `predicates: ["type"]`, `actors: [name]`.
//!
//! # Example
//!
//! ```rust,ignore
//! use qntx_grpc::plugin::{TypeDef, ensure_types};
//!
//! let types = vec![
//!     TypeDef::new("observation", "Observation", "#e67e22")
//!         .rich_string_fields(vec!["message"]),
//! ];
//! ensure_types(&channel, &auth_token, "my-plugin", types).await?;
//! ```

use prost_types::value::Kind;
use prost_types::{Struct, Value};
use tracing::{debug, warn};

use super::proto::ats_store_service_client::AtsStoreServiceClient;
use super::proto::{
    AttestationCommand, AttestationFilter, GenerateAttestationRequest, GetAttestationsRequest,
};

/// A type definition to be attested. Mirrors Go's `types.TypeDef`.
#[derive(Clone, Debug)]
pub struct TypeDef {
    pub name: String,
    pub label: String,
    pub color: String,
    pub rich_string_fields: Vec<String>,
    pub array_fields: Vec<String>,
}

impl TypeDef {
    /// Create a new type definition with required fields.
    pub fn new(
        name: impl Into<String>,
        label: impl Into<String>,
        color: impl Into<String>,
    ) -> Self {
        Self {
            name: name.into(),
            label: label.into(),
            color: color.into(),
            rich_string_fields: Vec::new(),
            array_fields: Vec::new(),
        }
    }

    /// Set rich string fields (attributes that contain embeddable text).
    pub fn rich_string_fields(mut self, fields: Vec<&str>) -> Self {
        self.rich_string_fields = fields.into_iter().map(String::from).collect();
        self
    }

    /// Set array fields (attributes that should be flattened into arrays).
    pub fn array_fields(mut self, fields: Vec<&str>) -> Self {
        self.array_fields = fields.into_iter().map(String::from).collect();
        self
    }

    /// Build the attributes Struct for the type attestation.
    fn to_attributes(&self) -> Struct {
        let mut fields = std::collections::BTreeMap::new();

        fields.insert(
            "display_label".to_string(),
            Value {
                kind: Some(Kind::StringValue(self.label.clone())),
            },
        );
        fields.insert(
            "display_color".to_string(),
            Value {
                kind: Some(Kind::StringValue(self.color.clone())),
            },
        );
        fields.insert(
            "opacity".to_string(),
            Value {
                kind: Some(Kind::NumberValue(1.0)),
            },
        );
        fields.insert(
            "deprecated".to_string(),
            Value {
                kind: Some(Kind::BoolValue(false)),
            },
        );

        if !self.rich_string_fields.is_empty() {
            fields.insert(
                "rich_string_fields".to_string(),
                Value {
                    kind: Some(Kind::ListValue(prost_types::ListValue {
                        values: self
                            .rich_string_fields
                            .iter()
                            .map(|f| Value {
                                kind: Some(Kind::StringValue(f.clone())),
                            })
                            .collect(),
                    })),
                },
            );
        }

        if !self.array_fields.is_empty() {
            fields.insert(
                "array_fields".to_string(),
                Value {
                    kind: Some(Kind::ListValue(prost_types::ListValue {
                        values: self
                            .array_fields
                            .iter()
                            .map(|f| Value {
                                kind: Some(Kind::StringValue(f.clone())),
                            })
                            .collect(),
                    })),
                },
            );
        }

        Struct { fields }
    }
}

/// Ensure type definitions exist in the attestation store.
///
/// For each type, checks if a `[name] is type` attestation already exists.
/// If not, creates one via `GenerateAndCreateAttestation`.
///
/// Non-fatal: logs warnings on failure but continues with remaining types.
/// Returns the count of types that were newly created.
pub async fn ensure_types(
    channel: &tonic::transport::Channel,
    auth_token: &str,
    source: &str,
    types: Vec<TypeDef>,
) -> Result<usize, crate::error::Error> {
    let mut client = AtsStoreServiceClient::new(channel.clone());
    let mut created = 0;

    for def in &types {
        // Check if type already exists
        let exists_req = GetAttestationsRequest {
            auth_token: auth_token.to_string(),
            filter: Some(AttestationFilter {
                subjects: vec![def.name.clone()],
                predicates: vec!["type".to_string()],
                contexts: vec![],
                actors: vec![],
                time_start: None,
                time_end: None,
                limit: Some(1),
            }),
        };

        match client.get_attestations(exists_req).await {
            Ok(resp) => {
                let inner = resp.into_inner();
                if !inner.attestations.is_empty() {
                    debug!("type '{}' already attested, skipping", def.name);
                    continue;
                }
            }
            Err(e) => {
                warn!("failed to check type '{}': {}", def.name, e);
                continue;
            }
        }

        // Create the type attestation
        let command = AttestationCommand {
            subjects: vec![def.name.clone()],
            predicates: vec!["type".to_string()],
            contexts: vec![],
            actors: vec![def.name.clone()], // Self-certifying: type IS its own actor
            timestamp: None,
            attributes: Some(def.to_attributes()),
            source: source.to_string(),
            source_version: String::new(),
        };

        let req = GenerateAttestationRequest {
            auth_token: auth_token.to_string(),
            command: Some(command),
        };

        match client.generate_and_create_attestation(req).await {
            Ok(resp) => {
                let inner = resp.into_inner();
                if inner.success {
                    debug!("attested type '{}'", def.name);
                    created += 1;
                } else {
                    warn!("type '{}' attestation rejected: {}", def.name, inner.error);
                }
            }
            Err(e) => {
                warn!("type '{}' attestation RPC failed: {}", def.name, e);
            }
        }
    }

    Ok(created)
}
