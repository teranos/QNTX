use serde::{Deserialize, Serialize};

/// Information about the loaded model
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ModelInfo {
    pub name: String,
    pub dimensions: usize,
    pub max_sequence_length: usize,
}

/// Result of an embedding operation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EmbeddingResult {
    pub text: String,
    pub embedding: Vec<f32>,
    pub tokens: usize,
    pub inference_ms: f64,
}

/// Batch embedding result
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BatchEmbeddingResult {
    pub embeddings: Vec<EmbeddingResult>,
    pub total_tokens: usize,
    pub total_inference_ms: f64,
}
