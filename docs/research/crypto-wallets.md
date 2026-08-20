# Crypto Wallets and Smart Contract Protocols Research

Exploratory. Nothing here is implemented. Every mechanism referenced below already exists in QNTX; the brainstorm is how far each one stretches toward hosting wallets (Ethereum, Solana) and speaking to smart contract protocols.

## What QNTX already has that is wallet-shaped

**The identity provider contract is a wallet contract.** ADR-030 defines a provider as three operations — `did() -> string`, `sign(bytes) -> signature`, `bindings() -> SignedBinding[]` — with the guarantee that "`sign` is the only use the private key is put to. That is what makes 'never leaves the tab' enforceable." A crypto wallet is exactly this: an address, a signing oracle, and a set of claims about who holds it. laye already mints an ed25519 `did:key` per browser.

**Solana is the same curve.** A Solana address *is* an ed25519 public key, base58-encoded. Every laye browser key and every passkey-PRF-derived device key is already a valid Solana account waiting for its first lamport. Ethereum is not: EOAs are secp256k1 + keccak, a curve laye does not carry.

**Access tokens are machine wallets.** ADR-025: a token's 32 bytes are "an ed25519 seed rather than only a secret, so the token has a public half and its holder can sign as it" — plus `minted_by`, `namespace`, and `scope`. That is a scoped hot key with provenance, which is what an automated on-chain agent needs.

**Attestations are chain-event-shaped.** A log emitted by a contract is a signed, immutable, actor-bearing, timestamped claim. `[tx 0xabc…] is [transfer] of [erc20:usdc] by [0xdead…] at [block 21_004_113]` needs no new primitive. Append-only with no retraction even absorbs reorgs: a reorged-out transaction is not deleted, it is superseded by a new attestation from the same observer — "Two claims aren't a conflict — they're two claims."

**Pulse is an RPC budget.** Chain access is metered (provider credits, rate limits) and continuous (block polling, event subscriptions). Pulse's `budget.Limiter`/`budget.Tracker`, schedules (ADR-028), and priority queuing were built for exactly this shape of external dependency — ADR-014 already routed LLM providers through it.

**The plugin-provided-service pattern fits chains one-to-one.** ADR-014's design — provider plugins register as gRPC backends, multiple providers run simultaneously, the caller names which backend, core owns queuing and observability — maps directly onto RPC providers (public endpoint, Alchemy, Infura, a local node) behind a `ChainRPCProvider` optional interface next to `LLMProvider`.

## Hosting wallets: four custody postures

1. **Browser custody (extend laye).** Chain transactions are bytes; `sign(bytes)` already exists. Solana works today curve-wise. Ethereum requires secp256k1 signing in the shared WASM core (`ats`/`ats-id` — the k256 crate is the usual Rust route). The key never leaves the tab, so this is self-custody with QNTX as the interface.

2. **Node custody.** The node already holds a signing key (`server/nodedid/`) and signs bindings and door attestations with it. A node-held chain key makes the *deployment* an on-chain actor — useful for automated agents, treasury operations, paying for its own resources. This is custodial; it belongs behind namespace + scope the way tokens are.

3. **External wallet as identity provider.** ADR-030's own move — "laye would just be another identity provider" — extends: MetaMask and Phantom satisfy the same three-operation contract. Binding an Ethereum address to the ROOT User is the existing binding ceremony with a new vocabulary: Sign-In With Ethereum (EIP-4361) and its Solana equivalent are precisely "the string in am.toml is whatever the provider calls the account." An address in `auth.root_identities` means logging into QNTX with a wallet signature. Spending keys stay in the user's wallet; QNTX never holds them.

4. **Passkey-PRF-derived wallets.** ADR-030 already derives a per-device key from the authenticator's PRF. The same derivation with a chain-specific info string yields a device-bound Solana key that no server, and no tab, ever stored. Losing the device loses the key — the same "root stands on a device" trade-off the ADR already accepts, but with funds attached, which sharpens the not-done item "a device cannot be listed, named, or removed."

