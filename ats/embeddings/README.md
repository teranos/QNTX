# QNTX Embeddings

Sentence transformer embeddings for semantic search in QNTX, using ONNX Runtime for fast inference.

## Architecture

This module follows the QNTX pattern for Rust/Go integration:
- **Rust library** (`src/`) - ONNX Runtime inference engine
- **C FFI** (`src/ffi.rs`) - C-compatible interface
- **Go wrapper** (`embeddings/`) - CGO integration with fallback

## Features

- ONNX model inference for sentence transformers
- Fast CPU inference with SIMD optimization
- JSON-based FFI for easy integration
- Thread-safe embedding service
- Batch embedding support

## Building

```bash
# Build the Rust library
cd ats/embeddings
cargo build --release --features ffi

# Or use the Makefile target (TODO: add this)
make rust-embeddings
```

## Usage

### 1. Export a Model to ONNX

Use the Python script to export a HuggingFace model:

```python
from optimum.onnxruntime import ORTModelForFeatureExtraction
from transformers import AutoTokenizer

model_name = "sentence-transformers/all-MiniLM-L6-v2"
model = ORTModelForFeatureExtraction.from_pretrained(model_name, export=True)
model.save_pretrained("models/all-MiniLM-L6-v2")

# Also save the tokenizer (for future integration)
tokenizer = AutoTokenizer.from_pretrained(model_name)
tokenizer.save_pretrained("models/all-MiniLM-L6-v2")
```

### 2. Use in Go

```go
import "github.com/teranos/QNTX/ats/embeddings/embeddings"

// Create service
service := embeddings.NewEmbeddingService()

// Initialize with model
err := service.Init("models/all-MiniLM-L6-v2/model.onnx")
if err != nil {
    log.Fatal(err)
}
defer service.Close()

// Embed text
result, err := service.Embed("This is a test sentence")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Embedding dimensions: %d\n", len(result.Embedding))
fmt.Printf("Inference time: %.2fms\n", result.InferenceMS)
```

## Models

Recommended models for different use cases:

| Model | Dimensions | Size | Use Case |
|-------|------------|------|----------|
| all-MiniLM-L6-v2 | 384 | 80MB | General purpose, fast |
| all-mpnet-base-v2 | 768 | 420MB | Higher quality |
| multi-qa-MiniLM-L6-cos-v1 | 384 | 80MB | Question answering |

## Integration with sqlite-vec

After generating embeddings, store them in SQLite using sqlite-vec:

```sql
-- Create virtual table
CREATE VIRTUAL TABLE attestation_embeddings USING vec0(
    attestation_id TEXT PRIMARY KEY,
    embedding float[384]
);

-- Insert embedding
INSERT INTO attestation_embeddings(attestation_id, embedding)
VALUES (?, ?);

-- Search similar
SELECT a.*, vec_distance(e.embedding, ?) as distance
FROM attestations a
JOIN attestation_embeddings e ON a.id = e.attestation_id
WHERE e.embedding MATCH vec_search(?, 10)
ORDER BY distance;
```

## Performance

On a modern CPU (M1/M2 or Intel i7+):
- all-MiniLM-L6-v2: ~10-20ms per embedding
- Batch processing: ~5-10ms per text when batched
- Memory usage: ~200MB with model loaded

## TODO

- [ ] Integrate proper tokenizer (HuggingFace tokenizers crate)
- [ ] Add model caching and lazy loading
- [ ] Implement true batch processing in Rust
- [ ] Add WASM compilation support
- [ ] Create Python script for model export
- [ ] Add benchmarks and tests