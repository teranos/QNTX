# ix-bin: D-Language Binary Ingestion Plugin

A QNTX plugin written in D that ingests binary files (PCAP, ELF, etc.), parses their structure, and writes attestations to the ATS. Implements the full `DomainPluginService` gRPC protocol in pure D with zero external dependencies — HTTP/2 framing, HPACK header compression, protobuf wire format, and gRPC message framing are all from scratch.

Fourth plugin language in QNTX (after Go, Rust, TypeScript). The thesis: D's compile-time function execution (CTFE) can generate protocol codecs and binary parsers at compile time, eliminating runtime schema interpretation entirely.

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

Requires LDC (LLVM D compiler). Assumes `ldc2` and `dub` are on PATH.

From `qntx-ix-bin/Makefile`:

```bash
make build    # dub build --compiler=ldc2 → bin/qntx-ix-bin-plugin
make test     # dub test --compiler=ldc2
make install  # copies to ~/.qntx/plugins/qntx-ix-bin-plugin
```

Root Makefile target (`Makefile:245`): `make ix-bin-plugin` — builds and installs.

## Running with QNTX

1. Build and install: `make ix-bin-plugin` (root `Makefile:245`)
2. Enable in `am.toml` — plugin discovery per ADR-002, binary search in `plugin.paths` for `qntx-ix-bin-plugin`:

```toml
[plugin]
enabled = ["ix-bin"]
```

3. `make dev` — QNTX discovers and starts the plugin. Verify in logs:

```
plugin-loader  Searching for 'ix-bin' plugin in [~/.qntx/plugins ./plugins]
plugin-loader  Found 'ix-bin' plugin binary: /path/to/qntx-ix-bin-plugin
plugin-loader  Connected to 'ix-bin' plugin gRPC server v0.1.0 at 127.0.0.1:PORT
```

Source: `plugin/grpc/loader.go:47` (search log), `plugin/grpc/loader.go:178` (found log), `plugin/grpc/loader.go:120-124` (candidate names: `qntx-ix-bin-plugin`, `qntx-ix-bin`, `ix-bin`)

## Verification status

### Passing

- **Build**: compiles cleanly, produces `bin/qntx-ix-bin-plugin`
- **Unit tests**: 4 modules pass (`dub test --compiler=ldc2`):
  - `ixbin.proto`: varint round-trip, MetadataResponse encode/decode, InitializeRequest with map, repeated strings
  - `ixbin.hpack`: integer codec round-trip, string codec round-trip, static table lookup, Huffman trie existence
  - `ixbin.grpc`: gRPC frame/unframe round-trip, empty frame handling (`grpc.d:486-500`)
  - `ixbin.ingest`: format detection, struct sizes (static assert), hex dump, magic scanner, ELF parser (`ingest.d:432-474`)
- **CLI**: `--version` prints `qntx-ix-bin-plugin 0.1.0`, `--port N` binds TCP and announces `QNTX_PLUGIN_PORT=N` on stdout
- **gRPC integration**: Hand-rolled HTTP/2 + HPACK + protobuf works against Go's grpc-go. QNTX core successfully calls `Metadata()` and `Initialize()` RPCs. Verified manually — plugin loads and connects.

### Not yet verified

1. **ATSStore callback** (`ats.d:24-47`): gRPC client calls `/protocol.ATSStoreService/GenerateAndCreateAttestation`. Protobuf encoding of nested `AttestationCommand` + `google.protobuf.Struct` attributes needs to match Go's expectations byte-for-byte. Test: `curl -X POST --data-binary @file.pcap http://localhost:PORT/api/ix-bin/ingest` — response should show `attestations_created > 0`.

2. **HTTP handler routing**: QNTX core strips `/api/ix-bin/` prefix (`plugin/grpc/client.go:338-346`), plugin receives `POST /ingest` (`plugin.d:121`). Same curl as above verifies both routing and ATSStore in one shot.

3. **Hex viewer glyph**: Glyph registered with symbol ⬢ (`plugin.d:102`), module served from `/api/ix-bin/hex-viewer-module.js` (`plugin.d:123,230-239`). Ingest button calls `fetch('/api/ix-bin/ingest')` (`hex-viewer-module.js:94`). Verify: glyph appears in spawn menu, file upload works, ingest creates attestations.

4. **Pulse job execution**: Handler `ix-bin.ingest` registered (`plugin.d:72`), dispatched in `executeJob` (`plugin.d:245-285`). Verify: trigger job through Pulse, check response includes progress and result JSON.

## Known limitations

1. **Single-connection server** (`grpc.d:216-223`): `serve()` accepts connections sequentially. If the connection drops and core reconnects, the old connection must close first.

2. **No CONTINUATION frames** (`grpc.d:281-303`): Headers read only from HEADERS frame. Very long `:path` values or many metadata headers could trigger CONTINUATION, which is unhandled.

3. **No gRPC streaming**: Only unary RPCs. WebSocket bidirectional streaming is stubbed.

4. **Permissive flow control** (`grpc.d:251`): Sends ~1GB window update on connection start. Does not track send-side flow control. Fine for plugin RPCs, would break for very large binary payloads via gRPC DATA frames.

5. **No TLS**: Plain TCP (h2c). Matches QNTX's existing plugin transport (localhost only).

6. **No dynamic table eviction edge cases**: Single entries exceeding HPACK `maxSize` are silently dropped.

7. **Format parsers are read-only** (`ingest.d:148-244`): PCAP counts packets but doesn't extract payloads. ELF reads header but doesn't walk program/section headers. Summaries for attestation creation, not full dissectors.

## Next

**Immediate** — complete verification items 1-4 above (ATSStore callback is the critical path).

**Then:**

- Thread-per-connection: replace sequential `accept()` loop (`grpc.d:216-223`) with `std.concurrency` or `core.thread`
- CONTINUATION frame support: buffer HEADERS payloads across CONTINUATION frames before HPACK decode
- More format parsers: ZIP central directory, PNG chunks, PDF cross-reference table
- SIMD scanning: replace scalar `scanForMagic` (`ingest.d:254-275`) with `core.simd` SSE2 intrinsics
