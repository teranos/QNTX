//! VAE decoder — converts latent representation to RGB pixels via ONNX

use ort::session::builder::GraphOptimizationLevel;
use ort::session::Session;
use ort::value::Value;
use parking_lot::Mutex;
use std::path::Path;

/// VAE decoder ONNX session
pub struct VaeDecoder {
    session: Mutex<Session>,
}

impl VaeDecoder {
    /// Load VAE decoder from ONNX model file
    pub fn load(model_path: impl AsRef<Path>, num_threads: usize) -> Result<Self, String> {
        let path = model_path.as_ref();
        let session = Session::builder()
            .map_err(|e| format!("Failed to create ONNX session builder: {}", e))?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|e| format!("Failed to set optimization level: {}", e))?
            .with_intra_threads(num_threads)
            .map_err(|e| format!("Failed to set thread count: {}", e))?
            .commit_from_file(path)
            .map_err(|e| {
                format!(
                    "Failed to load VAE decoder model from '{}': {}",
                    path.display(),
                    e
                )
            })?;

        Ok(Self {
            session: Mutex::new(session),
        })
    }

    /// Decode latent to RGB image data.
    ///
    /// # Arguments
    /// * `latent` - Latent representation [1, 4, H/8, W/8]
    /// * `latent_shape` - Shape of the latent tensor
    ///
    /// # Returns
    /// RGB pixel data [1, 3, H, W] as flat f32 vector, plus output shape
    pub fn decode(
        &self,
        latent: &[f32],
        latent_shape: &[i64],
    ) -> Result<(Vec<f32>, Vec<usize>), String> {
        // Scale latent before decoding (SD uses 1/0.18215 scaling)
        let scaled: Vec<f32> = latent.iter().map(|&x| x / 0.18215).collect();

        let input = Value::from_array((latent_shape, scaled))
            .map_err(|e| format!("Failed to create VAE input tensor: {}", e))?;

        let mut session = self.session.lock();

        // SD 1.5 ONNX VAE decoder expects input named "latent_sample"
        let outputs = session
            .run(ort::inputs!["latent_sample" => input])
            .map_err(|e| format!("VAE decode failed: {}", e))?;

        let (out_shape, data) = outputs[0]
            .try_extract_tensor::<f32>()
            .map_err(|e| format!("Failed to extract VAE output tensor: {}", e))?;

        let shape_vec: Vec<usize> = out_shape.iter().map(|&x| x as usize).collect();
        if shape_vec.len() != 4 || shape_vec[1] != 3 {
            return Err(format!(
                "Expected VAE output [1, 3, H, W], got {:?}",
                shape_vec
            ));
        }

        Ok((data.to_vec(), shape_vec))
    }
}
