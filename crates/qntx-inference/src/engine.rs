//! Inference engine for embedding generation.
//!
//! Supports ONNX models for text embeddings, optimized for semantic search.

use ndarray::{Array1, Array2, ArrayView1};
use ort::{GraphOptimizationLevel, Session};
use parking_lot::RwLock;
use std::path::Path;
use std::sync::Arc;
use thiserror::Error;
use tokenizers::Tokenizer;
use tracing::{debug, info};

#[derive(Error, Debug)]
pub enum EngineError {
    #[error("model not loaded")]
    ModelNotLoaded,

    #[error("failed to load model: {0}")]
    ModelLoad(String),

    #[error("failed to load tokenizer: {0}")]
    TokenizerLoad(String),

    #[error("tokenization failed: {0}")]
    Tokenization(String),

    #[error("inference failed: {0}")]
    Inference(String),

    #[error("invalid input: {0}")]
    InvalidInput(String),
}

pub type Result<T> = std::result::Result<T, EngineError>;

/// Configuration for the inference engine.
#[derive(Debug, Clone)]
pub struct EngineConfig {
    /// Path to the ONNX model file
    pub model_path: String,
    /// Path to the tokenizer.json file
    pub tokenizer_path: String,
    /// Maximum sequence length for tokenization
    pub max_length: usize,
    /// Whether to normalize output embeddings
    pub normalize: bool,
    /// Number of threads for inference (0 = auto)
    pub num_threads: usize,
}

impl Default for EngineConfig {
    fn default() -> Self {
        Self {
            model_path: String::new(),
            tokenizer_path: String::new(),
            max_length: 512,
            normalize: true,
            num_threads: 0,
        }
    }
}

/// Loaded model state.
struct LoadedModel {
    session: Session,
    tokenizer: Tokenizer,
    config: EngineConfig,
}

/// Inference engine for generating text embeddings.
pub struct InferenceEngine {
    model: RwLock<Option<LoadedModel>>,
}

impl InferenceEngine {
    /// Create a new inference engine (model not loaded).
    pub fn new() -> Self {
        Self {
            model: RwLock::new(None),
        }
    }

    /// Check if a model is loaded.
    pub fn is_loaded(&self) -> bool {
        self.model.read().is_some()
    }

    /// Get the current model configuration, if loaded.
    pub fn config(&self) -> Option<EngineConfig> {
        self.model.read().as_ref().map(|m| m.config.clone())
    }

    /// Load a model from the specified paths.
    pub fn load(&self, config: EngineConfig) -> Result<()> {
        info!(
            "Loading model from {} with tokenizer {}",
            config.model_path, config.tokenizer_path
        );

        // Validate paths exist
        if !Path::new(&config.model_path).exists() {
            return Err(EngineError::ModelLoad(format!(
                "model file not found: {}",
                config.model_path
            )));
        }
        if !Path::new(&config.tokenizer_path).exists() {
            return Err(EngineError::TokenizerLoad(format!(
                "tokenizer file not found: {}",
                config.tokenizer_path
            )));
        }

        // Load tokenizer
        let tokenizer = Tokenizer::from_file(&config.tokenizer_path)
            .map_err(|e| EngineError::TokenizerLoad(e.to_string()))?;

        // Load ONNX model
        let mut session_builder = Session::builder()
            .map_err(|e| EngineError::ModelLoad(e.to_string()))?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|e| EngineError::ModelLoad(e.to_string()))?;

        if config.num_threads > 0 {
            session_builder = session_builder
                .with_intra_threads(config.num_threads)
                .map_err(|e| EngineError::ModelLoad(e.to_string()))?;
        }

        let session = session_builder
            .commit_from_file(&config.model_path)
            .map_err(|e| EngineError::ModelLoad(e.to_string()))?;

        info!("Model loaded successfully");

        *self.model.write() = Some(LoadedModel {
            session,
            tokenizer,
            config,
        });

