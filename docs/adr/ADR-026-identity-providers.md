# ADR-026: Identity Providers

**Status:** Proposed — decided in conversation, nothing compiles against it
**Date:** 2026-08-12
**Related:** ADR-010 (Identity System), ADR-012 (Browser WASM Parity), ADR-014 (plugin-provided service)

## Decision

"laye would just be another identity provider"

"satisfying a qntx contract"

"and relaye the relay in laye would be a qntx plugin as well"

The node DID is the anchor. A QNTX deployment signs bindings with
`server/nodedid/`'s key and is its own root — a peer of the laye
federation, not a client of it.

## Consequences

- There is no contract to satisfy yet. `server/auth/` has no provider
  seam; WebAuthn is the implementation. Writing the seam means moving the
  live passkey path behind it.
- The private key never leaves the tab. Everything else — the DID, the
  public key, signatures, bindings — exists in order to leave. A server can
  verify you and can never be you, so relaye signs claims about a key it
  has never held.
