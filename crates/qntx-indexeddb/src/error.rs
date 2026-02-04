//! Error types for IndexedDB storage backend

use qntx_core::storage::StoreError;
use thiserror::Error;

/// Result type for IndexedDB operations
pub type Result<T> = std::result::Result<T, IndexedDbError>;

/// Errors that can occur during IndexedDB storage operations
#[derive(Debug, Error)]
pub enum IndexedDbError {
    /// IndexedDB is not available in this environment
    #[error("IndexedDB not available: {0}")]
    NotAvailable(String),

    /// Database open/upgrade error
    #[error("IndexedDB open error: {0}")]
    Open(String),

    /// Transaction error
    #[error("IndexedDB transaction error: {0}")]
    Transaction(String),

    /// Request error from IDB operation
    #[error("IndexedDB request error: {0}")]
    Request(String),

    /// JSON serialization/deserialization error
    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    /// Attestation with given ID already exists
    #[error("Attestation {0} already exists")]
    AlreadyExists(String),

    /// Attestation with given ID not found
    #[error("Attestation {0} not found")]
    NotFound(String),

    /// JavaScript value conversion error
    #[error("JS conversion error: {0}")]
    JsValue(String),
}

impl From<wasm_bindgen::JsValue> for IndexedDbError {
    fn from(val: wasm_bindgen::JsValue) -> Self {
        let msg = js_sys::JSON::stringify(&val)
            .map(|s| String::from(s))
            .unwrap_or_else(|_| format!("{:?}", val));
        IndexedDbError::Request(msg)
    }
}

/// Convert IndexedDbError to StoreError for the storage trait
impl From<IndexedDbError> for StoreError {
    fn from(err: IndexedDbError) -> Self {
        match err {
            IndexedDbError::AlreadyExists(id) => StoreError::AlreadyExists(id),
            IndexedDbError::NotFound(id) => StoreError::NotFound(id),
            IndexedDbError::Json(e) => StoreError::Serialization(e.to_string()),
            IndexedDbError::NotAvailable(msg) => {
                StoreError::Backend(format!("IndexedDB not available: {}", msg))
            }
            IndexedDbError::Open(msg) => StoreError::Backend(format!("IndexedDB open: {}", msg)),
            IndexedDbError::Transaction(msg) => {
                StoreError::Backend(format!("IndexedDB transaction: {}", msg))
            }
            IndexedDbError::Request(msg) => {
                StoreError::Backend(format!("IndexedDB request: {}", msg))
            }
            IndexedDbError::JsValue(msg) => StoreError::Backend(format!("IndexedDB JS: {}", msg)),
        }
    }
}
