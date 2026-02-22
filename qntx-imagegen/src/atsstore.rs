//! ATSStore gRPC client for creating attestations on image generation completion.

use crate::proto::{
    ats_store_service_client::AtsStoreServiceClient, AttestationCommand,
    GenerateAttestationRequest,
};
use std::collections::HashMap;
use std::sync::Arc;
use tonic::transport::Channel;
use tracing::{error, info, warn};

/// ATSStore client configuration
#[derive(Debug, Clone)]
pub struct AtsStoreConfig {
    pub endpoint: String,
    pub auth_token: String,
}

/// ATSStore client wrapper
pub struct AtsStoreClient {
    config: AtsStoreConfig,
}

impl AtsStoreClient {
    pub fn new(config: AtsStoreConfig) -> Self {
        Self { config }
    }

    /// Create an attestation for a generated image
    pub fn attest(
        &self,
        subjects: Vec<String>,
        predicates: Vec<String>,
        contexts: Vec<String>,
        actors: Vec<String>,
        attributes: Option<HashMap<String, serde_json::Value>>,
    ) -> Result<String, String> {
        let endpoint = self.config.endpoint.clone();
        let auth_token = self.config.auth_token.clone();

        let attributes =
            attributes.map(|attrs| qntx_proto::serde_struct::json_map_to_struct(&attrs));

        let command = AttestationCommand {
            subjects,
            predicates,
            contexts,
            actors,
            timestamp: None,
            attributes,
        };

        let request = GenerateAttestationRequest {
            auth_token,
            command: Some(command),
        };

        // Spawn OS thread to avoid "runtime within runtime" error
        let response = std::thread::spawn(move || {
            let rt = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .map_err(|e| format!("failed to create runtime: {}", e))?;

            rt.block_on(async {
                let endpoint_uri =
                    if endpoint.starts_with("http://") || endpoint.starts_with("https://") {
                        endpoint.clone()
                    } else {
                        format!("http://{}", endpoint)
                    };

                let channel = Channel::from_shared(endpoint_uri)
                    .map_err(|e| format!("invalid endpoint: {}", e))?
                    .connect()
                    .await
                    .map_err(|e| format!("connection to ATSStore failed: {}", e))?;

                let mut client = AtsStoreServiceClient::new(channel);
                client
                    .generate_and_create_attestation(request)
                    .await
                    .map_err(|e| format!("gRPC error creating attestation: {}", e))
            })
        })
        .join()
        .map_err(|e| format!("attestation thread panicked: {:?}", e))??
        .into_inner();

        if !response.success {
            return Err(format!("ATSStore returned error: {}", response.error));
        }

        let attestation = response
            .attestation
            .ok_or_else(|| "ATSStore returned success but no attestation".to_string())?;

        info!("Created attestation for generated image: {}", attestation.id);
        Ok(attestation.id)
    }
}

/// Shared ATSStore client
pub type SharedAtsStoreClient = Arc<parking_lot::Mutex<Option<AtsStoreClient>>>;

/// Create a new shared ATSStore client (initially empty)
pub fn new_shared_client() -> SharedAtsStoreClient {
    Arc::new(parking_lot::Mutex::new(None))
}

/// Initialize the shared client with config
pub fn init_shared_client(shared: &SharedAtsStoreClient, config: AtsStoreConfig) {
    let mut guard = shared.lock();
    *guard = Some(AtsStoreClient::new(config));
}

/// Create an attestation for a generated image (fire-and-forget).
/// Logs warnings on failure but does not propagate errors.
pub fn create_attestation(
    client: &SharedAtsStoreClient,
    sha256: &str,
    prompt: &str,
    steps: u32,
    seed: u64,
    duration_ms: u64,
    output_path: &str,
) {
    let guard = client.lock();
    let client = match guard.as_ref() {
        Some(c) => c,
        None => {
            warn!("ATSStore client not initialized, skipping attestation");
            return;
        }
    };

    let mut attrs = HashMap::new();
    attrs.insert(
        "prompt".to_string(),
        serde_json::Value::String(prompt.to_string()),
    );
    attrs.insert(
        "model".to_string(),
        serde_json::Value::String("stable-diffusion-v1-5".to_string()),
    );
    attrs.insert(
        "steps".to_string(),
        serde_json::json!(steps),
    );
    attrs.insert(
        "seed".to_string(),
        serde_json::json!(seed),
    );
    attrs.insert(
        "duration_ms".to_string(),
        serde_json::json!(duration_ms),
    );
    attrs.insert(
        "output_path".to_string(),
        serde_json::Value::String(output_path.to_string()),
    );

    match client.attest(
        vec![format!("image:{}", sha256)],
        vec!["generated".to_string()],
        vec!["imagegen".to_string()],
        vec!["imagegen-plugin".to_string()],
        Some(attrs),
    ) {
        Ok(id) => info!("Attested generated image: {}", id),
        Err(e) => error!("Failed to attest generated image: {}", e),
    }
}
