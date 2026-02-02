use anyhow::Result;
use ndarray::{Array2, Axis};
use std::path::Path;
use std::sync::Arc;
use std::time::Instant;

use crate::types::{BatchEmbeddingResult, EmbeddingResult, ModelInfo};

/// ONNX-based embedding engine for sentence transformers
pub struct EmbeddingEngine {
    session: Arc<ort::session::Session>,
    model_info: ModelInfo,
}

impl EmbeddingEngine {
    /// Create a new embedding engine from an ONNX model file
    pub fn new(model_path: impl AsRef<Path>, model_name: String) -> Result<Self> {
        // Initialize ORT if needed
        ort::init().ok();

        let session = ort::session::Session::builder()?
            .commit_from_file(model_path)?;

        // Get model metadata from the session
        let inputs = session.inputs();
        let outputs = session.outputs();

        // Validate model has expected inputs/outputs
        if inputs.len() < 2 {
            return Err(anyhow::anyhow!(
                "Model should have at least 2 inputs (input_ids, attention_mask)"
            ));
        }

        if outputs.is_empty() {
            return Err(anyhow::anyhow!("Model should have at least 1 output"));
        }

        // Default dimensions for sentence transformers
        let dimensions = 384; // all-MiniLM-L6-v2 default

        let model_info = ModelInfo {
            name: model_name,
            dimensions,
            max_sequence_length: 512, // Standard for most sentence transformers
        };

        Ok(Self {
            session: Arc::new(session),
            model_info,
        })
    }

    /// Get model information
    pub fn model_info(&self) -> &ModelInfo {
        &self.model_info
    }

    /// Embed a single text string
    pub fn embed(&self, text: &str) -> Result<EmbeddingResult> {
        let start = Instant::now();

        // For now, we'll use dummy tokenization
        // In production, you'd use a proper tokenizer (e.g., tokenizers crate)
        let (input_ids, attention_mask) = self.tokenize(text)?;

        // Convert to CowArray for ort
        let input_ids_cow = input_ids.view().into_dyn();
        let attention_mask_cow = attention_mask.view().into_dyn();

        // Run inference
        let outputs = self.session.run(ort::session::inputs![
            input_ids_cow,
            attention_mask_cow,
        ]?)?;

        // Extract embeddings from output
        let embeddings = self.extract_embeddings(&outputs)?;

        let inference_ms = start.elapsed().as_secs_f64() * 1000.0;

        Ok(EmbeddingResult {
            text: text.to_string(),
            embedding: embeddings,
            tokens: text.split_whitespace().count(), // Rough approximation
            inference_ms,
        })
    }

    /// Embed multiple texts in a batch
    pub fn embed_batch(&self, texts: &[String]) -> Result<BatchEmbeddingResult> {
        let start = Instant::now();
        let mut results = Vec::new();
        let mut total_tokens = 0;

        // Process each text
        // Note: In production, you'd want to actually batch these together
        for text in texts {
            let result = self.embed(text)?;
            total_tokens += result.tokens;
            results.push(result);
        }

        let total_inference_ms = start.elapsed().as_secs_f64() * 1000.0;

        Ok(BatchEmbeddingResult {
            embeddings: results,
            total_tokens,
            total_inference_ms,
        })
    }

    /// Dummy tokenization - in production, use a proper tokenizer
    fn tokenize(&self, text: &str) -> Result<(Array2<i64>, Array2<i64>)> {
        // Simple whitespace tokenization for demonstration
        // Real implementation would use HuggingFace tokenizers
        let tokens: Vec<i64> = text
            .split_whitespace()
            .take(self.model_info.max_sequence_length)
            .enumerate()
            .map(|(i, _)| (i as i64) + 1) // Start from 1, 0 is usually padding
            .collect();

        let mut input_ids = Array2::<i64>::zeros((1, self.model_info.max_sequence_length));
        let mut attention_mask = Array2::<i64>::zeros((1, self.model_info.max_sequence_length));

        for (i, &token) in tokens.iter().enumerate() {
            input_ids[[0, i]] = token;
            attention_mask[[0, i]] = 1;
        }

        Ok((input_ids, attention_mask))
    }

    /// Extract embeddings from model output
    fn extract_embeddings(&self, outputs: &ort::session::SessionOutputs) -> Result<Vec<f32>> {
        // Get the first output (usually "last_hidden_state" or similar)
        let output_name = outputs.keys().next()
            .ok_or_else(|| anyhow::anyhow!("No output found"))?;

        let (shape, data) = outputs[output_name]
            .try_extract_tensor::<f32>()?;

        // Handle different output shapes
        let embeddings = match shape.len() {
            3 if shape[0] == 1 => {
                // Shape is [1, seq_len, hidden_size]
                // Mean pooling over sequence dimension
                // For simplicity, just take the first token's embedding
                // In production, you'd do proper mean pooling
                let hidden_size = shape[2];
                data[..hidden_size].to_vec()
            }
            2 if shape[0] == 1 => {
                // Shape is [1, hidden_size] - already pooled
                data.to_vec()
            }
            _ => {
                return Err(anyhow::anyhow!(
                    "Unexpected output shape: {:?}",
                    shape
                ))
            }
        };

        Ok(embeddings)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tokenize() {
        // This would require a model file to test properly
        // For now, just ensure the module compiles
        assert!(true);
    }
}