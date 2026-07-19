# Reticulum Implementation Research

Research into Reticulum protocol implementations for QNTX integration as a plugin. See [reticulum.md](./reticulum.md) for the vision.

P2P attestation sync was removed from QNTX core (#779) because baking it into the monolith caused repeated breakage. The distributed future lives as a plugin.

## Protocol basics

- Identity = Ed25519 signing + X25519 encryption keypair
- Destination address = truncated SHA-256 of (app name hash || identity hash), 16 bytes
- Encrypted packet payload: 383 bytes
- Link establishment: 3 packets, 297 bytes total
- Max hops: 128, min bitrate: 5 bits/sec
- Crypto: X25519, Ed25519, AES-256-CBC, HMAC-SHA256, HKDF — non-negotiable

## LXMF

[LXMF](https://github.com/markqvist/lxmf) (Lightweight Extensible Message Format) provides store-and-forward on top of Reticulum. Messages carry a `Fields` dictionary with custom type/data/meta fields designed for embedding external protocols. Attestations could ride in these fields.

Delivery methods: opportunistic (single packet), direct (establishes link), propagated (store-and-forward via propagation nodes). Propagation nodes store messages for offline peers and deliver on reconnect.

## Implementations

### Complete

| Project | Language | License | Notes |
|---|---|---|---|
| [Reticulum](https://github.com/markqvist/Reticulum) | Python | Reticulum License | Reference. Canonical. Anti-AI/ML clause in license |
| [RetiNet](https://codeberg.org/skyguy/retinet) | Python | AGPL-3.0 | Fork — exists because original license blocks Debian/F-Droid |
| [Leviculum](https://codeberg.org/Lew_Palm/leviculum) | Rust | AGPL-3.0 | `no_std` core, runs on MCUs. `lnsd` drop-in replaces `rnsd`. 503 commits |
| [go-reticulum](https://github.com/svanichkin/go-reticulum) | Go | MIT | Full CLI suite. Parity target: Python 1.1.5 |
| rns-cr | Crystal | MIT | |

### Elixir

| Project | Approach | License | Status |
|---|---|---|---|
| [sgiath/reticulum](https://github.com/sgiath/reticulum) | Pure Elixir/OTP | WTFPL | Methodical, phased. Packets, crypto, announce/path, receipts, proofs. UDP. No links/channels yet |
| [reticulum_ex](https://codeberg.org/Reticulum_Elixir/reticulum_ex) | Pure Elixir | WTFPL | TCP interfaces. hex.pm published |
| [rns_ex](https://github.com/jtippett/rns_ex) | Pure Elixir | MIT | Claims full parity incl. links/channels/resources. Appeared in 2 days — depth unverified |
| [ex_reticulum](https://github.com/jtippett/ex_reticulum) | Rust NIF via Rustler | None | Wraps Reticulum-rs. Thin integration layer |

### Other incomplete

Rust (Reticulum-rs, cyypherus/rinse, dearheart, ferret-rns), Go (holiman/Reticulum-Go), Zig (reticulum-zig), Swift (reticulum-swift), Kotlin (reticulum-kt, KRNS-core), Java (reticulum-network-stack), C++ (microReticulum — ESP32), JavaScript (rns.js), Elixir NIF (twenty-eighty/reticulum_ex), MicroPython, Elixir (sgiath/reticulum).

## Integration paths for QNTX

### Leviculum as Rust library

QNTX already has Rust components (sqlite-ffi, WASM). Leviculum's `no_std` core could link directly as a library. No Python subprocess, no FFI bridge. A Rust plugin embedding leviculum is a self-contained Reticulum node. AGPL-3.0 — no AI/ML restriction.

### go-reticulum as Go library

A Go plugin could embed go-reticulum directly. No foreign runtime. MIT licensed.

### Elixir/OTP

The BEAM model — supervision trees, fault isolation, hot code loading — maps naturally to a distributed attestation network. Each peer connection as a supervised process. Crashes isolated. Links that come and go are OTP process lifecycle. Would require a new plugin runtime (Elixir/Erlang), which QNTX doesn't have today.

### Python via pyre

[pyre](https://github.com/teranos/pyre) is a Rust plugin embedding a Python interpreter (formerly `qntx-python`). Running the reference RNS library directly is possible without a subprocess. But the Reticulum License anti-AI/ML clause applies.

### TCP/UDP to running daemon

Any language can connect to a running `rnsd` or `lnsd` via TCP interface on a local port, speaking the Reticulum wire protocol directly. Simplest integration, but adds a dependency on a running daemon.

### Bridges

- [RNS-over-HTTP](https://git.quad4.io/Reticulum-Interfaces/RNS-over-HTTP) — tunnels Reticulum over HTTP POST
- [Reticulum OpenAPI](https://github.com/FreeTAKTeam/Reticulum_OpenAPI) — REST layer on LXMF

## License landscape

| Implementation | License | AI/ML restriction |
|---|---|---|
| Python reference | Reticulum License | Yes — cannot be used in AI/ML systems |
| Leviculum | AGPL-3.0 | No |
| go-reticulum | MIT | No |
| RetiNet | AGPL-3.0 | No |
| Elixir (sgiath, reticulum_ex) | WTFPL | No |
| Elixir (rns_ex) | MIT | No |

The anti-AI/ML clause in the original license is what drove the community forks.

## Community

- [reticulum.zulipchat.com](https://reticulum.zulipchat.com)
- [#reticulum:matrix.org](https://matrix.to/#/#reticulum:matrix.org)
- [awesome-reticulum](https://github.com/lorien/awesome-reticulum)
- FOSDEM 2026 had a Reticulum community meetup and Reticulum-rs talk
