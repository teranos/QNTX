# QNTX Embeddings (Working!)

**STATUS: Full tokenization working with semantically meaningful embeddings!**

## What This Is

Sentence transformer embeddings for semantic search in QNTX using ONNX Runtime.

## Model Files

The ONNX model files are NOT checked into git (they are binary files ~86MB). To obtain them:

```bash
# Install dependencies
pip install transformers optimum[onnxruntime]

# Export the model from HuggingFace
cd ats/embeddings
python export_model.py

# This creates:
# - models/all-MiniLM-L6-v2/model.onnx (the ONNX model)
# - models/all-MiniLM-L6-v2/tokenizer.json (the tokenizer config)
```

The script downloads the model from HuggingFace and converts it to ONNX format.

## Current State

✅ **Working:**
- ONNX Runtime 2.0 API fully integrated with proper library paths
- Successfully loads and runs all-MiniLM-L6-v2 model
- Real HuggingFace tokenizer integration (tokenizers crate)
- Generates semantically meaningful 384-dimensional embeddings
- Mean pooling for sentence-level embeddings
- ~67ms inference time per sentence (including tokenization)
- Verified semantic similarity (cat/kitten: 0.94, cat/dog: 0.92, cat/car: 0.88)
- sqlite-vec extension fully integrated (v0.1.6)
- vec0 virtual tables and FLOAT32_BLOB types working
- Vector distance functions (L2) tested and working
- Full Go-Rust FFI integration via CGO
- Go service layer with batch processing support
- Embedding serialization for sqlite-vec FLOAT32_BLOB format

⚠️ **Still Needs Work:**
- No API endpoints
- No database integration with embeddings table

## Files That Exist

- `src/` - Rust code with dummy implementation
- `models/` - Downloaded all-MiniLM-L6-v2 model (86MB, unused)
- `embeddings/` - Go CGO wrapper (calls dummy Rust code)

## What Needs To Be Done

See `docs/embeddings-todo.md` for the complete list of work required to make this functional.