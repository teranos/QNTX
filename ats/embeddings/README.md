# QNTX Embeddings (Working!)

**STATUS: Full tokenization working with semantically meaningful embeddings!**

## What This Is

Sentence transformer embeddings for semantic search in QNTX using ONNX Runtime.

## Current State

✅ **Working:**
- ONNX Runtime 2.0 API fully integrated
- Successfully loads and runs all-MiniLM-L6-v2 model
- Real HuggingFace tokenizer integration (tokenizers crate)
- Generates semantically meaningful 384-dimensional embeddings
- Mean pooling for sentence-level embeddings
- ~78ms inference time per sentence (including tokenization)
- Verified semantic similarity (cat/kitten: 0.94, cat/car: 0.87)
- sqlite-vec extension fully integrated (v0.1.6)
- vec0 virtual tables and FLOAT32_BLOB types working
- Vector distance functions (L2) tested and working

⚠️ **Still Needs Work:**
- No Go service layer
- No API endpoints

## Files That Exist

- `src/` - Rust code with dummy implementation
- `models/` - Downloaded all-MiniLM-L6-v2 model (86MB, unused)
- `embeddings/` - Go CGO wrapper (calls dummy Rust code)

## What Needs To Be Done

See `docs/embeddings-todo.md` for the complete list of work required to make this functional.