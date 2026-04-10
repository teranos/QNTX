# Reticulum Transport

QNTX on the mesh. Reticulum provides the network layer QNTX wants to live on — cryptographic identity, transport agnosticism, delay tolerance, self-configuration.

## Why Reticulum

The sync protocol is transport-agnostic. The `Conn` interface abstracts the wire. Today that wire is WebSocket over HTTP. Reticulum replaces the HTTP assumptions — IP addresses, DNS, always-on connectivity — with a cryptographic mesh where nodes are identified by keys, links form over any physical medium, and routing is automatic.

A QNTX node becomes a Reticulum destination. The same `Peer.Reconcile()` that syncs over WebSocket runs unchanged over LoRa, serial, I2P, or satellite.

## Architecture

```
sync.Conn (Go)
    |
reticulum.Conn (adapter)
    |  wazero FFI
qntx-core + leviculum (Rust)
    |
Reticulum network (any transport)
```

**Leviculum** is a wire-compatible Rust implementation of Reticulum. Compiles as `no_std` with only `alloc` — fits QNTX's existing Rust/WASM strategy. Same toolchain as qntx-core's parser, Merkle tree, and content hashing.

## Identity Convergence

A QNTX node's ed25519 keypair (`did:key`) becomes its Reticulum destination identity. One key, two purposes — no mapping layer. An attestation signed by a DID is verifiable by anyone who can address the corresponding Reticulum destination.

The node already generates ed25519 at first boot (`server/nodedid`). Reticulum needs X25519 for key exchange — derived from the same ed25519 seed via standard curve conversion.

## Sync Over Constrained Links

Reticulum over LoRa has ~500 byte MTU. The sync protocol currently uses JSON. For constrained links, the Reticulum `Resource` system handles fragmentation and reassembly — but a binary encoding (msgpack or protobuf) would reduce overhead. The existing rate limits (`maxGroupsPerSync`, `maxAttestationsPerSync`) may need per-transport tuning.

The Merkle tree is the natural fit: matching roots cost one message. Only divergent groups transfer. A field station on LoRa syncs the delta, not the world.

## Next Steps

| Code | Name | What |
|------|------|------|
| **IDN** | Identity | Derive Reticulum destination hash from node ed25519 keypair. Prove one key serves both DID and destination. |
| **LNK** | Link | Implement `sync.Conn` over Reticulum links via Leviculum. Read/write JSON sync messages through an encrypted link. |
| **ANN** | Announce | Announce QNTX destinations on the mesh. Discover other QNTX nodes. Context-based interest filtering. |
| **CFG** | Config | `am.toml` support for Reticulum peers: `transport = "reticulum"`, destination hash instead of URL. |
| **BRG** | Bridge | End-to-end proof: two QNTX instances sync attestations over a Reticulum link. |

**IDN** is the foundation. Everything else builds on it.

## Related

- [sync/](../sync/) — Merkle reconciliation protocol
- [server/nodedid/](../server/nodedid/) — ed25519 DID generation
- [docs/vision/reticulum.md](../docs/vision/reticulum.md) — design vision
- [docs/vision/identity.md](../docs/vision/identity.md) — identity convergence
- [Leviculum](https://codeberg.org/Lew_Palm/leviculum) — Rust Reticulum implementation
- [Reticulum](https://reticulum.network/) — the cryptographic mesh network
