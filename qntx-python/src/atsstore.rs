//! ATSStore gRPC client for creating attestations from Python code.
//!
//! Provides a blocking wrapper around the ATSStore gRPC client that can be
//! called from PyO3 functions during Python execution.

use crate::proto::{
    ats_store_service_client::AtsStoreServiceClient, AttestationCommand, GenerateAttestationRequest,
};
use pyo3::prelude::*;
use pyo3::types::PyDict;
use std::cell::RefCell;
use std::collections::HashMap;
use std::sync::Arc;
use tonic::transport::Channel;
use tracing::{error, info};

// Thread-local storage for the ATSStore client during Python execution
thread_local! {
    static CURRENT_CLIENT: RefCell<Option<SharedAtsStoreClient>> = const { RefCell::new(None) };
    // Glyph ID for actor convention: when set, attest() defaults actor to "glyph:{id}"
    static CURRENT_GLYPH_ID: RefCell<Option<String>> = const { RefCell::new(None) };
}

/// ATSStore client configuration
#[derive(Debug, Clone)]
pub struct AtsStoreConfig {
    pub endpoint: String,
    pub auth_token: String,
}

/// ATSStore client wrapper with blocking operations for PyO3 compatibility
///
/// TODO: Implement connection pooling - currently creates fresh connection per operation
/// Each thread spawns its own runtime and connection, which works but is inefficient
pub struct AtsStoreClient {
    config: AtsStoreConfig,
}

impl AtsStoreClient {
    /// Create a new ATSStore client
    pub fn new(config: AtsStoreConfig) -> Self {
        Self { config }
    }

    /// Create an attestation with auto-generated ID
    ///
    /// This is the main function called from Python via `attest()`.
    pub fn create_attestation(
        &mut self,
        subjects: Vec<String>,
        predicates: Vec<String>,
        contexts: Vec<String>,
        actors: Option<Vec<String>>,
        attributes: Option<HashMap<String, serde_json::Value>>,
    ) -> Result<AttestationResult, String> {
        // Get endpoint for use in spawned thread
        let endpoint = self.config.endpoint.clone();
        let auth_token = self.config.auth_token.clone();

        // Convert attributes to prost Struct if provided
        let attributes =
            attributes.map(|attrs| qntx_proto::serde_struct::json_map_to_struct(&attrs));

        let command = AttestationCommand {
            subjects,
            predicates,
            contexts,
            actors: actors.unwrap_or_default(),
            timestamp: None, // Server will use current time
            attributes,
        };

        let request = GenerateAttestationRequest {
            auth_token,
            command: Some(command),
        };

        // Spawn a separate OS thread with its own runtime (avoid "runtime within runtime" error)
        let response = std::thread::spawn(move || {
            let rt = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .map_err(|e| format!("failed to create runtime: {}", e))?;

            rt.block_on(async {
                // Create fresh connection inside the spawned thread's async context
                // Ensure endpoint has http:// scheme for tonic
                let endpoint_uri =
                    if endpoint.starts_with("http://") || endpoint.starts_with("https://") {
                        endpoint.clone()
                    } else {
                        format!("http://{}", endpoint)
                    };

                let ep = Channel::from_shared(endpoint_uri)
                    .map_err(|e| format!("invalid endpoint: {}", e))?;

                let channel = ep
                    .connect()
                    .await
                    .map_err(|e| format!("connection failed: {}", e))?;

                let mut client = AtsStoreServiceClient::new(channel);
                client
                    .generate_and_create_attestation(request)
                    .await
                    .map_err(|e| format!("gRPC error: {}", e))
            })
        })
        .join()
        .map_err(|e| format!("thread panicked: {:?}", e))??
        .into_inner();

        if !response.success {
            error!("Failed to create attestation: {}", response.error);
            return Err(response.error);
        }

        let attestation = response
            .attestation
            .ok_or_else(|| "server returned success but no attestation".to_string())?;

        info!("Created attestation via Python: {}", attestation.id);

        Ok(AttestationResult {
            id: attestation.id,
            subjects: attestation.subjects,
            predicates: attestation.predicates,
            contexts: attestation.contexts,
            actors: attestation.actors,
            timestamp: attestation.timestamp,
            source: attestation.source,
        })
    }
}

/// Result of attestation creation, returned to Python
#[derive(Debug, Clone)]
pub struct AttestationResult {
    pub id: String,
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    pub timestamp: i64,
    pub source: String,
}

/// Shared ATSStore client that can be passed to Python execution context
pub type SharedAtsStoreClient = Arc<parking_lot::Mutex<Option<AtsStoreClient>>>;

/// Create a new shared ATSStore client
pub fn new_shared_client() -> SharedAtsStoreClient {
    Arc::new(parking_lot::Mutex::new(None))
}

/// Initialize the shared client with config
pub fn init_shared_client(shared: &SharedAtsStoreClient, config: AtsStoreConfig) {
    let mut guard = shared.lock();
    *guard = Some(AtsStoreClient::new(config));
}

