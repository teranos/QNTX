use anyhow::Result;
use ndarray::Array2;
use ort::session::builder::GraphOptimizationLevel;
use ort::session::{Session, SessionOutputs};
use ort::value::Value;
use std::path::Path;
use std::time::Instant;

use crate::tokenizer::EmbeddingTokenizer;
use crate::types::{BatchEmbeddingResult, EmbeddingResult, ModelInfo};

/// ONNX-based embedding engine for sentence transformers
pub struct EmbeddingEngine {
    session: Session,
    model_info: ModelInfo,
    tokenizer: EmbeddingTokenizer,
}

impl EmbeddingEngine {
    /// Initialize the ONNX Runtime environment (idempotent, safe to call multiple times)
    pub fn init_ort() {
        ort::init().commit();
    }

    /// Create a new embedding engine from an ONNX model file
    pub fn new(model_path: impl AsRef<Path>, model_name: String) -> Result<Self> {
        Self::init_ort();

        // Load the ONNX model
        let session = Session::builder()?
            .with_optimization_level(GraphOptimizationLevel::Level3)?
            .with_intra_threads(4)?
            .commit_from_file(&model_path)?;

        // Get model metadata
        let inputs = session.inputs();
        let outputs = session.outputs();

        // Validate model has expected inputs/outputs
        if inputs.len() < 2 {
            return Err(anyhow::anyhow!(
                "Model should have at least 2 inputs (input_ids, attention_mask), got {}",
                inputs.len()
            ));
        }

        if outputs.is_empty() {
            return Err(anyhow::anyhow!("Model should have at least 1 output"));
        }

        // Log input/output info
        eprintln!("Model inputs:");
        for (i, input) in inputs.iter().enumerate() {
            eprintln!("  [{}] {}", i, input.name());
        }
        eprintln!("Model outputs:");
        for (i, output) in outputs.iter().enumerate() {
            eprintln!("  [{}] {}", i, output.name());
        }

        // Default dimensions for all-MiniLM-L6-v2
        let dimensions = 384;
        let max_sequence_length = 512;

        let model_info = ModelInfo {
            name: model_name,
            dimensions,
            max_sequence_length,
        };

        // Load tokenizer from same directory as model
        let model_dir = model_path
            .as_ref()
            .parent()
            .ok_or_else(|| anyhow::anyhow!("Invalid model path"))?;
        let tokenizer_path = model_dir.join("tokenizer.json");

        eprintln!("Loading tokenizer from: {:?}", tokenizer_path);
        let tokenizer = EmbeddingTokenizer::from_file(tokenizer_path, max_sequence_length)?;

        Ok(Self {
            session,
            model_info,
            tokenizer,
        })
    }

    /// Get model information
    pub fn model_info(&self) -> &ModelInfo {
        &self.model_info
    }

    /// Embed a single text string
    pub fn embed(&mut self, text: &str) -> Result<EmbeddingResult> {
        let start = Instant::now();

        // Use real tokenization
        let (input_ids, attention_mask) = self.tokenizer.encode(text)?;

        // Get input names from the model
        let input_names: Vec<String> = self
            .session
            .inputs()
            .iter()
            .map(|i| i.name().to_string())
            .collect();

        // Make sure we have the expected inputs
        if input_names.len() < 2 {
            return Err(anyhow::anyhow!("Model doesn't have expected inputs"));
        }

        // Create Value types from arrays - need to pass as (shape, data) tuple
        let input_ids_shape = input_ids.shape().to_vec();
        let input_ids_data = input_ids.as_slice().unwrap().to_vec();
        let input_ids_value = Value::from_array((input_ids_shape, input_ids_data))?;

        let attention_mask_shape = attention_mask.shape().to_vec();
        let attention_mask_data = attention_mask.as_slice().unwrap().to_vec();
        let attention_mask_value = Value::from_array((attention_mask_shape, attention_mask_data))?;

        // Check if model needs token_type_ids (BERT models do)
        let inputs = if input_names.len() >= 3 && input_names[2] == "token_type_ids" {
            // Create token_type_ids (all zeros for single sentence)
            let token_type_ids = Array2::<i64>::zeros((1, self.model_info.max_sequence_length));
            let token_type_shape = token_type_ids.shape().to_vec();
            let token_type_data = token_type_ids.as_slice().unwrap().to_vec();
            let token_type_value = Value::from_array((token_type_shape, token_type_data))?;

            ort::inputs![
                input_names[0].as_str() => input_ids_value,
                input_names[1].as_str() => attention_mask_value,
                input_names[2].as_str() => token_type_value,
            ]
        } else {
            ort::inputs![
                input_names[0].as_str() => input_ids_value,
                input_names[1].as_str() => attention_mask_value,
            ]
        };

        // Run inference
        let outputs = self.session.run(inputs)?;

        // Extract embeddings from output
        let embeddings = Self::extract_embeddings(&outputs, self.model_info.dimensions)?;

        let inference_ms = start.elapsed().as_secs_f64() * 1000.0;

        // Count actual tokens from attention mask (non-padding tokens)
        let tokens = attention_mask.iter().filter(|&&x| x == 1).count();

        Ok(EmbeddingResult {
            text: text.to_string(),
            embedding: embeddings,
            tokens,
            inference_ms,
        })
    }

