# QNTX Rust Types

Auto-generated Rust type definitions from QNTX's Go source code.

## Usage in Tauri

Add these dependencies to your `Cargo.toml`:

```toml
[dependencies]
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
lazy_static = "1.4"
```

Then in your Rust code:

```rust
use qntx_types::async::Job;
use qntx_types::sym::*;

// Use the types
let job_status = JobStatus::Running;

// Access symbol constants
println!("Pulse symbol: {}", pulse);
println!("Command for IX: {}", command_to_symbol.get("ix").unwrap());
```

## Generated Files

- [`mod.rs`](./mod.rs) - Main module file with re-exports
- [`async.rs`](./async.rs) - Async job types (Job, JobStatus, Progress, etc.)
- [`budget.rs`](./budget.rs) - Rate limiting and budget types
- [`schedule.rs`](./schedule.rs) - Pulse scheduling types
- [`server.rs`](./server.rs) - Server API request/response types
- [`sym.rs`](./sym.rs) - Symbol constants and mappings
- [`types.rs`](./types.rs) - Core types (Attestation, Event, CodeBlock, etc.)

## Type Compatibility

All types derive `serde::Serialize` and `serde::Deserialize` for JSON compatibility with QNTX's Go backend.

### Example: Deserializing from QNTX API

```rust
use qntx_types::async::Job;

let json = r#"{"id":"123","handler_name":"test","status":"completed",...}"#;
let job: Job = serde_json::from_str(json)?;
```

## Regeneration

Types are regenerated with:

```bash
make types
# or
./qntx typegen --lang rust --output types/generated/
```

**Do not manually edit** - changes will be overwritten when types are regenerated.

---

*Generated at: 2026-01-03T21:38:39Z*