/// Set the current ATSStore client for the executing thread.
/// Called before Python execution to make attest() available.
pub fn set_current_client(client: SharedAtsStoreClient) {
    CURRENT_CLIENT.with(|c| {
        *c.borrow_mut() = Some(client);
    });
}

/// Clear the current ATSStore client after Python execution.
pub fn clear_current_client() {
    CURRENT_CLIENT.with(|c| {
        *c.borrow_mut() = None;
    });
}

/// Set the current glyph ID for actor convention.
/// When set, attest() defaults actor to "glyph:{id}" if no explicit actors provided.
pub fn set_current_glyph_id(glyph_id: Option<String>) {
    CURRENT_GLYPH_ID.with(|g| {
        *g.borrow_mut() = glyph_id;
    });
}

/// Python-callable attest function.
/// Creates an attestation using the current thread's ATSStore client.
#[pyfunction]
#[pyo3(signature = (subjects, predicates, contexts, actors=None, attributes=None))]
pub fn attest(
    py: Python<'_>,
    subjects: Vec<String>,
    predicates: Vec<String>,
    contexts: Vec<String>,
    actors: Option<Vec<String>>,
    attributes: Option<Bound<'_, PyDict>>,
) -> PyResult<PyObject> {
    // Convert Python dict to Rust HashMap if provided
    let attrs: Option<HashMap<String, serde_json::Value>> = match attributes {
        Some(dict) => {
            let mut map = HashMap::new();
            for (key, value) in dict.iter() {
                let k: String = key.extract()?;
                let v = python_value_to_json(py, &value)?;
                map.insert(k, v);
            }
            Some(map)
        }
        None => None,
    };

    // Default actors to "glyph:{id}" when glyph_id is set and user didn't pass explicit actors
    let actors = match actors {
        Some(a) if !a.is_empty() => Some(a), // User provided explicit actors — use them
        _ => {
            // Check if a glyph ID is set for this execution
            CURRENT_GLYPH_ID.with(|g| g.borrow().as_ref().map(|id| vec![format!("glyph:{}", id)]))
        }
    };

    // Get the current client from thread-local storage
    let result = CURRENT_CLIENT.with(|c| {
        let client_opt = c.borrow();
        match client_opt.as_ref() {
            Some(shared_client) => {
                let mut guard = shared_client.lock();
                match guard.as_mut() {
                    Some(client) => {
                        client.create_attestation(subjects, predicates, contexts, actors, attrs)
                    }
                    None => Err("ATSStore client not initialized".to_string()),
                }
            }
            None => Err("ATSStore client not available in this context".to_string()),
        }
    });

    match result {
        Ok(attestation) => {
            // Return a dict with the attestation details
            let dict = PyDict::new(py);
            dict.set_item("id", &attestation.id)?;
            dict.set_item("subjects", &attestation.subjects)?;
            dict.set_item("predicates", &attestation.predicates)?;
            dict.set_item("contexts", &attestation.contexts)?;
            dict.set_item("actors", &attestation.actors)?;
            dict.set_item("timestamp", attestation.timestamp)?;
            dict.set_item("source", &attestation.source)?;
            Ok(dict.into())
        }
        Err(e) => Err(pyo3::exceptions::PyRuntimeError::new_err(e)),
    }
}

/// Convert a Python value to serde_json::Value
fn python_value_to_json(_py: Python<'_>, value: &Bound<'_, PyAny>) -> PyResult<serde_json::Value> {
    if value.is_none() {
        Ok(serde_json::Value::Null)
    } else if let Ok(b) = value.extract::<bool>() {
        Ok(serde_json::Value::Bool(b))
    } else if let Ok(i) = value.extract::<i64>() {
        Ok(serde_json::Value::Number(i.into()))
    } else if let Ok(f) = value.extract::<f64>() {
        Ok(serde_json::json!(f))
    } else if let Ok(s) = value.extract::<String>() {
        Ok(serde_json::Value::String(s))
    } else if let Ok(list) = value.downcast::<pyo3::types::PyList>() {
        let vec: Result<Vec<_>, _> = list.iter().map(|v| python_value_to_json(_py, &v)).collect();
        Ok(serde_json::Value::Array(vec?))
    } else if let Ok(dict) = value.downcast::<PyDict>() {
        let mut map = serde_json::Map::new();
        for (k, v) in dict.iter() {
            let key: String = k.extract()?;
            map.insert(key, python_value_to_json(_py, &v)?);
        }
        Ok(serde_json::Value::Object(map))
    } else {
        // Fallback: convert to string representation
        Ok(serde_json::Value::String(value.str()?.to_string()))
    }
}

/// Add the attest function to a Python globals dict.
/// Call this before executing Python code to make attest() available.
pub fn inject_attest_function(py: Python<'_>, globals: &Bound<'_, PyDict>) -> PyResult<()> {
    // Create the attest function and add it to globals
    let attest_fn = wrap_pyfunction!(attest, py)?;
    globals.set_item("attest", attest_fn)?;
    Ok(())
}
