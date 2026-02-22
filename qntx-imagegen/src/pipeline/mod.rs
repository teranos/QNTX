//! Stable Diffusion 1.5 diffusion pipeline
//!
//! Orchestrates CLIP tokenizer, CLIP text encoder, UNet, DDIM scheduler,
//! and VAE decoder to generate images from text prompts.

mod clip;
mod scheduler;
mod tokenizer;
mod unet;
mod vae;

use crate::config::ImagegenConfig;
use crate::models;
use clip::ClipEncoder;
use scheduler::DdimScheduler;
use tokenizer::ClipTokenizer;
use unet::UNet;
use vae::VaeDecoder;

use ndarray::{Array3, Array4};
use rand::SeedableRng;
use rand_distr::{Distribution, StandardNormal};
use tracing::info;

/// Maximum CLIP sequence length
const MAX_SEQ_LEN: usize = 77;

/// CLIP embedding dimension for SD 1.5
const CLIP_HIDDEN_DIM: usize = 768;

/// Full Stable Diffusion pipeline
pub struct DiffusionPipeline {
    tokenizer: ClipTokenizer,
    text_encoder: ClipEncoder,
    unet: UNet,
    vae_decoder: VaeDecoder,
}

// ONNX sessions are thread-safe behind Mutex
unsafe impl Send for DiffusionPipeline {}
unsafe impl Sync for DiffusionPipeline {}

impl DiffusionPipeline {
    /// Load all pipeline components from the model directory.
    pub fn load(config: &ImagegenConfig) -> Result<Self, String> {
        let models_dir = &config.models_dir;
        let threads = config.num_threads;

        info!("Loading CLIP tokenizer...");
        let tokenizer_path = models::model_path(models_dir, "tokenizer");
        let tokenizer = ClipTokenizer::from_file(&tokenizer_path, MAX_SEQ_LEN)?;

        info!("Loading CLIP text encoder...");
        let text_encoder_path = models::model_path(models_dir, "text_encoder");
        let text_encoder = ClipEncoder::load(&text_encoder_path, threads)?;

        info!("Loading UNet...");
        let unet_path = models::model_path(models_dir, "unet");
        let unet = UNet::load(&unet_path, threads)?;

        info!("Loading VAE decoder...");
        let vae_path = models::model_path(models_dir, "vae_decoder");
        let vae_decoder = VaeDecoder::load(&vae_path, threads)?;

        info!("Pipeline loaded successfully");
        Ok(Self {
            tokenizer,
            text_encoder,
            unet,
            vae_decoder,
        })
    }

    /// Generate an image from a text prompt.
    ///
    /// Returns PNG-encoded image bytes.
    #[allow(clippy::too_many_arguments)]
    pub fn generate(
        &self,
        prompt: &str,
        negative_prompt: &str,
        num_inference_steps: u32,
        guidance_scale: f32,
        seed: u64,
        width: u32,
        height: u32,
    ) -> Result<Vec<u8>, String> {
        let latent_h = (height / 8) as usize;
        let latent_w = (width / 8) as usize;

        // 1. Tokenize prompt and negative prompt
        info!("Tokenizing prompt...");
        let (prompt_ids, _) = self.tokenizer.encode(prompt)?;
        let (neg_ids, _) = self.tokenizer.encode(if negative_prompt.is_empty() {
            ""
        } else {
            negative_prompt
        })?;

        // 2. Text encode — get embeddings for both prompts
        info!("Encoding text...");
        let prompt_embeds = self.text_encoder.encode(&prompt_ids)?;
        let neg_embeds = self.text_encoder.encode(&neg_ids)?;

        // Concatenate [negative, positive] for classifier-free guidance → [2, 77, 768]
        let text_embeddings = concat_embeddings(&neg_embeds, &prompt_embeds)?;

        // 3. Initialize latents with Gaussian noise
        info!("Initializing latents (seed={})...", seed);
        let mut rng = rand::rngs::StdRng::seed_from_u64(seed);
        let latent_size = 4 * latent_h * latent_w;
        let mut latents: Vec<f32> = (0..latent_size)
            .map(|_| StandardNormal.sample(&mut rng))
            .collect();

        // 4. Set up scheduler
        let mut scheduler = DdimScheduler::new(1000);
        scheduler.set_timesteps(num_inference_steps);

        let latent_shape = [1i64, 4, latent_h as i64, latent_w as i64];
        let encoder_shape = [2i64, MAX_SEQ_LEN as i64, CLIP_HIDDEN_DIM as i64];
        let text_embed_flat: Vec<f32> = text_embeddings.iter().cloned().collect();

        // 5. Denoising loop
        let timesteps = scheduler.timesteps().to_vec();
        for (step_idx, &timestep) in timesteps.iter().enumerate() {
            info!(
                "Denoising step {}/{} (timestep={})",
                step_idx + 1,
                num_inference_steps,
                timestep
            );

            // UNet predicts noise for [uncond, cond] batch
            let noise_pred_batch = self.unet.predict_noise(
                &latents,
                &latent_shape,
                timestep as f32,
                &text_embed_flat,
                &encoder_shape,
            )?;

            // Apply classifier-free guidance
            let noise_pred =
                apply_cfg(&noise_pred_batch, guidance_scale, latent_size);

            // Scheduler step
            latents = scheduler.step(&noise_pred, timestep, &latents);
        }

        // 6. VAE decode
        info!("Decoding latents to image...");
        let (pixels, pixel_shape) = self.vae_decoder.decode(&latents, &latent_shape)?;

        // 7. Convert to PNG
        info!("Encoding PNG...");
        let png_data = pixels_to_png(&pixels, &pixel_shape)?;

        info!(
            "Image generated: {}x{}, {} bytes",
            width,
            height,
            png_data.len()
        );
        Ok(png_data)
    }
}

