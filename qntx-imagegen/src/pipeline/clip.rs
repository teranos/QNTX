//! CLIP text encoder — converts token IDs to text embeddings via ONNX

use ndarray::Array3;
use ort::session::builder::GraphOptimizationLevel;
use ort::session::Session;
use ort::value::Value;
use parking_lot::Mutex;
use std::path::Path;

/// CLIP text encoder ONNX session
pub struct ClipEncoder {
    session: Mutex<Session>,
}

impl ClipEncoder {
    /// Load CLIP text encoder from ONNX model file
    pub fn load(model_path: impl AsRef<Path>, num_threads: usize) -> Result<Self, String> {
        let path = model_path.as_ref();
        let session = Session::builder()
            .map_err(|e| format!("Failed to create ONNX session builder: {}", e))?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|e| format!("Failed to set optimization level: {}", e))?
            .with_intra_threads(num_threads)
            .map_err(|e| format!("Failed to set thread count: {}", e))?
            .commit_from_file(path)
            .map_err(|e| format!("Failed to load CLIP model from '{}': {}", path.display(), e))?;

        Ok(Self {
            session: Mutex::new(session),
        })
    }

    /// Encode token IDs to text embeddings.
    ///
    /// # Arguments
    /// * `input_ids` - Token IDs [1, 77]
    ///
    /// # Returns
    /// Text embeddings as [1, 77, 768]
    pub fn encode(&self, input_ids: &[i64]) -> Result<Array3<f32>, String> {
        let seq_len = input_ids.len();
        let shape = vec![1i64, seq_len as i64];

        let input_value = Value::from_array((shape.as_slice(), input_ids.to_vec()))
            .map_err(|e| format!("Failed to create input tensor: {}", e))?;

        let mut session = self.session.lock();
        let outputs = session
            .run(ort::inputs![input_value])
            .map_err(|e| format!("CLIP inference failed: {}", e))?;

        // Output shape: [1, 77, 768]
        let (out_shape, data) = outputs[0]
            .try_extract_tensor::<f32>()
            .map_err(|e| format!("Failed to extract CLIP output tensor: {}", e))?;

        let shape_vec: Vec<usize> = out_shape.iter().map(|&x| x as usize).collect();
        if shape_vec.len() != 3 {
            return Err(format!(
                "Expected 3D CLIP output, got {}D: {:?}",
                shape_vec.len(),
                shape_vec
            ));
        }

        Array3::from_shape_vec((shape_vec[0], shape_vec[1], shape_vec[2]), data.to_vec())
            .map_err(|e| format!("Failed to reshape CLIP output: {}", e))
    }
}
