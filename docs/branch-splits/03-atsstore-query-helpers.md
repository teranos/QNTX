# Branch Split: AtsStore Query Helpers

**Source Branch:** `feature/dynamic-ix-routing`
**Target Branch:** `feature/atsstore-query-helpers`
**Priority:** LOW (nice-to-have, only if generally useful)
**Status:** Complete but narrow use case

## Overview

Extract convenience query methods for AtsStore in Rust Python plugin. Adds `query_by_predicate()` and `query_by_predicate_context()` helpers to simplify attestation queries.

## Problem Solved

Current `AtsStoreClient` requires verbose setup for common query patterns. These helpers provide ergonomic shortcuts for predicate-based queries, commonly used when looking up handlers or capabilities.

## Files to Extract

1. **`qntx-python/src/atsstore.rs`**
   - Add `query_by_predicate()` method
   - Add `query_by_predicate_context()` method
   - Add internal `query_attestations()` helper
   - Add `attributes_json` field to `AttestationResult` struct

## Code Changes

### New Methods

```rust
/// Query attestations by predicate and context
pub fn query_by_predicate_context(
    &mut self,
    predicate: &str,
    context: &str,
) -> Result<Vec<AttestationResult>, String>

/// Query attestations by predicate only
pub fn query_by_predicate(
    &mut self,
    predicate: &str,
) -> Result<Vec<AttestationResult>, String>

/// Internal helper for queries
fn query_attestations(
    &mut self,
    subjects: Vec<String>,
    predicates: Vec<String>,
    contexts: Vec<String>,
    limit: i32,
) -> Result<Vec<AttestationResult>, String>
```

### Updated Struct

```rust
pub struct AttestationResult {
    pub id: String,
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    pub timestamp: i64,
    pub source: String,
    pub attributes_json: String, // NEW: JSON-encoded attributes
}
```

## Extraction Steps

```bash
# 1. Create new branch from main
git checkout main
git pull
git checkout -b feature/atsstore-query-helpers

# 2. Extract changes from atsstore.rs
git checkout feature/dynamic-ix-routing -- qntx-python/src/atsstore.rs

# 3. Verify Rust builds
make rust-python-install

# 4. Run tests (if any exist for atsstore)
cd qntx-python && cargo test

# 5. Commit
git add qntx-python/src/atsstore.rs
git commit -m "Add AtsStore query convenience methods

Add query_by_predicate() and query_by_predicate_context() helpers
for common attestation lookup patterns. Include attributes_json in
AttestationResult for accessing attestation metadata."

# 6. Create PR
gh pr create --base main --title "Add AtsStore query helpers" --body "..."
```

## Usage Examples

### Before (verbose)

```rust
let channel = self.connect()?;
let filter = AttestationFilter {
    subjects: vec![],
    predicates: vec!["ix_handler".to_string()],
    contexts: vec!["webhook".to_string()],
    actors: vec![],
    time_start: 0,
    time_end: 0,
    limit: 0,
};
let request = GetAttestationsRequest {
    auth_token: self.config.auth_token.clone(),
    filter: Some(filter),
};
// ... spawn thread, create runtime, make call ...
```

### After (concise)

```rust
let results = ats_client.query_by_predicate_context("ix_handler", "webhook")?;
```

## Testing Checklist

- [ ] Rust builds: `make rust-python-install`
- [ ] No existing functionality broken
- [ ] (Optional) Add unit tests for new methods

## Dependencies

**None** - Pure additive change to existing `AtsStoreClient`

## Decision Point

**Should we extract this?**

**YES if:**
- You anticipate multiple features querying attestations by predicate
- Code readability and ergonomics are priorities
- You want to establish a pattern for common query shortcuts

**NO if:**
- This is only used by dynamic IX routing (keep bundled)
- You prefer explicit, verbose query construction everywhere
- The 3 new methods don't justify a separate PR

**Recommendation:** Only extract if you see 2+ other use cases for these helpers. Otherwise, keep bundled with the feature that uses them.

## Risk Assessment

**VERY LOW**
- Purely additive (no changes to existing methods)
- No database schema changes
- Simple wrapper around existing `query_attestations` functionality
- No Python-visible API changes (unless you expose these to Python later)

## Alternative Approach

Instead of a separate branch, consider:
1. **Bundle with dynamic IX routing** - These helpers are only used there currently
2. **Extract later** - Wait until a second feature needs predicate queries, then refactor
3. **Skip entirely** - Verbose query construction is explicit and clear

## Commit Message

```
Add AtsStore query convenience methods

Add query_by_predicate() and query_by_predicate_context() helpers
for common attestation lookup patterns. Include attributes_json in
AttestationResult for accessing attestation metadata.
```
