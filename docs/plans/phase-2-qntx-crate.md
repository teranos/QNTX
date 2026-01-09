# Phase 2: QNTX Shared Rust Crate - Remaining Work

## ⚠️ CRITICAL TODO: Delete Legacy Types Directory

**`types/generated/rust/` must be deleted after updating typegen.**

Current state (transitional):
- typegen outputs to `types/generated/rust/`
- flake.nix syncs files to `crates/qntx/src/types/`
- Legacy directory excluded from workspace but still exists

To complete:
1. Update `cmd/qntx/commands/typegen.go` to output Rust types directly to `crates/qntx/src/types/`
2. Update `typegen/rust/index.go` to skip generating `lib.rs`, `Cargo.toml`, `README.md` when outputting to embedded location
3. Update `typegen/check.go` to use new path for Rust types
4. Remove sync step from `flake.nix` generate-types
5. **Delete `types/generated/rust/` directory**
6. Remove from workspace `exclude` list

---

## Completed (Phase 1)

- [x] Cargo workspace at project root
- [x] `crates/qntx/` with types, plugin scaffolding, error handling, tracing
- [x] `qntx-python-plugin` and `qntx-app` migrated to use `qntx` crate
- [x] Type sync from `types/generated/rust/` to `crates/qntx/src/types/`
- [x] `qntx-inference` plugin created

## Phase 2 Tasks

1. **Update typegen to output directly to `crates/qntx/src/types/`** - Remove the sync step by having typegen generate types in place, skipping lib.rs/mod.rs generation for the embedded location.

2. **Migrate `qntx-python-plugin` to use shared proto definitions** - Replace its local proto compilation with imports from `qntx::plugin::proto`.

3. **Add `qntx-python-plugin` integration with `qntx` types** - Use `qntx::types::{Job, JobStatus}` for job lifecycle consistency.

4. **Move workspace profiles to root Cargo.toml** - Consolidate release profiles from individual crates.
