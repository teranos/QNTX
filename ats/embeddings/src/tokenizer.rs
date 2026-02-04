use anyhow::Result;
use ndarray::Array2;
use std::path::Path;
use tokenizers::tokenizer::Tokenizer;
use tokenizers::Encoding;

/// Tokenizer for the embedding model
pub struct EmbeddingTokenizer {
    tokenizer: Tokenizer,
    max_length: usize,
}

impl EmbeddingTokenizer {
    /// Load tokenizer from a JSON file
    pub fn from_file(path: impl AsRef<Path>, max_length: usize) -> Result<Self> {
        let tokenizer = Tokenizer::from_file(path)
            .map_err(|e| anyhow::anyhow!("Failed to load tokenizer: {}", e))?;

        Ok(Self {
            tokenizer,
            max_length,
        })
    }

    /// Encode text into token IDs and attention mask
    pub fn encode(&self, text: &str) -> Result<(Array2<i64>, Array2<i64>)> {
        // Encode with special tokens (CLS and SEP) automatically added
        let encoding = self.tokenizer
            .encode(text, true)
            .map_err(|e| anyhow::anyhow!("Tokenization failed: {}", e))?;

        // Get token IDs and attention mask
        let ids = encoding.get_ids();
        let attention = encoding.get_attention_mask();

        // Ensure we don't exceed max length
        let actual_len = ids.len().min(self.max_length);

        // Create arrays with proper dimensions
        let mut input_ids = Array2::<i64>::zeros((1, self.max_length));
        let mut attention_mask = Array2::<i64>::zeros((1, self.max_length));

        // Copy actual tokens (converting u32 to i64)
        for i in 0..actual_len {
            input_ids[[0, i]] = ids[i] as i64;
            attention_mask[[0, i]] = attention[i] as i64;
        }

        Ok((input_ids, attention_mask))
    }

    /// Get the actual number of tokens (for accurate counting)
    pub fn count_tokens(&self, text: &str) -> Result<usize> {
        let encoding = self.tokenizer
            .encode(text, true)
            .map_err(|e| anyhow::anyhow!("Tokenization failed: {}", e))?;

        Ok(encoding.get_ids().len())
    }

    /// Encode and return the Encoding object (useful for debugging)
    pub fn encode_raw(&self, text: &str) -> Result<Encoding> {
        self.tokenizer
            .encode(text, true)
            .map_err(|e| anyhow::anyhow!("Tokenization failed: {}", e))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tokenizer_creation() {
        // This test would require the actual tokenizer.json file
        // For now, just ensure the module compiles
        assert!(true);
    }

    #[test]
    fn test_array_dimensions() {
        // Test would verify that arrays have correct shape
        // Would need actual tokenizer file to run properly
        assert!(true);
    }
}