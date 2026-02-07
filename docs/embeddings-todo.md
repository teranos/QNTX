# Sentence Transformers Integration - TODO

## Status
We have the foundation in place but need to complete the integration to make it functional.

## Completed ✅
- Downloaded ONNX model (all-MiniLM-L6-v2) and tokenizer files
- Created Rust embeddings crate structure at `ats/embeddings/`
- Integrated sqlite-vec into Rust SQLite backend (`crates/qntx-sqlite`)
- Added database migration for embeddings table
- Added Makefile target for building embeddings library
- Clarified that Rust SQLite is the primary backend

## High Priority Tasks 🔴

### 1. ~~Fix ONNX Runtime Integration in Rust~~ ✅ DONE
**Location:** `ats/embeddings/src/engine.rs`
- ✅ Fixed ort 2.0 API compatibility issues
- ✅ Code compiles successfully
- ✅ Real tokenization implemented with HuggingFace tokenizers crate
- ✅ Model tested and generating semantically meaningful embeddings

### 2. ~~Complete sqlite-vec Integration~~ ✅ DONE
**Location:** `crates/qntx-sqlite/src/vec.rs`
- ✅ Fixed sqlite-vec initialization using `sqlite3_auto_extension`
- ✅ Must call `init_vec_extension()` BEFORE creating any connections
- ✅ All vector operations working (vec_version returns v0.1.6)
- ✅ Migrations with vec0 tables and FLOAT32_BLOB types working
- ✅ L2 distance functions tested and working

## Medium Priority Tasks 🟡

### 3. Create Go Service Layer
**Location:** Create new file `ats/embeddings/service.go`
- Call Rust embeddings library via CGO
- Handle model initialization
- Provide embed() and embedBatch() methods
- Manage vector serialization for sqlite-vec

### 4. Add Semantic Search API Endpoints
**Location:** TBD - likely in API routes
- POST `/api/embeddings/generate` - Generate embeddings for text
- POST `/api/search/semantic` - Search using vector similarity
- GET `/api/embeddings/:id` - Retrieve specific embedding

## Low Priority Tasks 🟢

### 5. Add Tests
- Test Rust ONNX inference
- Test sqlite-vec vector operations
- Test Go service integration
- Test API endpoints

### 6. Documentation
- Add usage examples
- Document API endpoints
- Add configuration options

## Technical Debt
- ort crate is at 2.0.0-rc.11 (release candidate) - update when stable
- ✅ ~~Consider adding tokenizer support directly in Rust instead of dummy tokenization~~ - DONE: Added tokenizers crate
- Add proper error handling throughout

## Notes
- sqlite-vec is critical - it enables the actual vector similarity search
- Without fixing the ONNX Runtime integration, we can't generate real embeddings
- The Go service layer is the bridge between the Rust implementation and the API