# qntx-fuzzy

High-performance fuzzy matching plugin for QNTX, written in Rust.

## Overview

This plugin provides advanced fuzzy matching capabilities for QNTX attestation queries, replacing the basic substring matching in the Go implementation with a multi-strategy approach:

| Strategy | Score | Description |
|----------|-------|-------------|
| Exact | 1.0 | Exact case-insensitive match |
| Prefix | 0.9 | Query is prefix of value |
| Word Boundary | 0.85 | Query matches complete word |
| Substring | 0.65-0.75 | Query appears within value |
| Jaro-Winkler | 0.6-0.82 | String similarity > 85% |
| Levenshtein | 0.6-0.8 | Edit distance ≤ 2 |

## Building

```bash
cd plugins/qntx-fuzzy
cargo build --release
```

The binary will be at `target/release/qntx-fuzzy`.

## Running

```bash
# Default port 9100
./target/release/qntx-fuzzy

# Custom port
QNTX_FUZZY_PORT=9200 ./target/release/qntx-fuzzy

# With custom minimum score
QNTX_FUZZY_MIN_SCORE=0.7 ./target/release/qntx-fuzzy
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `QNTX_FUZZY_PORT` | 9100 | gRPC server port |
| `QNTX_FUZZY_MIN_SCORE` | 0.6 | Minimum match score (0.0-1.0) |
| `RUST_LOG` | info | Log level (trace, debug, info, warn, error) |

## gRPC API

The service exposes `FuzzyMatchService` with these methods:

### RebuildIndex
Rebuilds the fuzzy index with new vocabulary. Called on startup and when vocabulary changes.

```protobuf
rpc RebuildIndex(RebuildIndexRequest) returns (RebuildIndexResponse);
```

### FindMatches
Finds vocabulary items matching a query with ranked scores.

```protobuf
rpc FindMatches(FindMatchesRequest) returns (FindMatchesResponse);
```

### BatchMatch
Processes multiple queries in a single request.

```protobuf
rpc BatchMatch(BatchMatchRequest) returns (BatchMatchResponse);
```

### Health
Health check for service readiness.

```protobuf
rpc Health(HealthRequest) returns (HealthResponse);
```

## Go Client

A Go client is provided for integration with QNTX:

```go
import "github.com/teranos/QNTX/plugins/qntx-fuzzy/client"

// Create client
cfg := client.DefaultRustFuzzyMatcherConfig()
cfg.ServiceAddress = "localhost:9100"
matcher, err := client.NewRustFuzzyMatcher(cfg)

// Update vocabulary (call when attestations change)
ctx := context.Background()
matcher.UpdateVocabulary(ctx, predicates, contexts)

// Find matches (same interface as ax.FuzzyMatcher)
matches := matcher.FindMatches("auth", allPredicates)
```

The `RustFuzzyMatcher` implements the same interface as the built-in `ax.FuzzyMatcher`, enabling drop-in replacement.

## Integration with AxExecutor

To use the Rust fuzzy matcher in AxExecutor:

```go
// In AxExecutor initialization
rustMatcher, err := client.NewRustFuzzyMatcher(client.DefaultRustFuzzyMatcherConfig())
if err != nil {
    // Fall back to built-in Go matcher
    rustMatcher = nil
}

// The wrapper automatically falls back to Go implementation
// if the Rust service is unavailable
```

## Performance

Expected improvements over Go implementation:

| Vocabulary Size | Go (current) | Rust | Improvement |
|-----------------|--------------|------|-------------|
| 1K items | ~1ms | ~0.1ms | 10x |
| 10K items | ~10ms | ~0.3ms | 33x |
| 100K items | ~100ms | ~3ms | 33x |

Additional capabilities:
- Typo tolerance via Levenshtein distance
- Better ranking via Jaro-Winkler similarity
- Consistent scoring across all match types

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    QNTX Core (Go)                       │
│  ┌────────────────┐    ┌───────────────────────────┐   │
│  │  AxExecutor    │───►│ RustFuzzyMatcher (wrapper)│   │
│  └────────────────┘    └───────────┬───────────────┘   │
└─────────────────────────────────────┼───────────────────┘
                                      │ gRPC
                                      ▼
┌─────────────────────────────────────────────────────────┐
│              qntx-fuzzy (Rust Plugin)                   │
│  ┌──────────────────────────────────────────────────┐  │
│  │  FuzzyMatchService (tonic gRPC)                   │  │
│  └──────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────┐  │
│  │  FuzzyEngine                                      │  │
│  │  - Multi-strategy matching                       │  │
│  │  - In-memory vocabulary index                    │  │
│  │  - Thread-safe with parking_lot::RwLock          │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Development

### Running Tests

```bash
cargo test
```

### Regenerating Protobuf

The protobuf code is generated at build time via `build.rs`. To regenerate:

```bash
cargo build
```

### Benchmarking

```bash
cargo bench
```

## Related

- [GitHub Issue #32](https://github.com/teranos/QNTX/issues/32) - Advanced fuzzy matching system
- [ats/ax/fuzzy.go](../../ats/ax/fuzzy.go) - Current Go implementation
- [External Plugin Guide](../../docs/development/external-plugin-guide.md)
