# laye-me — signed chat

Payload-layer authorship for chat messages. The relaye WS
gateway stays byte-agnostic (per `crates/relaye/docs/
gateway.md`); every trust decision lives at the payload layer,
which is Ed25519 signatures every sender attaches and every
receiver verifies.

## Why

The gateway forwards opaque bytes and never authenticates the
sender — good for positions ("someone spoofed my dot" is
harmless), bad for chat ("someone said something as me"
matters). The right shape is the same one the M2i binding
already ships: the browser signs; the receiver checks.

## Topic

`laye-chat/v1`. New topic — `rave-chat/v1` stays plaintext so
rave.wasm keeps working during the transition. Laye clients
subscribe to both and render each flavor with its own
attribution ("authored by @onf@chaos.social" for signed +
resolved, "unattributed peer 12D3Ko…qaf" for plaintext or
resolved-to-nobody).

## Shape

```
SignedChat {
    author_peer_pubkey: [u8; 32],   // Ed25519 pubkey the sender
                                    // uses on the wire — the
                                    // same key that identifies
                                    // them as a libp2p peer.
    body: String,                   // free-text message body.
    at_ms: u64,                     // sender's clock, unix ms.
    signature: Vec<u8>,             // Ed25519 signature over
                                    // canonical_bytes().
}
```

Wire is JSON with hex-encoded byte fields — same shape family
as `SignedBinding`. Field names use the `_hex` suffix so a
future language-agnostic reader can be written by hand.

```json
{
  "author_peer_pubkey_hex": "21d778ae316c…",
  "body": "hello",
  "at_ms": 1751888000000,
  "signature_hex": "6f71ab991a…"
}
```

## Canonical bytes

```
laye-chat/v1|<author_peer_pubkey_hex>|<at_ms>|<body>
```

Same pipe-delimited pattern as `BindingClaim::canonical_bytes`.
Reproducible in any language without a serde toolchain.

## Verify

`SignedChat::verify()` decodes `author_peer_pubkey` as an
Ed25519 public key and checks the signature against
`canonical_bytes()`. Returns typed `VerifyError` (`BadAuthorKey`
/ `BadSignature`). Receiver drops any message where verify
fails.

## Trust model

Verification proves "the holder of `author_peer_pubkey` signed
this." Attribution — mapping that pubkey to a human — happens
one layer up: the receiver looks the peer up in the
`BindingTable` (M2i) and renders whichever `SignedBinding.
link.handle` it finds. No match = "unattributed peer."

The relay does not sign chat. Chat is peer-authored end to end.
`laye-identity/v1` bindings still trust the relay's Ed25519
pubkey (`SignedBinding.signer_pubkey`); `laye-chat/v1` messages
trust the author's own pubkey.

## Non-goals

- **No gateway-level auth.** Gateway stays byte-agnostic; the
  R16 handoff-doc invariant survives.
- **No moderation or block-lists.** Rendering-time concern.
- **No rate limiting.** Same reason.
- **No delivery guarantees.** Gossipsub is best-effort;
  receivers drop malformed bytes without notifying anyone.
- **No timestamp validation.** `at_ms` is display-only; a
  future receiver can render "sent in the past" or "sent in
  the future" but there's no cryptographic bind between clocks.
- **No encryption.** `laye-chat/v1` is public read, signed
  write. If we need private chat later, it's a different topic
  and a different shape.
