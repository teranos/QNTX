# ADR-030: Identity Providers

**Status:** Proposed — decided in conversation, nothing compiles against it
**Date:** 2026-08-12
**Related:** ADR-010 (Identity System), ADR-012 (Browser WASM Parity), ADR-014 (plugin-provided service), ADR-027 (Levels)

`server/auth/caller.go` cites ADR-026 for namespaces and ADR-027 for levels.
Neither document exists. This one was numbered 026 before that was noticed,
and moved.

## Decision

"laye would just be another identity provider"

"satisfying a qntx contract"

laye is the wasm that ships with QNTX web.

relaye ceases to exist. QNTX signs bindings with its own node DID and is the
peer laye bootstraps from.

The node DID is the anchor. A QNTX deployment signs bindings with
`server/nodedid/`'s key and is its own root.

An identity provider gives QNTX web four operations:

    did()          -> string             did:key for this browser
    sign(bytes)    -> signature          proof of possession
    bindings()     -> SignedBinding[]    external identities bound to the key
    link(provider) -> SignedBinding      run a ceremony, add one

`sign` is the only use the private key is put to. That is what makes "never
leaves the tab" enforceable.

The contract speaks `did:key`. The consumer names the shape and the provider
converts.

## Consequences

- The passkey path moves behind the contract and becomes one provider
  among several.
- The private key never leaves the tab. Everything else — the DID, the
  public key, signatures, bindings — exists in order to leave.
