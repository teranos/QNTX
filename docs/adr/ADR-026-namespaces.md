# ADR-026: Namespaces

Date: 2026-08-05
Status: Draft — in progress.

## Context

The word "namespace" appears nowhere in QNTX — no type, no column, no config key. The thing it names
is nevertheless already half-built, and the question that appears to block it turns out to be the
same question wearing a different hat: **QNTX has no concept of different users.**

Today the system has exactly one identity, and it is the machine.

- `node_identity` (`db/sqlite/migrations/039_create_node_identity.sql`) holds one row. Its primary
  key defaults to the literal string `'self'`. It carries an ed25519 keypair and a `did`.
- `webauthn_credentials` (`038_create_webauthn_credentials.sql`) has **no `user_id`**. Passkeys are a
  door lock that does not record who walked through.
- The actor on an attestation is `ats+$USER@$(hostname)` — see `formatActor` in
  `ats/interfaces.go:88`, fed by `getSystemActor` reading `$USER` and `os.Hostname()`. Nothing signs
  it, nothing verifies it, nothing can deny it.

Meanwhile the axis a namespace would occupy already exists in the data. `ats/signing/signing.go`
signs every locally-created attestation, and `As.SignerDID` (`ats/types/attestation.go:28`) records
which identity vouches for it. The column is there. It has exactly one possible value, because there
is exactly one key.

[vision/identity.md](../vision/identity.md) already states the resolution without using the word:

> A name→DID binding isn't stored in a registry. It's attested by peers.

That is a namespace. Which means users were never a prerequisite for namespaces — **users were
always going to be namespaces.** A human is a namespace whose keys are held by biometrics; a node is
a namespace holding its own; an agent is a namespace holding a delegated one. The DID does not care
which. Waiting for a user system before defining namespaces would be waiting for the same thing
twice.

This ADR settles what a namespace *is* and resolves the identity question. It deliberately does not
settle blast radius or migration; those belong to the implementation branches.

## Decision

### A namespace is a name→DID binding

The DID is the namespace. The name is a human-readable alias bound to it by attestation, carrying no
authority of its own.

This is not a new primitive. It is a name for `SignerDID`, plus the admission that more than one key
may exist locally.

Attestations are **in** a namespace: the namespace whose key signed them.

### Namespaces are flat

A namespace is a keypair, and keypairs have no hierarchy. `SBVH` and `SBVH-WORK` are two unrelated
namespaces whose names happen to look similar. There is no parent, no inheritance, no path syntax,
no derived-key ceremony.

### `by` becomes the namespace

`Actors` stops being plural, self-asserted free text and becomes the namespace DID. One identity
axis, not two.

The grammar `[Subject] [Predicate] [Context] by [Actor] at [Temporal]` survives; the `by` slot
changes meaning from *a string claiming to be who made this* to *the identity that provably did*.

`Contexts` is untouched. Context is a grammatical slot, the object of the claim: in *"ENTITY-A is
member of ORG-1"*, `ORG-1` is the context. It has never been a scope, despite the field comment that
said so.

### Edges carry their own origin

Per-glyph provenance moves off `by` onto a dedicated origin key.

Today an edge's subscription is filtered on actor — `glyph/handlers/canvas.go:499` sets
`w.Filter.Actors = []string{fmt.Sprintf("glyph:%s", edge.From)}`, which is the mechanism behind the
axiom *the edge is the watcher*. That makes actor the DAG's routing key, and routing keys are
per-glyph, numerous, and ephemeral. Identity is none of those things.

### Foreign attribution is provenance, not identity

Ingested claims name sources whose keys we will never hold — `ats/doc.go:11-13` carries
`by hr-system@company` and `by dr.smith@hospital` as canonical examples. Under this decision the
signer is ours, and the attributed source becomes provenance metadata alongside the `source` and
`source_version` fields that already exist on `AsCommand` (`ats/types/attestation.go:40-41`).

## Consequences

### Positive

- **The user question dissolves rather than blocks.** No separate account system is needed. A user
  is a namespace with biometric keys, reachable through the WebAuthn PRF path already planned.
- **The axis already exists.** `SignerDID` is on every attestation and populated by working signing
  code. This is largely naming and unlocking, not new plumbing.
- **Two words stop competing.** Actor and signer answered the same question differently. Now there
  is one answer.

### Negative

- **The DAG's routing key moves.** `glyph/handlers/canvas.go:499` is load-bearing.
  `server/prompt_handlers.go:540` and `qntx-plugins/qntx-openrouter/handlers.go:488` both mint
  `"glyph:" + glyphID` actors. `glyph/handlers/subscriptions_test.go:155-156` and
  `glyph/handlers/edge_cursor_test.go:51,90` assert on `glyph:`-prefixed actor filters. All of it
  must move together or melding breaks.
- **`Actors` is plural and required.** The field is `validate:"required,min=1"`
  (`ats/types/attestation.go:22`). Collapsing to a single DID is a schema change reaching
  `attestation_actors` and its indexes (`048_create_attestation_junction_tables.sql`).
- **Enforcement counters change meaning.** `enforcement_actor_context`,
  `enforcement_actor_contexts`, `enforcement_entity_actors` and its detail table
  (`049_create_enforcement_counters.sql`) are keyed on actor. Their `(actor, context)` pairs re-read
  as `(namespace, context)`, and the configured limits — `actor_context_limit`,
  `actor_contexts_limit`, `entity_actors_limit` in `am.toml` — change what they bound.
- **Documented conventions break.** The `glyph:{id}` actor convention and the `attest()` default of
  `["glyph:{glyph_id}"]` are both specified in [GLOSSARY.md](../GLOSSARY.md). The `ats/doc.go`
  examples stop being valid.
- **Every existing attestation predates this.** They carry free-text actors and a node signature.
- **Flat rules out nesting**, and nothing replaces it.

## Out of Scope

This ADR settles what a namespace is and the identity question. Not decided here:

- The origin field and the watcher-filter migration.
- `Actors []string` collapsing to a single DID, and the junction/counter schema changes.
- Multi-row `node_identity`, and how a namespace key is created, stored, or unlocked.
- Migration of existing attestations.

## References

- [vision/identity.md](../vision/identity.md) — decentralized identity, name→DID binding, delegations
- [ADR-010](ADR-010-identity-system.md) — vanity IDs, ASUIDs, node DIDs
- [AXIOMS.md](../AXIOMS.md) — everything flowing through the DAG is an attestation
- [ADR-009](ADR-009-edge-based-composition-dag.md) — edge-based composition DAG
- `ats/signing/signing.go` — ed25519 signing, `SignerDID`
- `server/nodedid/` — node DID infrastructure
