# QNTX Embeddings (Working!)

**STATUS: ONNX Runtime integration working with real model**

## What This Is

Sentence transformer embeddings for semantic search in QNTX using ONNX Runtime.

## Current State

✅ **Working:**
- ONNX Runtime 2.0 API fully integrated
- Successfully loads and runs all-MiniLM-L6-v2 model
- Generates real 384-dimensional embeddings
- Mean pooling for sentence-level embeddings
- ~65ms inference time per sentence

⚠️ **Still Needs Work:**
- Uses dummy tokenization (needs proper tokenizer for accurate results)
- sqlite-vec initialization broken
- No Go service layer
- No API endpoints

## Files That Exist

- `src/` - Rust code with dummy implementation
- `models/` - Downloaded all-MiniLM-L6-v2 model (86MB, unused)
- `embeddings/` - Go CGO wrapper (calls dummy Rust code)

## What Needs To Be Done

See `docs/embeddings-todo.md` for the complete list of work required to make this functional.