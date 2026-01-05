# qntx-fuzzy

High-performance fuzzy matching library for QNTX, written in Rust with CGO integration.

## Overview

This library provides advanced fuzzy matching capabilities for QNTX attestation queries, replacing the basic substring matching in the Go implementation with a multi-strategy approach:

| Strategy | Score | Description |
|----------|-------|-------------|
| Exact | 1.0 | Exact case-insensitive match |
| Prefix | 0.9 | Query is prefix of value |
| Word Boundary | 0.85 | Query matches complete word (split on space, _, -) |
| Substring | 0.65-0.75 | Query appears within value |
| Jaro-Winkler | 0.6-0.82 | String similarity > 85% |
| Levenshtein | 0.6-0.8 | Edit distance ≤ 2 |

## Building

```bash
cd plugins/qntx-fuzzy
cargo build --release --lib

# Creates:
#   target/release/libqntx_fuzzy.so   (Linux shared)
#   target/release/libqntx_fuzzy.a    (Linux static)
#   target/release/libqntx_fuzzy.dylib (macOS)
```

## CGO Integration

This library uses CGO for direct integration with Go, providing:
- Direct function calls from Go to Rust (~1-5μs latency)
- Thread-safe concurrent access via RwLock
- No network overhead or separate process
- Memory-safe FFI interface

### Using from Go

```go
import "github.com/teranos/QNTX/plugins/qntx-fuzzy/cgo"

// Create engine (links with Rust library)
engine := cgo.NewFuzzyEngine()
defer engine.Free()

// Build index
result := engine.RebuildIndex(predicates, contexts)
fmt.Printf("Indexed %d predicates in %dms\n",
    result.PredicateCount, result.BuildTimeMs)

// Find matches
matches := engine.FindMatches(
    "author",
    cgo.VocabPredicates,
    20,   // limit
    0.6,  // min score
)

for _, m := range matches.GetMatches() {
    fmt.Printf("  %s (%.2f, %s)\n", m.Value, m.Score, m.Strategy)
}
```

### Integration with AxExecutor

```go
// Create CGO matcher
engine := cgo.NewFuzzyEngine()
defer engine.Free()

matcher := &ax.CGOMatcher{
    Engine: engine,
}

// Use with AxExecutor
executor := ax.NewAxExecutor(ax.AxExecutorOptions{
    Matcher: matcher, // Use Rust implementation
})
```

### Build Requirements

Set the library path correctly:

```bash
# Option 1: Set library path at runtime
export LD_LIBRARY_PATH=$PWD/target/release:$LD_LIBRARY_PATH  # Linux
export DYLD_LIBRARY_PATH=$PWD/target/release:$DYLD_LIBRARY_PATH  # macOS

# Option 2: Build with rustfuzzy tag
go build -tags rustfuzzy ./...
go test -tags rustfuzzy ./ats/ax/...
```

## Performance

Expected improvements over Go implementation:

| Vocabulary Size | Go (substring) | Rust (multi-strategy) | Improvement |
|-----------------|----------------|----------------------|-------------|
| 1K items | ~1ms | ~0.05ms | 20x |
| 10K items | ~10ms | ~0.3ms | 33x |
| 100K items | ~100ms | ~3ms | 33x |

Additional benefits:
- Typo tolerance via Levenshtein distance
- Better ranking via multiple strategies
- Word boundary detection (important for predicates like `is_author_of`)
- Consistent scoring across all match types

## Files

| File | Purpose |
|------|---------|
| `src/engine.rs` | Core fuzzy matching engine |
| `src/ffi.rs` | Rust FFI implementation (C ABI) |
| `src/lib.rs` | Library interface |
| `include/fuzzy_engine.h` | C header for FFI functions |
| `cgo/fuzzy_cgo.go` | Go CGO wrapper |
| `ats/ax/matcher_cgo.go` | Go Matcher interface implementation |

## Testing

```bash
# Rust unit tests
cargo test --lib

# Integration tests (Go + Rust)
make rust-fuzzy-integration

# Run with specific test
go test -tags "integration rustfuzzy" -run TestBooksFuzzyMatching ./plugins/qntx-fuzzy/...
```

## Related

- [GitHub Issue #32](https://github.com/teranos/QNTX/issues/32) - Advanced fuzzy matching system
- [ats/ax/fuzzy.go](../../ats/ax/fuzzy.go) - Current Go implementation
- [ats/ax/matcher.go](../../ats/ax/matcher.go) - Matcher interface