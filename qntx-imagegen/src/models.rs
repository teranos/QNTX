//! Model file verification

use std::path::{Path, PathBuf};

/// Expected model files within the models directory
const EXPECTED_FILES: &[&str] = &[
    "text_encoder/model.onnx",
    "unet/model.onnx",
    "vae_decoder/model.onnx",
    "tokenizer/tokenizer.json",
];

/// Result of checking model file presence
#[derive(Debug, serde::Serialize)]
pub struct ModelCheckResult {
    pub models_dir: String,
    pub all_present: bool,
    pub files: Vec<ModelFileStatus>,
}

#[derive(Debug, serde::Serialize)]
pub struct ModelFileStatus {
    pub path: String,
    pub present: bool,
    pub size_bytes: Option<u64>,
}

/// Check if all required model files are present in the models directory
pub fn check_models(models_dir: &Path) -> ModelCheckResult {
    let mut files = Vec::new();
    let mut all_present = true;

    for &relative_path in EXPECTED_FILES {
        let full_path = models_dir.join(relative_path);
        let present = full_path.exists();
        let size_bytes = if present {
            std::fs::metadata(&full_path).ok().map(|m| m.len())
        } else {
            None
        };

        if !present {
            all_present = false;
        }

        files.push(ModelFileStatus {
            path: relative_path.to_string(),
            present,
            size_bytes,
        });
    }

    ModelCheckResult {
        models_dir: models_dir.display().to_string(),
        all_present,
        files,
    }
}

/// Get the path to a specific model file
pub fn model_path(models_dir: &Path, component: &str) -> PathBuf {
    match component {
        "text_encoder" => models_dir.join("text_encoder/model.onnx"),
        "unet" => models_dir.join("unet/model.onnx"),
        "vae_decoder" => models_dir.join("vae_decoder/model.onnx"),
        "tokenizer" => models_dir.join("tokenizer/tokenizer.json"),
        _ => models_dir.join(component),
    }
}
