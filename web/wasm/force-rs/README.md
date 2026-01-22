# Force Simulation (Rust/WASM)

Force-directed graph layout implemented in Rust, compiled to WebAssembly.

This is a direct port of the Zig version for comparison purposes. Uses raw WASM exports (no wasm-bindgen) to keep the comparison fair.

## Building

```bash
# Install wasm32 target (one-time)
rustup target add wasm32-unknown-unknown

# Build
cd web/wasm/force-rs
cargo build --release --target wasm32-unknown-unknown

# Copy to dist
mkdir -p ../dist
cp target/wasm32-unknown-unknown/release/force_wasm.wasm ../dist/force-rs.wasm
```

Or use Make:

```bash
make wasm-force-rs
```

Output: `web/wasm/dist/force-rs.wasm`

## API

Identical to Zig version - same function signatures, same memory layout.

See `../force/README.md` for full documentation.

## Comparison Notes

| Aspect | Zig | Rust |
|--------|-----|------|
| Allocator | Fixed buffer (explicit) | Custom bump allocator (no_std) |
| State | Module-level globals | thread_local RefCell |
| NaN check | `std.math.isNan()` | `.is_nan()` |
| Random | Manual LCG | Manual LCG (identical) |
| Build | `zig build` | `cargo build --target wasm32` |

Both use:
- No standard library heap
- Same 1MB fixed memory pool
- Same force algorithms (O(n²) many-body, collision)
- Same exported function names

## Why no wasm-bindgen?

To keep the comparison fair. wasm-bindgen adds convenience (automatic JS glue code, better types) but also adds overhead and complexity. Raw WASM exports let us compare the core simulation performance directly.

If you prefer wasm-bindgen ergonomics in production, add it to `Cargo.toml`:

```toml
[dependencies]
wasm-bindgen = "0.2"
```
