# ADR-012: Browser as First-Class Node

## Status

Accepted.

## Context

The browser should not be a lite client that talks to a server. It should run the same engine — same parser, same execution. This means both kern (OCaml) and the Rust engine need to compile to WASM and run in the browser.

## The problem

kern (OCaml) compiles to WASM via `wasm_of_ocaml`, which outputs WasmGC instructions. The Rust engine compiles to WASM via `wasm-bindgen`. Both need to run in the browser and talk to each other.

### WasmGC support

- **Browsers**: Chrome 119+, Firefox 122+, Safari 18.2+ — all support WasmGC
- **wazero (Go)**: does not support WasmGC, no timeline ([#1860](https://github.com/tetratelabs/wazero/issues/1860))
- **wasmtime (Rust)**: supports WasmGC since v27.0 (November 2024)

This is why the browser path works but the Go server path doesn't for OCaml WASM.

### Composition

Modules do not call each other. Each exposes a small JS surface taking and
returning bytes, and JS moves them across. Nothing but bytes crosses, so the
producing toolchain — `wasm_of_ocaml` or `wasm-bindgen` — never enters the
picture.

## Open questions

- Is the Component Model the right composition mechanism, or is there something simpler?
- What does the interface between kern.wasm and qntx.wasm look like?
- IndexedDB as the browser-side storage backend (replacing SQLite)?
- Offline-first: what subset of the engine runs without a server?