/// Concatenate two [1, 77, 768] embeddings into [2, 77, 768]
fn concat_embeddings(a: &Array3<f32>, b: &Array3<f32>) -> Result<Array3<f32>, String> {
    ndarray::concatenate(ndarray::Axis(0), &[a.view(), b.view()])
        .map_err(|e| format!("Failed to concatenate embeddings: {}", e))
}

/// Apply classifier-free guidance to the batched noise prediction.
/// noise_pred_batch is [2, 4, H, W] where [0] is uncond and [1] is cond.
fn apply_cfg(noise_pred_batch: &Array4<f32>, guidance_scale: f32, latent_size: usize) -> Vec<f32> {
    let flat = noise_pred_batch.as_slice().unwrap_or(&[]);
    let mut result = Vec::with_capacity(latent_size);

    for i in 0..latent_size {
        let uncond = flat[i];
        let cond = flat[latent_size + i];
        result.push(uncond + guidance_scale * (cond - uncond));
    }

    result
}

/// Convert CHW float pixels to PNG bytes
fn pixels_to_png(pixels: &[f32], shape: &[usize]) -> Result<Vec<u8>, String> {
    // shape = [1, 3, H, W]
    if shape.len() != 4 || shape[0] != 1 || shape[1] != 3 {
        return Err(format!("Unexpected pixel shape: {:?}", shape));
    }

    let height = shape[2] as u32;
    let width = shape[3] as u32;
    let hw = (height * width) as usize;

    // Convert CHW to RGB8
    let mut rgb = vec![0u8; hw * 3];
    for i in 0..hw {
        // Clamp to [0, 1] and convert to u8
        let r = ((pixels[i].clamp(-1.0, 1.0) + 1.0) / 2.0 * 255.0) as u8;
        let g = ((pixels[hw + i].clamp(-1.0, 1.0) + 1.0) / 2.0 * 255.0) as u8;
        let b = ((pixels[2 * hw + i].clamp(-1.0, 1.0) + 1.0) / 2.0 * 255.0) as u8;
        rgb[i * 3] = r;
        rgb[i * 3 + 1] = g;
        rgb[i * 3 + 2] = b;
    }

    // Encode as PNG
    let img = image::RgbImage::from_raw(width, height, rgb)
        .ok_or_else(|| "Failed to create image buffer".to_string())?;

    let mut png_data = Vec::new();
    let mut cursor = std::io::Cursor::new(&mut png_data);
    img.write_to(&mut cursor, image::ImageFormat::Png)
        .map_err(|e| format!("PNG encoding failed: {}", e))?;

    Ok(png_data)
}
