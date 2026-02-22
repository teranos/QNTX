//! CLIP BPE tokenizer wrapper using the `tokenizers` crate

use std::path::Path;

/// CLIP tokenizer for text-to-token conversion
pub struct ClipTokenizer {
    tokenizer: tokenizers::Tokenizer,
    max_length: usize,
}

impl ClipTokenizer {
    /// Load CLIP tokenizer from a tokenizer.json file
    pub fn from_file(path: impl AsRef<Path>, max_length: usize) -> Result<Self, String> {
        let path = path.as_ref();
        let tokenizer = tokenizers::Tokenizer::from_file(path)
            .map_err(|e| format!("Failed to load tokenizer from '{}': {}", path.display(), e))?;

        Ok(Self {
            tokenizer,
            max_length,
        })
    }

    /// Encode text to token IDs with padding/truncation to max_length.
    /// Returns (input_ids, attention_mask) each of shape [1, max_length].
    pub fn encode(&self, text: &str) -> Result<(Vec<i64>, Vec<i64>), String> {
        let encoding = self
            .tokenizer
            .encode(text, true)
            .map_err(|e| format!("Tokenization failed: {}", e))?;

        let ids = encoding.get_ids();

        // Pad or truncate to max_length
        let mut input_ids = vec![0i64; self.max_length];
        let mut attention_mask = vec![0i64; self.max_length];

        let len = ids.len().min(self.max_length);
        for i in 0..len {
            input_ids[i] = ids[i] as i64;
            attention_mask[i] = 1;
        }

        // CLIP uses 49407 as EOS token. If truncated, force EOS at end.
        if ids.len() > self.max_length {
            input_ids[self.max_length - 1] = 49407;
        }

        Ok((input_ids, attention_mask))
    }
}
