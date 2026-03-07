# ix-bin: Binary Ingestion Plugin

Ingests arbitrary binary files into QNTX — detects format, parses structure, writes attestations to the ATS. Written in D as a proof that CTFE (compile-time function execution) can generate protocol codecs and binary parsers with zero runtime reflection and zero external dependencies.

Fourth plugin language in QNTX (after Go, Rust, TypeScript).

## What it does

Drop any binary file onto the hex viewer glyph (⬢). ix-bin detects the format from magic bytes, parses what it can, and creates an attestation with the structural summary. 15 formats detected, 3 with deep parsers (PCAP, ELF, Mach-O).

## Why D

The thesis: D's compile-time function execution can replace entire code generation toolchains. The protobuf codec, HPACK Huffman trie, and binary format parsers are all generated at compile time from struct annotations and layouts. No protoc, no runtime reflection, no external dependencies.

The hand-rolled gRPC stack (HTTP/2 + HPACK + protobuf) exists because D has no grpc-go equivalent. It's minimal but functional — enough to prove the CTFE approach works against Go's grpc-go in production.

## Building

Requires LDC and dub on PATH. `make ix-bin-plugin` from project root builds and installs.

## Known limitations

- Single-connection gRPC server (sequential accept)
- No CONTINUATION frames, no streaming RPCs, no TLS
- Permissive HTTP/2 flow control (~1GB window)
- Format parsers are read-only summaries, not full dissectors
