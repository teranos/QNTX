# Undefined terms in CLAUDE.md

An agent's first contact with QNTX is CLAUDE.md — loaded before any other file is read. The file uses domain words it never defines, so the agent starts off primed oddly: it is following rules phrased in vocabulary it has to guess at.

This table records those words at the moment of first contact. **Current assumption** is what a fresh agent infers from CLAUDE.md alone — unverified, and to be checked against `docs/GLOSSARY.md` and the ADRs.

| linenr | word | context | current assumption |
|---|---|---|---|
| 1 | QNTX | `# QNTX LAW` | Name of the project. Never expanded; unknown whether it is an acronym. |
| 9 | workers | `0 workers = no workers` | A configurable pool of background job processors; which subsystem owns them is not stated. |
| 9 | ticker interval | `0 ticker interval = no ticking` | A periodic timer driving some recurring background process; owner unstated. |
| 21 | User Vision | "User Vision outlives derived code" | Capitalized as a first-class concept: the user's intent and reasoning in their own words, a durable artifact that commits/PRs must preserve verbatim. |
| 38 | provider ceremony | "the provider ceremony" | The login/registration flow with an identity provider. "Ceremony" is WebAuthn spec vocabulary, so assumed a WebAuthn-style authentication ceremony; ADR-030 presumably defines it. |
| 38 | passkey | "what a passkey carries" | WebAuthn credential — but "carries" implies it holds domain claims beyond a bare key pair; what those are is unstated. |
| 42 | plugin | `## Plugins` | An extension running as a separate process (stated), started/stopped live from config; the server↔plugin protocol is unstated here. |
| 44 | am.toml | "`[plugin] enabled` in am.toml" | The server's main configuration file. Meaning of "am" unknown. |
| 44 | config watcher | "The config watcher diffs the list" | A server component watching am.toml for changes and applying the diff without restart. |
| 50 | ats | "build ats WASM module" | A core subsystem — also a top-level directory (`ats/`) and Rust crate (`ats-sqlite`). Acronym unexpanded; guess: attestation store/system, given the `attestations` table and `ats/storage`. |
| 50 | qntxwasm | "building with `qntxwasm` tag" | Go build tag gating WASM-dependent code paths. |
| 51 | `_qntx.go` | "Use different naming like `_qntx.go`" | Project convention for files that would naturally be named `_wasm.go`; the suffix has no Go toolchain meaning, which is the point. |
| 83 | that directory | "Two runners read that directory" | Dangling referent — no directory has been named at this point in the file. Assumed: the SQL migrations directory; path unknown. |
| 83 | both backends | "on both backends" | Storage backends — SQLite and Parquet, inferred from `backend = "parquet"`; whether others exist is unstated. |
| 83 | operational tables | "passkeys and operational tables live in SQLite" | Non-attestation bookkeeping tables (auth, jobs, config?) that stay in SQLite regardless of backend choice. |
| 83 | sqlitecgo | "`sqlitecgo.NewFileStore`" | A Go package bridging to the Rust `ats-sqlite` crate via cgo — consistent with Rust being the sole SQLite owner. |
| 89 | attestations | `CREATE TABLE attestations ...` | Looks like the core domain record of the entire system, yet its only appearance is inside a "NEVER do this" example. What an attestation *is* goes undefined. |
