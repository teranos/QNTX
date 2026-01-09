# Session Handover: Shared Rust Crate & Inference Plugin

**Date**: 2026-01-08
**Branch**: `claude/plan-shared-rust-crate-T7RwQ`

---

## Summary

Created a shared `qntx` Rust crate and a new `qntx-inference` plugin for local embedding generation. All Rust projects now use a unified Cargo workspace.

---

## Changes Made

### 1. Cargo Workspace (`Cargo.toml` at root)

```toml
[workspace]
members = [
    "crates/qntx",
    "crates/qntx-inference",
    "qntx-python",
    "web/src-tauri",
]
exclude = [
    "ats/ax/fuzzy-ax",      # CGO library for Go
    "types/generated/rust", # Legacy, superseded by crates/qntx/src/types/
]
```

Shared dependencies defined in `[workspace.dependencies]` for consistency.

### 2. Shared Crate (`crates/qntx/`)

```
crates/qntx/
├── Cargo.toml
├── build.rs              # Proto compilation (feature-gated)
└── src/
    ├── lib.rs
    ├── error.rs          # Common error types (thiserror)
    ├── tracing.rs        # Logging with segment prefixes (꩜, ✿, ❀)
    ├── types/            # Generated from Go source
    │   ├── mod.rs
    │   ├── async.rs      # Job, JobStatus, Progress
    │   ├── budget.rs
    │   ├── schedule.rs
    │   ├── server.rs
    │   ├── sym.rs        # QNTX symbols
    │   └── types.rs
    └── plugin/           # gRPC plugin infrastructure
        ├── mod.rs
        ├── server.rs     # PluginServer builder
        └── shutdown.rs   # Graceful shutdown signal handling
```

**Features**:
- `types` (default) - Just the generated types
- `plugin` - Full gRPC plugin infrastructure (requires protoc)

### 3. Inference Plugin (`crates/qntx-inference/`)

```
crates/qntx-inference/
├── Cargo.toml
├── README.md             # Documentation on ONNX and usage
└── src/
    ├── main.rs           # CLI entry point
    ├── lib.rs
    ├── engine.rs         # ONNX inference engine
    └── service.rs        # gRPC DomainPluginService
```

**Dependencies**:
- `ort` (ONNX Runtime) v2.0.0-rc.9
- `tokenizers` v0.20 (HuggingFace)
- `qntx` with `plugin` feature

**Endpoints**:
- `POST /embed` - Generate embeddings
- `POST /v1/embeddings` - OpenAI-compatible
- `GET /health` - Health check

### 4. Migrated Existing Crates

**`qntx-python-plugin`**:
- Now depends on `qntx` crate
- Uses workspace dependencies
- Nix build updated to use workspace `Cargo.lock`

**`qntx-app` (Tauri)**:
- Now uses `qntx::types` instead of `qntx-types`
- Import changed: `use qntx::types::{sym, Job, JobStatus, ...}`

### 5. Type Generation

**Current flow** (transitional):
1. `typegen --lang rust` outputs to `types/generated/rust/`
2. `flake.nix` syncs `.rs` files to `crates/qntx/src/types/`
3. Excludes `lib.rs` and `mod.rs` (we have custom ones)
4. Fixes `server.rs` import (`crate::` → `super::`)

**TODO**: Update typegen to output directly to `crates/qntx/src/types/`, then delete `types/generated/rust/`.

---

## Files Modified

| File | Change |
|------|--------|
| `Cargo.toml` | NEW - Workspace root |
| `Cargo.lock` | Updated for workspace |
| `crates/qntx/*` | NEW - Shared crate |
| `crates/qntx-inference/*` | NEW - Inference plugin |
| `qntx-python/Cargo.toml` | Uses workspace deps |
| `qntx-python/Cargo.lock` | DELETED - Uses workspace |
| `web/src-tauri/Cargo.toml` | Uses workspace deps |
| `web/src-tauri/src/main.rs` | Updated imports |
| `flake.nix` | Updated for workspace, added type sync |
| `docs/plans/phase-2-qntx-crate.md` | Phase 2 TODO list |

---

## Testing Needed

### Local (requires Nix)

