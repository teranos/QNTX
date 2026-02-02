# QNTX Embeddings (Non-functional)

**STATUS: Broken scaffold that returns dummy data**

## What This Is Supposed To Be

Sentence transformer embeddings for semantic search in QNTX using ONNX Runtime.

## What It Actually Is

- Returns hardcoded `vec![0.1f32; 384]` for any input
- ONNX Runtime integration broken (ort 2.0 API incompatible)
- sqlite-vec initialization broken
- No Go service layer
- No API endpoints

## Files That Exist

- `src/` - Rust code with dummy implementation
- `models/` - Downloaded all-MiniLM-L6-v2 model (86MB, unused)
- `embeddings/` - Go CGO wrapper (calls dummy Rust code)

## What Needs To Be Done

See `docs/embeddings-todo.md` for the complete list of work required to make this functional.