# Phase 2: QNTX Shared Rust Crate - Remaining Work

## ⚠️ CRITICAL TODO: Delete Legacy Types Directory

**The generated Rust types directory must be deleted after updating typegen.**

Current state (transitional):
- typegen outputs to a generated Rust types directory
- flake.nix syncs files into the crate
- Legacy directory excluded from workspace but still exists

To complete:
1. Update the typegen command to output Rust types directly into the crate
2. Update `typegen/rust/index.go` to skip generating `lib.rs`, `Cargo.toml`, `README.md` when outputting to embedded location
3. Update `typegen/check.go` to use new path for Rust types
4. Remove sync step from `flake.nix` generate-types
5. **Delete the generated Rust types directory**
6. Remove from workspace `exclude` list

---

## Completed (Phase 1)

- [x] Cargo workspace at project root
- [x] The crate, with types, plugin scaffolding, error handling, tracing
- [x] [pyre](https://github.com/teranos/pyre) and `qntx-app` migrated to use `qntx` crate
- [x] Type sync from the generated directory into the crate
- [x] `qntx-inference` plugin created

## Phase 2 Tasks

1. **Update typegen to output directly into the crate** - Remove the sync step by having typegen generate types in place, skipping lib.rs/mod.rs generation for the embedded location.

2. **Migrate [pyre](https://github.com/teranos/pyre) to use shared proto definitions** - Replace its local proto compilation with imports from `qntx::plugin::proto`.

3. **Add [pyre](https://github.com/teranos/pyre) integration with `qntx` types** - Use `qntx::types::{Job, JobStatus}` for job lifecycle consistency.

4. **Move workspace profiles to root Cargo.toml** - Consolidate release profiles from individual crates.