These compose rather than compete: 3 for the user's main funds, 1/4 for low-value signing without wallet-popup friction, 2 for the node acting on its own behalf.

## Chain data as attestations

A `qntx-evm` / `qntx-solana` plugin ingests the way `qntx-atproto` does — Pulse-scheduled sync creating attestations:

- Balances and reads: `[0xdead…] is [holder-of erc20:usdc {balance}] of [ethereum:mainnet] by [rpc:alchemy] at [block-time]`. The actor is the *observer*, not the address — provenance is which RPC said so. Two providers attesting the same read is convergent verification for free; disagreement is visible instead of silently resolved.
- Events: transfers, swaps, mints as attestations per log.
- Volume is the constraint: a popular token's transfer stream is unbounded. Bounded storage + sigma distillation (min/max/sum/count over evicted transfers) is the existing answer — portfolio history compresses into statistical shape instead of being dropped.
- Finality: attest at observation with block depth in context; a finalized observation supersedes a tentative one. No mutation needed.

## Smart contract protocol integration

**Ethereum:** JSON-RPC + ABI decoding. ERC-20/721/4626 as the first vocabularies; then protocol adapters (Uniswap quotes, Aave positions, Safe multisig). ENS resolution is a binding in ADR-030's sense — a name attesting to an address. Safe is the interesting one for posture 2: the node DID's chain key as one signer in a user-controlled multisig bounds custodial risk structurally.

**Solana:** JSON-RPC + websocket subscriptions. SPL Token first; Anchor programs after. An Anchor IDL is a machine-readable type vocabulary — the natural bridge to attested types (⊢): a program's types become real in QNTX because the plugin attested them from the IDL, not because a schema was hand-declared.

**Transaction submission is a ceremony.** ADR-030's binding ceremony inverts cleanly: there, "the browser proposes no part of the answer it is going to be judged on"; for a transaction, the node proposes (builds, simulates, prices the tx) and the *user* judges — a glyph draws calldata decoded against the ABI, the wallet signs, the node broadcasts and attests the outcome. Simulation-before-signature is the chain analogue of the consent screen. Pulse budgets extend to gas: a spending cap per namespace is the same shape as an API budget.

**Glyphs:** wallet glyph (balances across chains, one triplet per holding), transaction-ceremony glyph, contract glyph (ABI/IDL-derived read/write surface), portfolio composition on the canvas melding chain data with everything else QNTX knows.

## Open questions

**Does QNTX ever hold a spending key?** Posture 2 is a line-crossing. Read-only indexing plus external-wallet signing (postures 3 + ceremony) delivers most of the value with none of the custody liability. Is node custody in scope at all, or only behind multisig?

**secp256k1 in the core?** Ethereum support in laye means a second curve in `ats-id` and the WASM. Worth it, or is Ethereum external-wallet-only while Solana gets native keys?

**Is an on-chain address an Actor or a Subject?** Attestation actors are currently QNTX identities. Making `0xdead…` an actor lets chains speak in the quintuple directly; keeping addresses as subjects keeps actors meaning "who observed." The observer model above assumes the latter — is that right?

**Ingestion scope.** Whole-chain indexing is a data-center problem. Wallet-centric indexing (addresses the User has bound, contracts explicitly watched) is the tractable slice — is that the permanent stance or a starting point?

**One plugin per chain, or one chain plugin?** EVM chains share everything (one plugin, chain-id in context); Solana shares nothing with EVM. Two plugins minimum, with `ChainRPCProvider` as the common service surface — does the service interface abstract over both, or is chain-abstraction a false economy?

**Regulatory posture.** Hosting signing (postures 1/2/4) may make a deployment money-transmission-adjacent depending on jurisdiction. Read-only + ceremony-with-external-wallet does not. This bounds which postures ship by default.
