# ix-bin: D-Language Binary Ingestion Plugin — Handover

## What this is

A QNTX plugin written in D that ingests binary files (PCAP, ELF, etc.), parses their structure, and writes attestations to the ATS. It implements the full `DomainPluginService` gRPC protocol in pure D with zero external dependencies — HTTP/2 framing, HPACK header compression, protobuf wire format, and gRPC message framing are all implemented from scratch.

This is the fourth plugin language in QNTX (after Go, Rust, TypeScript). The thesis it proves: D's compile-time function execution (CTFE) can generate protocol codecs and binary parsers at compile time, eliminating runtime schema interpretation entirely.

## How it works

```
QNTX Core                          ix-bin plugin (D)
    |                                    |
    |--- gRPC/HTTP2 (TCP) ------------->|
    |    Metadata()                      |-- returns name, version
    |    Initialize(endpoints, token) -->|-- connects ATSStore gRPC client
    |    HandleHTTP(POST /ingest) ------>|-- detectFormat() → parsePcap/parseElf
    |                                    |-- creates AttestationCommand
    |    <-- ATSStore callback RPC ------|-- writes attestation to ATS
    |    ExecuteJob(ix-bin.ingest) ----->|-- same flow via Pulse async
    |    RegisterGlyphs() ------------->|-- returns hex viewer glyph (⬢)
    |    Health() ---------------------->|-- returns status + ATS connection state
```

### File layout

```
qntx-ix-bin/
├── dub.json                          # DUB package manifest
├── Makefile                          # build / test / install targets
├── source/
│   ├── hex-viewer-module.js          # Glyph UI (embedded via D string import)
│   └── ixbin/
│       ├── app.d                     # Entry point, CLI flags, QNTX_PLUGIN_PORT announcement
│       ├── proto.d                   # Protobuf codec — @Proto UDA + CTFE encode/decode
│       ├── hpack.d                   # HPACK — static table, Huffman trie (CTFE-built), int/string codec
│       ├── grpc.d                    # HTTP/2 frames + gRPC server + gRPC client
│       ├── ingest.d                  # Format detection, CTFE struct parsers, hex dump, scanner
│       ├── plugin.d                  # Plugin logic, HTTP handlers, glyph def, RPC dispatch
│       └── ats.d                     # ATSStore gRPC client wrapper
```

### Key D patterns

**CTFE protobuf codec** (`proto.d`): Structs annotated with `@Proto(N)` get encode/decode generated at compile time. `encode(T)` uses `static foreach` over `T.tupleof` + `__traits` to emit serialization code per field type. No runtime reflection.

**CTFE Huffman trie** (`hpack.d`): `buildHuffTrie()` runs during compilation, constructing a ~500-node binary trie from the RFC 7541 Huffman table (257 symbols, up to 30-bit codes). Result is `static immutable` — zero runtime cost.

**CTFE binary parsers** (`ingest.d`): `parseBinaryStruct!PcapGlobalHeader(data)` casts bytes to a packed `align(1)` struct. `static assert` on sizes catches layout errors at compile time. The struct layout IS the parser.

## Building

Requires LDC (LLVM D compiler). Installed at `~/dlang/ldc-1.39.0/` via the dlang.org installer script.

```bash
source ~/dlang/ldc-1.39.0/activate
cd qntx-ix-bin

dub build --compiler=ldc2     # → bin/qntx-ix-bin-plugin
dub test --compiler=ldc2      # → runs unittests in all 4 modules
./bin/qntx-ix-bin-plugin --version
```

Or via the Makefile (assumes `ldc2` and `dub` are on PATH):

```bash
make build    # build
make test     # unit tests
make install  # copies to ~/.qntx/plugins/
```

Root Makefile target: `make ix-bin-plugin`

## Automated verification (what passes now)

- `dub build --compiler=ldc2` — compiles cleanly, produces `bin/qntx-ix-bin-plugin`
- `dub test --compiler=ldc2` — 4 modules pass:
  - `ixbin.proto`: varint round-trip, MetadataResponse encode/decode, InitializeRequest with map, repeated strings
  - `ixbin.hpack`: integer codec round-trip, string codec round-trip, static table lookup, Huffman trie existence
  - `ixbin.grpc`: gRPC frame/unframe round-trip, empty frame handling
  - `ixbin.ingest`: format detection (PCAP, ELF, PNG, unknown), struct sizes (static assert), hex dump, magic scanner (finds 2 occurrences), ELF parser (64-bit, little-endian, shared-object, x86-64)
