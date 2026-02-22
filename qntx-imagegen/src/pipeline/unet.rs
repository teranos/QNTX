//! UNet denoising model — single DDIM step via ONNX

use ndarray::Array4;
use ort::session::builder::GraphOptimizationLevel;
use ort::session::Session;
use ort::value::Value;
use parking_lot::Mutex;
use std::path::Path;

/// UNet ONNX session for denoising
pub struct UNet {
    session: Mutex<Session>,
}

impl UNet {
    /// Load UNet from ONNX model file
    pub fn load(model_path: impl AsRef<Path>, num_threads: usize) -> Result<Self, String> {
        let path = model_path.as_ref();
        let session = Session::builder()
            .map_err(|e| format!("Failed to create ONNX session builder: {}", e))?
            .with_optimization_level(GraphOptimizationLevel::Level3)
            .map_err(|e| format!("Failed to set optimization level: {}", e))?
            .with_intra_threads(num_threads)
            .map_err(|e| format!("Failed to set thread count: {}", e))?
            .commit_from_file(path)
            .map_err(|e| format!("Failed to load UNet model from '{}': {}", path.display(), e))?;

        Ok(Self {
            session: Mutex::new(session),
        })
    }

    /// Run a single denoising step.
    ///
    /// # Arguments
    /// * `latent` - Noisy latent [1, 4, H/8, W/8]
    /// * `timestep` - Current timestep (scalar)
    /// * `encoder_hidden_states` - Text embeddings [2, 77, 768] (for classifier-free guidance)
    ///
    /// # Returns
    /// Noise prediction [1, 4, H/8, W/8]
    pub fn predict_noise(
        &self,
        latent: &[f32],
        latent_shape: &[i64],
        timestep: f32,
        encoder_hidden_states: &[f32],
        encoder_shape: &[i64],
    ) -> Result<Array4<f32>, String> {
        // Duplicate latent for classifier-free guidance: [2, 4, H/8, W/8]
        let batch_latent: Vec<f32> = [latent, latent].concat();
        let batch_shape = vec![2, latent_shape[1], latent_shape[2], latent_shape[3]];

        let sample = Value::from_array((batch_shape.as_slice(), batch_latent))
            .map_err(|e| format!("Failed to create latent tensor: {}", e))?;

        // Timestep as [1] tensor
        let timestep_value = Value::from_array(([1i64].as_slice(), vec![timestep as i64]))
            .map_err(|e| format!("Failed to create timestep tensor: {}", e))?;

        let encoder_hs =
            Value::from_array((encoder_shape, encoder_hidden_states.to_vec()))
                .map_err(|e| format!("Failed to create encoder hidden states tensor: {}", e))?;

        let mut session = self.session.lock();

        // SD 1.5 ONNX expects inputs: sample, timestep, encoder_hidden_states
        let outputs = session
            .run(ort::inputs![
                "sample" => sample,
                "timestep" => timestep_value,
                "encoder_hidden_states" => encoder_hs
            ])
            .map_err(|e| format!("UNet inference failed: {}", e))?;

        let (out_shape, data) = outputs[0]
            .try_extract_tensor::<f32>()
            .map_err(|e| format!("Failed to extract UNet output tensor: {}", e))?;

        let shape_vec: Vec<usize> = out_shape.iter().map(|&x| x as usize).collect();
        if shape_vec.len() != 4 || shape_vec[0] != 2 {
            return Err(format!(
                "Expected UNet output [2, 4, H, W], got {:?}",
                shape_vec
            ));
        }

        // Apply classifier-free guidance: noise_pred = uncond + guidance_scale * (cond - uncond)
        // This is done by the caller — return the full [2, ...] output
        Array4::from_shape_vec(
            (shape_vec[0], shape_vec[1], shape_vec[2], shape_vec[3]),
            data.to_vec(),
        )
        .map_err(|e| format!("Failed to reshape UNet output: {}", e))
    }
}