```bash
# Build qntx crate (types only)
cargo build -p qntx

# Build qntx crate with plugin feature (requires protoc)
nix develop
cargo build -p qntx --features plugin

# Build inference plugin
cargo build -p qntx-inference

# Build python plugin
nix build .#qntx-python

# Run all workspace tests
cargo test --workspace
```

### CI Checks

- [ ] `cargo fmt --check --all` - Formatting
- [ ] `cargo clippy --workspace` - Lints
- [ ] Nix builds for qntx-python-plugin
- [ ] Tauri build (desktop)

### Manual Testing

- [ ] Start qntx-inference with a model, call `/embed`
- [ ] Verify qntx-app still works with new type imports
- [ ] Run `make types` and verify sync works

---

## Known Issues

1. **Proto compilation requires Nix** - `cargo build -p qntx --features plugin` needs `protoc`
2. **Legacy types directory** - `types/generated/rust/` still exists, excluded from workspace
3. **ONNX Runtime version** - Using `2.0.0-rc.9` (prerelease)

---

## UI Suggestions for Inference Plugin

### Settings Panel (≡ am → Plugins → Inference)

```
┌─────────────────────────────────────────────────────┐
│ Local Inference                              [ON/OFF]│
├─────────────────────────────────────────────────────┤
│                                                     │
│ Model                                               │
│ ┌─────────────────────────────────────────────────┐ │
│ │ ~/.qntx/models/minilm/model.onnx          [...] │ │
│ └─────────────────────────────────────────────────┘ │
│                                                     │
│ Tokenizer                                           │
│ ┌─────────────────────────────────────────────────┐ │
│ │ ~/.qntx/models/minilm/tokenizer.json      [...] │ │
│ └─────────────────────────────────────────────────┘ │
│                                                     │
│ ─────────────── Advanced ───────────────           │
│                                                     │
│ Max Sequence Length    [512        ]               │
│ Normalize Embeddings   [✓]                         │
│ Inference Threads      [0 (auto)   ]               │
│                                                     │
│ ─────────────── Status ────────────────            │
│                                                     │
│ ● Model loaded (384 dimensions)                    │
│   all-MiniLM-L6-v2                                 │
│                                                     │
│ [Download Model...] [Test Embedding]               │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### Model Browser (future)

```
┌─────────────────────────────────────────────────────┐
│ Available Models                           [Search] │
├─────────────────────────────────────────────────────┤
│                                                     │
│ ★ Recommended                                       │
│ ┌─────────────────────────────────────────────────┐ │
│ │ all-MiniLM-L6-v2                                │ │
│ │ 384 dims · 23MB · Fast                 [Install]│ │
│ └─────────────────────────────────────────────────┘ │
│ ┌─────────────────────────────────────────────────┐ │
│ │ bge-small-en-v1.5                               │ │
│ │ 384 dims · 45MB · Retrieval-focused    [Install]│ │
│ └─────────────────────────────────────────────────┘ │
│                                                     │
│ ○ Installed                                         │
│ ┌─────────────────────────────────────────────────┐ │
│ │ ✓ all-MiniLM-L6-v2              [Use] [Delete] │ │
│ └─────────────────────────────────────────────────┘ │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### Integration with ⋈ ax (semantic search)

```
⋈ ax "people who worked at tech companies"
     ├─ 🔍 Fuzzy: "tech companies" → 3 matches
     └─ 🧠 Semantic: embedding similarity → 12 matches

Results (sorted by relevance):
  0.94  Alice (as software_engineer at Google)
  0.91  Bob (as product_manager at Meta)
  0.87  Charlie (as founder at TechStartup)
  ...
```

---

## Next Steps (Phase 2)

See `docs/plans/phase-2-qntx-crate.md`:

1. **Update typegen** to output directly to `crates/qntx/src/types/`
2. **Delete** `types/generated/rust/` directory
3. **Migrate qntx-python-plugin** to use `qntx::plugin::proto`
4. **Add integration** with ax for semantic search

---

## Commits in This Session

```
2b082c2 Add TODO for deleting legacy types/generated/rust directory
f5f8faa Exclude types/generated/rust from workspace
28d7639 Use workspace Cargo.lock for qntx-python Nix build
2af4a14 Fix Rust formatting and exclude fuzzy-ax from workspace
733766d Add documentation for qntx-inference plugin
38da168 Add qntx-inference plugin for local embedding generation
4e8c1f5 Add shared qntx Rust crate with workspace configuration
```