    /// Embed multiple texts in a batch
    pub fn embed_batch(&mut self, texts: &[String]) -> Result<BatchEmbeddingResult> {
        let start = Instant::now();
        let mut results = Vec::new();
        let mut total_tokens = 0;

        // Process each text individually for now
        // TODO: Implement actual batching
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

    /// Extract embeddings from model output with mean pooling
    fn extract_embeddings(
        outputs: &SessionOutputs,
        expected_dimensions: usize,
    ) -> Result<Vec<f32>> {
        // Get the output names
        let output_names: Vec<String> = outputs.keys().map(|s| s.to_string()).collect();

        if output_names.is_empty() {
            return Err(anyhow::anyhow!("No outputs found"));
        }

        // Try to find the right output (usually "last_hidden_state" or the first output)
        let output_name = output_names
            .iter()
            .find(|name: &&String| name.contains("hidden_state") || name.contains("output"))
            .unwrap_or(&output_names[0]);

        // Extract the tensor - SessionOutputs can be indexed by &str
        let (tensor_shape, data) = outputs[output_name.as_str()].try_extract_tensor::<f32>()?;

        // Shape is an ort::tensor::Shape which we can iterate
        let shape: Vec<i64> = tensor_shape.iter().copied().collect();
        eprintln!("Output tensor shape: {:?}", shape);

        // Handle different output shapes
        let embeddings = match shape.len() {
            3 => {
                // Shape is [batch, seq_len, hidden_size]
                // Perform mean pooling over the sequence dimension
                let batch = shape[0] as usize;
                let seq_len = shape[1] as usize;
                let hidden_size = shape[2] as usize;

                if batch != 1 {
                    return Err(anyhow::anyhow!("Batch size should be 1, got {}", batch));
                }

                // Mean pooling: average all token embeddings
                let mut pooled = vec![0.0f32; hidden_size];

                // Count actual tokens (non-padding)
                let mut token_count = 0;
                for seq_idx in 0..seq_len {
                    // Check if this position has a non-zero embedding (simple heuristic)
                    let start = seq_idx * hidden_size;
                    let mut is_padding = true;
                    for i in 0..hidden_size.min(10) {
                        if data[start + i] != 0.0 {
                            is_padding = false;
                            break;
                        }
                    }

                    if !is_padding {
                        token_count += 1;
                        for i in 0..hidden_size {
                            pooled[i] += data[start + i];
                        }
                    }
                }

                // Average
                if token_count > 0 {
                    for val in &mut pooled {
                        *val /= token_count as f32;
                    }
                }

                pooled
            }
            2 => {
                // Shape is [batch, hidden_size] - already pooled
                let batch = shape[0] as usize;
                let hidden_size = shape[1] as usize;

                if batch != 1 {
                    return Err(anyhow::anyhow!("Batch size should be 1, got {}", batch));
                }

                data[..hidden_size].to_vec()
            }
            _ => return Err(anyhow::anyhow!("Unexpected output shape: {:?}", shape)),
        };

        // Ensure we have the right dimension
        if embeddings.len() != expected_dimensions {
            eprintln!(
                "Warning: Expected {} dimensions, got {}. Truncating/padding.",
                expected_dimensions,
                embeddings.len()
            );
            let mut result = vec![0.0f32; expected_dimensions];
            let copy_len = embeddings.len().min(expected_dimensions);
            result[..copy_len].copy_from_slice(&embeddings[..copy_len]);
            Ok(result)
        } else {
            Ok(embeddings)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tokenize() {
        // Would require a model file to test properly
        // For now, just ensure the module compiles
        assert!(true);
    }
}
