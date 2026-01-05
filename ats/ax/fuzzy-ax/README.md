# fuzzy-ax

High-performance fuzzy matching optimization for the ax (⋈) segment, written in Rust with CGO integration.

## Overview

This is **not a plugin** - it's a performance-critical component compiled directly into QNTX when the Rust toolchain is available. It provides advanced fuzzy matching for attestation queries, replacing the basic substring matching in the Go implementation with a multi-strategy approach:

| Strategy | Score | Description |
|----------|-------|-------------|
| Exact | 1.0 | Exact case-insensitive match |
| Prefix | 0.9 | Query is prefix of value |
| Word Boundary | 0.85 | Query matches complete word (split on space, _, -) |
| Substring | 0.65-0.75 | Query appears within value |
| Jaro-Winkler | 0.6-0.82 | String similarity > 85% |
| Levenshtein | 0.6-0.8 | Edit distance ≤ 2 |

## Architecture

**CGO-based internal optimization:**
- Direct function calls from Go to Rust (~1-5μs latency)
- Thread-safe concurrent access via RwLock
- No network overhead, no separate process
- Memory-safe FFI interface
- Compiled into QNTX binary with `-tags rustfuzzy`
- **Automatic fallback**: Without the `rustfuzzy` build tag, QNTX uses a Go-based substring matcher
  - No build errors, just reduced fuzzy match quality
  - Web UI shows striped pattern on ax button to indicate fallback mode

## Building

```bash
# From project root
make rust-fuzzy

# Or directly
cd ats/ax/fuzzy-ax
cargo build --release --lib

# Creates:
#   target/release/libqntx_fuzzy.so   (Linux shared)
#   target/release/libqntx_fuzzy.a    (Linux static)
#   target/release/libqntx_fuzzy.dylib (macOS)
```

## Integration with ax Segment

The fuzzy matcher implements the `Matcher` interface defined in `ats/ax/matcher.go`:

```go
import (
    "github.com/teranos/QNTX/ats/ax"
    "github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
)

// Create Rust-backed matcher
matcher, err := ax.NewCGOMatcher()
if err != nil {
    // Falls back to Go implementation
    matcher = ax.NewGoMatcher()
}
defer matcher.Close()

// Use with AxExecutor
executor := ax.NewAxExecutor(ax.AxExecutorOptions{
    Matcher: matcher,
})
```

### Auto-Detection

When building with CGO enabled and the Rust library available, QNTX automatically uses the optimized implementation:

```bash
# Build with Rust optimization (if available)
make rust-fuzzy
go build -tags rustfuzzy ./...

# Build without (uses Go fallback)
go build ./...
```

### Build Requirements

```bash
# Set library path for runtime linking
export LD_LIBRARY_PATH=$PWD/ats/ax/fuzzy-ax/target/release:$LD_LIBRARY_PATH  # Linux
export DYLD_LIBRARY_PATH=$PWD/ats/ax/fuzzy-ax/target/release:$DYLD_LIBRARY_PATH  # macOS

# Build with rustfuzzy tag
go build -tags rustfuzzy ./...
go test -tags rustfuzzy ./ats/ax/...
```

## Performance

The Rust implementation provides several advantages over the Go fallback:

- **Typo tolerance**: Levenshtein distance for character-level edits
- **Better ranking**: Multiple strategies (exact, prefix, substring, Jaro-Winkler, Levenshtein)
- **Word boundary detection**: Critical for predicates like `is_author_of`
- **Consistent scoring**: Normalized 0.0-1.0 scores across all match types

Formal benchmarks are tracked in [issue #XXX](https://github.com/teranos/QNTX/issues/XXX).

## Files

| File | Purpose |
|------|---------|
| `src/engine.rs` | Core fuzzy matching engine |
| `src/ffi.rs` | Rust FFI implementation (C ABI) |
| `src/lib.rs` | Library interface |
| `include/fuzzy_engine.h` | C header for FFI functions |
| `fuzzyax/fuzzy_cgo.go` | Go CGO wrapper (package fuzzyax) |
| `../matcher_cgo.go` | CGOMatcher implementation |
| `../matcher.go` | Matcher interface |
| `../fuzzy.go` | Go fallback implementation |

## Testing

```bash
# Rust unit tests
make rust-fuzzy-test

# Integration tests (Go + Rust)
make rust-fuzzy-integration

# Run specific test
go test -tags "integration rustfuzzy" -run TestBooksFuzzyMatching ./ats/ax/fuzzy-ax/...
```

## Related

- [GitHub Issue #32](https://github.com/teranos/QNTX/issues/32) - Advanced fuzzy matching system
- [../fuzzy.go](../fuzzy.go) - Go fallback implementation
- [../matcher.go](../matcher.go) - Matcher interface
- [../executor.go](../executor.go) - AxExecutor integration
