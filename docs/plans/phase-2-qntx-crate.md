# Phase 2: QNTX Shared Rust Crate - Remaining Work

## Completed (Phase 1)

- [x] Cargo workspace at project root
- [x] `crates/qntx/` with types, plugin scaffolding, error handling, tracing
- [x] `qntx-python-plugin` and `qntx-app` migrated to use `qntx` crate
- [x] Type sync from `types/generated/rust/` to `crates/qntx/src/types/`

## Phase 2 Tasks

1. **Update typegen to output directly to `crates/qntx/src/types/`** - Remove the sync step by having typegen generate types in place, skipping lib.rs/mod.rs generation for the embedded location.

2. **Migrate `qntx-python-plugin` to use shared proto definitions** - Replace its local proto compilation with imports from `qntx::plugin::proto`.

3. **Add `qntx-python-plugin` integration with `qntx` types** - Use `qntx::types::{Job, JobStatus}` for job lifecycle consistency.

4. **Move workspace profiles to root Cargo.toml** - Consolidate release profiles from individual crates.

---

**Next: Local Inference Plugin** - With the shared crate in place, `qntx-inference` can be built using `qntx::plugin` for gRPC scaffolding and `qntx::types` for job/progress reporting.