        Ok(())
    }

    /// Unload the current model.
    pub fn unload(&self) {
        info!("Unloading model");
        *self.model.write() = None;
    }

    /// Generate embeddings for a batch of texts.
    pub fn embed(&self, texts: &[String]) -> Result<Vec<Vec<f32>>> {
        let model = self.model.read();
        let model = model.as_ref().ok_or(EngineError::ModelNotLoaded)?;

        if texts.is_empty() {
            return Err(EngineError::InvalidInput("empty input".to_string()));
        }

        debug!("Generating embeddings for {} texts", texts.len());

        // Tokenize all texts
        let encodings = model
            .tokenizer
            .encode_batch(texts.to_vec(), true)
            .map_err(|e| EngineError::Tokenization(e.to_string()))?;

        let batch_size = encodings.len();
        let max_len = model.config.max_length;

        // Prepare input tensors
        let mut input_ids: Vec<i64> = Vec::with_capacity(batch_size * max_len);
        let mut attention_mask: Vec<i64> = Vec::with_capacity(batch_size * max_len);

        for encoding in &encodings {
            let ids = encoding.get_ids();
            let mask = encoding.get_attention_mask();

            // Pad or truncate to max_length
            for i in 0..max_len {
                if i < ids.len() {
                    input_ids.push(ids[i] as i64);
                    attention_mask.push(mask[i] as i64);
                } else {
                    input_ids.push(0); // PAD token
                    attention_mask.push(0);
                }
            }
        }

        // Create ndarray views
        let input_ids = Array2::from_shape_vec((batch_size, max_len), input_ids).map_err(|e| {
            EngineError::Inference(format!("failed to create input_ids tensor: {}", e))
        })?;

        let attention_mask = Array2::from_shape_vec((batch_size, max_len), attention_mask)
            .map_err(|e| {
                EngineError::Inference(format!("failed to create attention_mask tensor: {}", e))
            })?;

        // Run inference
        let outputs = model
            .session
            .run(
                ort::inputs! {
                    "input_ids" => input_ids,
                    "attention_mask" => attention_mask,
                }
                .map_err(|e| EngineError::Inference(e.to_string()))?,
            )
            .map_err(|e| EngineError::Inference(e.to_string()))?;

        // Extract embeddings from output
        // Most sentence transformers output shape: [batch_size, hidden_size]
        let embeddings = outputs
            .get("sentence_embedding")
            .or_else(|| outputs.get("last_hidden_state"))
            .ok_or_else(|| EngineError::Inference("no embedding output found".to_string()))?;

        let embeddings: ndarray::ArrayViewD<f32> = embeddings
            .try_extract_tensor()
            .map_err(|e| EngineError::Inference(e.to_string()))?;

        // Convert to Vec<Vec<f32>>
        let mut result = Vec::with_capacity(batch_size);

        // Handle different output shapes
        let shape = embeddings.shape();
        if shape.len() == 2 {
            // [batch_size, hidden_size] - direct sentence embeddings
            for i in 0..batch_size {
                let embedding: Vec<f32> = embeddings
                    .slice(ndarray::s![i, ..])
                    .iter()
                    .copied()
                    .collect();
                let embedding = if model.config.normalize {
                    normalize_vector(&embedding)
                } else {
                    embedding
                };
                result.push(embedding);
            }
        } else if shape.len() == 3 {
            // [batch_size, seq_len, hidden_size] - need mean pooling
            for i in 0..batch_size {
                let tokens: ndarray::ArrayView2<f32> = embeddings
                    .slice(ndarray::s![i, .., ..])
                    .into_dimensionality()
                    .map_err(|e| EngineError::Inference(e.to_string()))?;

                // Mean pooling over sequence dimension
                let embedding = mean_pool(tokens, &attention_mask.slice(ndarray::s![i, ..]));
                let embedding = if model.config.normalize {
                    normalize_vector(&embedding)
                } else {
                    embedding
                };
                result.push(embedding);
            }
        } else {
            return Err(EngineError::Inference(format!(
                "unexpected output shape: {:?}",
                shape
            )));
        }

        Ok(result)
    }

    /// Generate embedding for a single text.
    pub fn embed_one(&self, text: &str) -> Result<Vec<f32>> {
        let results = self.embed(&[text.to_string()])?;
        results
            .into_iter()
            .next()
            .ok_or_else(|| EngineError::Inference("no embedding generated".to_string()))
    }
}

impl Default for InferenceEngine {
    fn default() -> Self {
        Self::new()
    }
}

/// Mean pooling over token embeddings with attention mask.
fn mean_pool(tokens: ndarray::ArrayView2<f32>, attention_mask: &ArrayView1<i64>) -> Vec<f32> {
    let hidden_size = tokens.shape()[1];
    let mut sum = vec![0.0f32; hidden_size];
    let mut count = 0.0f32;

    for (i, mask) in attention_mask.iter().enumerate() {
        if *mask == 1 {
            for (j, val) in tokens.slice(ndarray::s![i, ..]).iter().enumerate() {
                sum[j] += val;
            }
            count += 1.0;
        }
    }

    if count > 0.0 {
        for val in &mut sum {
            *val /= count;
        }
    }

    sum
}

/// L2 normalize a vector.
fn normalize_vector(v: &[f32]) -> Vec<f32> {
    let norm: f32 = v.iter().map(|x| x * x).sum::<f32>().sqrt();
    if norm > 0.0 {
        v.iter().map(|x| x / norm).collect()
    } else {
        v.to_vec()
    }
}

/// Create a thread-safe reference to the engine.
pub fn create_engine() -> Arc<InferenceEngine> {
    Arc::new(InferenceEngine::new())
}