- `--version` prints `qntx-ix-bin-plugin 0.1.0` and `QNTX Version: >= 0.1.0`
- `--port N` binds to TCP, outputs `QNTX_PLUGIN_PORT=N` on stdout (flushed immediately), log messages on stderr

## Manual verification needed

1. **gRPC integration with QNTX core**: The HTTP/2 + HPACK implementation has not been tested against Go's grpc-go client. The HPACK decoder handles indexed, literal-with-indexing, literal-without-indexing, and never-indexed representations, plus Huffman-encoded strings. But edge cases in HPACK dynamic table management or HTTP/2 flow control may surface under real traffic. Start the plugin, configure `am.toml` to discover it, and verify `Metadata()` and `Initialize()` RPCs succeed.

2. **ATSStore callback**: The gRPC client (`ats.d`) connects to core's ATSStore service and calls `GenerateAndCreateAttestation`. This path is untested end-to-end. The protobuf encoding of `GenerateAttestationRequest` (with nested `AttestationCommand` and `google.protobuf.Struct` attributes) needs to match what Go's server expects byte-for-byte.

3. **HTTP handler routing**: QNTX core strips the `/api/ix-bin/` prefix before forwarding to the plugin's `HandleHTTP`. Verify that `POST /ingest` with a binary body returns a JSON response with format, size, and attestation count. Test with a real PCAP file and a real ELF binary.

4. **Hex viewer glyph**: Verify the glyph appears in the spawn menu, the JS module loads, drag-and-drop works, and the "Ingest" button calls `/api/ix-bin/ingest` and reports results.

5. **Pulse job execution**: Trigger an `ix-bin.ingest` job through Pulse and verify `ExecuteJob` RPC returns success with progress and result JSON.

## Known limitations

1. **Single-connection server**: `GrpcServer.serve()` accepts connections sequentially (`listener.accept()` blocks in a loop, then `serveConnection()` blocks until that connection closes). QNTX core typically uses one long-lived connection, so this works, but if the connection drops and core reconnects, the old connection must close first. A production version would spawn a thread per connection.

2. **No HTTP/2 CONTINUATION frames**: If HPACK-encoded headers exceed one HTTP/2 frame, they arrive split across HEADERS + CONTINUATION frames. The current implementation only reads headers from the HEADERS frame. grpc-go typically fits headers in one frame for the short paths used here, but very long `:path` values or many metadata headers could trigger CONTINUATION.

3. **No gRPC streaming**: `HandleWebSocket` is registered but the bidirectional streaming implementation is stubbed (returns empty). The gRPC server only handles unary RPCs. WebSocket proxying won't work.

4. **Flow control is permissive**: The server sends a ~1GB window update on connection start and per-stream window updates after every DATA frame. It does not track send-side flow control. This is fine for the small payloads in plugin RPCs but would break for very large binary ingestion payloads sent via gRPC DATA frames (the HTTP /ingest endpoint receives data via HandleHTTP, which is already a single unary RPC).

5. **No TLS**: gRPC runs over plain TCP (h2c). Matches QNTX's existing plugin transport (localhost only, `insecure.NewCredentials()`).

6. **No dynamic table eviction edge cases**: The HPACK dynamic table implementation evicts from the back when adding entries, but doesn't handle the case where a single entry exceeds `maxSize` (it silently drops it). This matches the RFC behavior but hasn't been stress-tested.

7. **Format parsers are read-only**: PCAP parsing counts packets and total bytes but doesn't extract individual packet payloads. ELF parsing reads the header but doesn't walk program/section headers. These are summaries for attestation creation, not full dissectors.

8. **`make test` blocked by Go toolchain**: The environment can't download Go 1.25.0 (DNS resolution failure for `storage.googleapis.com`). This is a pre-existing infrastructure issue unrelated to ix-bin. The D plugin's own tests pass via `dub test`.

## What to work on next

- **Integration test**: Run QNTX core with ix-bin enabled, send a real PCAP file to `POST /api/ix-bin/ingest`, verify attestation appears in ATS
- **Thread-per-connection**: Replace sequential `accept()` loop with `std.concurrency` or `core.thread` to handle reconnections
- **CONTINUATION frame support**: Buffer HEADERS payloads across CONTINUATION frames before HPACK decode
- **More format parsers**: ZIP central directory, PNG chunks, PDF cross-reference table
- **SIMD scanning**: Replace scalar `scanForMagic` with actual SSE2 intrinsics via `core.simd` for large file scanning
- **Per-glyph config**: Store per-glyph state as attestations (like ix-json does) so the plugin is stateless across restarts
