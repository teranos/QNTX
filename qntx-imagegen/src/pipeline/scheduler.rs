//! DDIM scheduler — pure math, no ML dependencies
//!
//! Implements the Denoising Diffusion Implicit Models (DDIM) scheduler
//! for the Stable Diffusion denoising loop.

/// DDIM scheduler state
pub struct DdimScheduler {
    /// Total training timesteps (typically 1000)
    num_train_timesteps: usize,
    /// Cumulative product of alphas (noise schedule)
    alphas_cumprod: Vec<f64>,
    /// Timesteps for the current inference run
    timesteps: Vec<usize>,
}

impl DdimScheduler {
    /// Create a new DDIM scheduler with the SD 1.5 noise schedule.
    ///
    /// Uses a scaled linear beta schedule from beta_start=0.00085 to beta_end=0.012
    /// over 1000 training timesteps.
    pub fn new(num_train_timesteps: usize) -> Self {
        let beta_start: f64 = 0.00085_f64.sqrt();
        let beta_end: f64 = 0.012_f64.sqrt();

        // Scaled linear schedule: betas = linspace(sqrt(beta_start), sqrt(beta_end), T)^2
        let mut betas = Vec::with_capacity(num_train_timesteps);
        for i in 0..num_train_timesteps {
            let t = i as f64 / (num_train_timesteps - 1) as f64;
            let beta_sqrt = beta_start + t * (beta_end - beta_start);
            betas.push(beta_sqrt * beta_sqrt);
        }

        // alphas = 1 - betas
        // alphas_cumprod = cumulative product of alphas
        let mut alphas_cumprod = Vec::with_capacity(num_train_timesteps);
        let mut cumprod = 1.0;
        for beta in &betas {
            cumprod *= 1.0 - beta;
            alphas_cumprod.push(cumprod);
        }

        Self {
            num_train_timesteps,
            alphas_cumprod,
            timesteps: Vec::new(),
        }
    }

    /// Set the number of inference steps and compute the timestep schedule.
    ///
    /// Timesteps are evenly spaced across the training schedule.
    pub fn set_timesteps(&mut self, num_inference_steps: u32) {
        let step_ratio = self.num_train_timesteps / num_inference_steps as usize;
        self.timesteps = (0..num_inference_steps as usize)
            .rev()
            .map(|i| i * step_ratio)
            .collect();
    }

    /// Get the timestep schedule
    pub fn timesteps(&self) -> &[usize] {
        &self.timesteps
    }

    /// Perform a single DDIM step.
    ///
    /// # Arguments
    /// * `noise_pred` - Predicted noise from UNet [N elements]
    /// * `timestep` - Current timestep
    /// * `sample` - Current noisy sample [N elements]
    ///
    /// # Returns
    /// Denoised sample for the next step [N elements]
    pub fn step(
        &self,
        noise_pred: &[f32],
        timestep: usize,
        sample: &[f32],
    ) -> Vec<f32> {
        let alpha_prod_t = self.alphas_cumprod[timestep];

        // Previous timestep (or 0 if this is the last step)
        let timestep_idx = self.timesteps.iter().position(|&t| t == timestep);
        let prev_timestep = match timestep_idx {
            Some(idx) if idx + 1 < self.timesteps.len() => self.timesteps[idx + 1],
            _ => 0,
        };
        let alpha_prod_t_prev = if prev_timestep > 0 {
            self.alphas_cumprod[prev_timestep]
        } else {
            1.0 // Final alpha
        };

        // DDIM formula:
        // pred_x0 = (sample - sqrt(1 - alpha_t) * noise_pred) / sqrt(alpha_t)
        // prev_sample = sqrt(alpha_t_prev) * pred_x0 + sqrt(1 - alpha_t_prev) * noise_pred
        let sqrt_alpha_t = (alpha_prod_t as f32).sqrt();
        let sqrt_one_minus_alpha_t = ((1.0 - alpha_prod_t) as f32).sqrt();
        let sqrt_alpha_t_prev = (alpha_prod_t_prev as f32).sqrt();
        let sqrt_one_minus_alpha_t_prev = ((1.0 - alpha_prod_t_prev) as f32).sqrt();

        let mut result = Vec::with_capacity(sample.len());
        for i in 0..sample.len() {
            // Predict original sample (x0)
            let pred_x0 = (sample[i] - sqrt_one_minus_alpha_t * noise_pred[i]) / sqrt_alpha_t;

            // Compute previous sample
            let prev_sample =
                sqrt_alpha_t_prev * pred_x0 + sqrt_one_minus_alpha_t_prev * noise_pred[i];
            result.push(prev_sample);
        }

        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_scheduler_creation() {
        let scheduler = DdimScheduler::new(1000);
        assert_eq!(scheduler.alphas_cumprod.len(), 1000);
        // First alpha_cumprod should be close to 1
        assert!(scheduler.alphas_cumprod[0] > 0.99);
        // Last should be much smaller
        assert!(scheduler.alphas_cumprod[999] < 0.01);
    }

    #[test]
    fn test_set_timesteps() {
        let mut scheduler = DdimScheduler::new(1000);
        scheduler.set_timesteps(20);
        assert_eq!(scheduler.timesteps().len(), 20);
        // First timestep should be the largest
        assert_eq!(scheduler.timesteps()[0], 950);
        // Last timestep should be 0
        assert_eq!(scheduler.timesteps()[19], 0);
    }

    #[test]
    fn test_step_reduces_noise() {
        let scheduler = DdimScheduler::new(1000);
        let sample = vec![1.0f32; 16];
        let noise = vec![0.5f32; 16];

        // Step at a high timestep should produce finite values
        let result = scheduler.step(&noise, 500, &sample);
        assert_eq!(result.len(), 16);
        for val in &result {
            assert!(val.is_finite());
        }
    }
}
