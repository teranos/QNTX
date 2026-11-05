# ADR-012: Browser as First-Class Node

## Status

Accepted (revised 2026-08-09)

Date: 2026-03-15


## Context

The browser should not be a lite client that talks to a server. It should run the same engine — same parser, same execution. This means the Rust engine need to compile to WASM and run in the browser.

## The problem

The Rust engine compiles to WASM via `wasm-bindgen`. Both need to run in the browser and talk to each other.


### Composition

Modules call each other directly — "this is what i actually want in reality".
One module's exports are wired as another's imports at instantiation, so a
cross-module call carries no JS frame. JS wires the graph once and then stays
out of the path.

Two constraints follow. Raw wasm imports carry only scalars, so payload verbs
take a pointer and a length rather than an owned buffer. And a pointer is
meaningless across separate linear memories, so every module in the graph
imports one shared `WebAssembly.Memory` instead of defining its own.

The WASM Component Model is rejected. The reason is not weight, it is
countability: a module's import section is the list of everything it can
reach, and a handful of `env.*` entries can be read and checked. Typed
inter-module interfaces produce thousands, and a leak crossing is invisible.

What is rejected is JS relaying between two modules — "wasm<>ts<>wasm bad" —
not JS as the host. A module still reaches IndexedDB and
`crypto.getRandomValues` through the host, because it cannot reach them any
other way.

## Open questions

- IndexedDB as the browser-side storage backend. Answered by shipping:
  `crates/ats-indexeddb` backs attestation CRUD and query in the browser.
- Offline-first: what subset of the engine runs without a server? Query
  parsing, attestation CRUD and query, and cosine similarity already do.
  What remains is a scoping decision, not a missing capability.
- A node has a Node DID (ADR-010). A first-class browser therefore has one,
  and `internal/nodedid/` is server-side only. Where a browser's signer
  identity comes from is left to a later ADR.
