//! Plugin configuration types

use std::collections::HashMap;
use std::path::PathBuf;

/// Plugin configuration received during initialization
#[derive(Debug, Clone, Default)]
pub struct PluginConfig {
    /// ATSStore gRPC endpoint
    pub ats_store_endpoint: String,
    /// Queue service gRPC endpoint
    pub queue_endpoint: String,
    /// Auth token for service calls
    pub auth_token: String,
    /// Custom configuration values
    pub config: HashMap<String, String>,
}

/// Image generation configuration parsed from plugin config
#[derive(Debug, Clone)]
pub struct ImagegenConfig {
    /// Path to model directory containing ONNX files
    pub models_dir: PathBuf,
    /// Output directory for generated images
    pub output_dir: PathBuf,
    /// Number of inference steps (default: 20)
    pub num_inference_steps: u32,
    /// Classifier-free guidance scale (default: 7.5)
    pub guidance_scale: f32,
    /// Output image width (default: 512)
    pub image_width: u32,
    /// Output image height (default: 512)
    pub image_height: u32,
    /// Number of ONNX inference threads (default: 4)
    pub num_threads: usize,
}

impl Default for ImagegenConfig {
    fn default() -> Self {
        let home = std::env::var("HOME").unwrap_or_else(|_| ".".to_string());
        Self {
            models_dir: PathBuf::from(format!("{}/.qntx/models/stable-diffusion-v1-5", home)),
            output_dir: PathBuf::from(format!("{}/.qntx/imagegen/output", home)),
            num_inference_steps: 20,
            guidance_scale: 7.5,
            image_width: 512,
            image_height: 512,
            num_threads: 4,
        }
    }
}

impl ImagegenConfig {
    /// Parse from plugin config map
    pub fn from_config_map(config: &HashMap<String, String>) -> Self {
        let mut result = Self::default();

        if let Some(dir) = config.get("models_dir") {
            result.models_dir = expand_home(dir);
        }
        if let Some(dir) = config.get("output_dir") {
            result.output_dir = expand_home(dir);
        }
        if let Some(steps) = config.get("num_inference_steps") {
            if let Ok(n) = steps.parse() {
                result.num_inference_steps = n;
            }
        }
        if let Some(scale) = config.get("guidance_scale") {
            if let Ok(s) = scale.parse() {
                result.guidance_scale = s;
            }
        }
        if let Some(w) = config.get("image_width") {
            if let Ok(n) = w.parse() {
                result.image_width = n;
            }
        }
        if let Some(h) = config.get("image_height") {
            if let Ok(n) = h.parse() {
                result.image_height = n;
            }
        }
        if let Some(t) = config.get("num_threads") {
            if let Ok(n) = t.parse() {
                result.num_threads = n;
            }
        }

        result
    }
}

fn expand_home(path: &str) -> PathBuf {
    if path.starts_with("~/") {
        let home = std::env::var("HOME").unwrap_or_else(|_| ".".to_string());
        PathBuf::from(path.replacen('~', &home, 1))
    } else {
        PathBuf::from(path)
    }
}

/// Build the configuration schema for the imagegen plugin
pub fn build_schema() -> HashMap<String, crate::proto::ConfigFieldSchema> {
    use crate::proto::ConfigFieldSchema;

    let mut fields = HashMap::new();

    fields.insert(
        "models_dir".to_string(),
        ConfigFieldSchema {
            r#type: "string".to_string(),
            description: "Path to directory containing Stable Diffusion ONNX model files"
                .to_string(),
            default_value: "~/.qntx/models/stable-diffusion-v1-5".to_string(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "output_dir".to_string(),
        ConfigFieldSchema {
            r#type: "string".to_string(),
            description: "Directory where generated images are saved".to_string(),
            default_value: "~/.qntx/imagegen/output".to_string(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "num_inference_steps".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Number of denoising steps".to_string(),
            default_value: "20".to_string(),
            required: false,
            min_value: "1".to_string(),
            max_value: "100".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "guidance_scale".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Classifier-free guidance scale".to_string(),
            default_value: "7.5".to_string(),
            required: false,
            min_value: "1.0".to_string(),
            max_value: "30.0".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "image_width".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Output image width in pixels (must be divisible by 8)".to_string(),
            default_value: "512".to_string(),
            required: false,
            min_value: "256".to_string(),
            max_value: "1024".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "image_height".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Output image height in pixels (must be divisible by 8)".to_string(),
            default_value: "512".to_string(),
            required: false,
            min_value: "256".to_string(),
            max_value: "1024".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "num_threads".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Number of threads for ONNX Runtime inference".to_string(),
            default_value: "4".to_string(),
            required: false,
            min_value: "1".to_string(),
            max_value: "32".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields
}
