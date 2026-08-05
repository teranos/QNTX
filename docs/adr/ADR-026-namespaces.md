# ADR-026: Namespaces

Date: 2026-08-05
Status: Proposed

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
may exist locally. Everything that follows is consequence.

Attestations are **in** a namespace: the namespace whose key signed them. Because
[AXIOMS.md](../AXIOMS.md) holds that *everything flowing through the DAG is an attestation*,
namespacing attestations namespaces everything that flows.

### Namespaces are flat

A namespace is a keypair, and keypairs have no hierarchy. `SBVH` and `SBVH-WORK` are two unrelated
namespaces whose names happen to look similar. There is no parent, no inheritance, no path syntax,
no derived-key ceremony.

Grouping is not absent — it is attested, like everything else. `did:key:z6MkB is member of
did:key:z6MkA by <peer>` is an ordinary attestation, and the graph answers containment questions the
same way it answers every other question. Hierarchy expressed as data rather than as structure stays
revisable; hierarchy carved into key derivation does not.

### `by` becomes the namespace

`Actors` stops being plural, self-asserted free text and becomes the namespace DID. One identity
axis, not two.

The grammar `[Subject] [Predicate] [Context] by [Actor] at [Temporal]` survives; the `by` slot
changes meaning from *a string claiming to be who made this* to *the identity that provably did*.
The alternative — actor asserted alongside namespace proven — keeps two things that answer the same
question and guarantees they will drift apart.

`Contexts` is untouched and must not be confused with this. Context is a grammatical slot, the
object of the claim: in *"ENTITY-A is member of ORG-1"*, `ORG-1` is the context. It has never been a
scope, despite the field comment that said so.

### Edges carry their own origin

Per-glyph provenance moves off `by` onto a dedicated origin key.

Today an edge's subscription is filtered on actor — `glyph/handlers/canvas.go:499` sets
`w.Filter.Actors = []string{fmt.Sprintf("glyph:%s", edge.From)}`, which is the mechanism behind the
axiom *the edge is the watcher*. That makes actor the DAG's routing key, and routing keys are
per-glyph, numerous, and ephemeral. Identity is none of those things.

Separating them keeps the axiom intact with a key that is honest about what it is. A glyph is not an
identity; it is where something came from.

### Foreign attribution is provenance, not identity

Ingested claims name sources whose keys we will never hold — `ats/doc.go:11-13` carries
`by hr-system@company` and `by dr.smith@hospital` as canonical examples. Under this decision the
signer is ours, and the attributed source becomes provenance metadata alongside the `source` and
`source_version` fields that already exist on `AsCommand` (`ats/types/attestation.go:40-41`).

This is the honest reading of what ingest was ever doing. We were never able to verify that
`hr-system@company` said anything. Recording it as *our* claim about what a source reported, rather
than as that source's own attestation, matches what actually happened.

### The namespace signs, not the node

`Signer` in `ats/signing/signing.go` holds the node's key and stamps `SignerDID` with the node DID.
Under this decision the namespace's key signs, and the node key demotes to transport and peer
authentication — the role [vision/identity.md](../vision/identity.md) already gives it in the
delegation chain.

### Relationship to ADR-010

[ADR-010](ADR-010-identity-system.md) defines three orthogonal ID layers: Vanity ID (human-readable
subject handles), ASUID (attestation identity), Node DID (signer identity). A namespace name is a
**fourth layer**, not a fifth use of the first.

ADR-010 scopes vanity IDs to subjects and states they "do not apply to predicates, contexts, or
actors," and its step 5 — deriving a handle from a human name — is recorded won't-do. A namespace
name is not derived from anything. It is chosen, and bound to a DID by peer attestation. Different
mechanism, different layer, no contradiction.

## Consequences

### Positive

- **The user question dissolves rather than blocks.** No separate account system is needed. A user
  is a namespace with biometric keys, reachable through the WebAuthn PRF path already planned.
- **The axis already exists.** `SignerDID` is on every attestation and populated by working signing
  code. This is largely naming and unlocking, not new plumbing.
- **Verifiable containment.** "This attestation is in namespace X" is a signature check, not a
  database assertion. It survives sync, and a peer can confirm it without contacting the origin.
- **Identity stays close to the user.** Flat, self-generated, no authority to issue or revoke —
  consistent with [vision/identity.md](../vision/identity.md).
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
  What happens to them is deferred, and deferring it is a debt, not a decision.

### Neutral

- **Flatness is a constraint, not a limitation.** Nesting remains expressible as attested membership;
  it simply is not expressible as a name.
- **Namespace count is unbounded but human-scale.** Because glyph provenance moves off `by`,
  namespaces do not proliferate per canvas object.
- **`Contexts` is unaffected.** Only its misleading comment changes.

## Out of Scope

Named here so the boundary is explicit. None of it is decided by this ADR:

- The origin field and the watcher-filter migration.
- `Actors []string` collapsing to a single DID, and the junction/counter schema changes.
- Multi-row `node_identity`, and how a namespace key is created, stored, or unlocked.
- User DIDs via WebAuthn PRF ([#577](https://github.com/teranos/QNTX/issues/577)), delegations
  ([#578](https://github.com/teranos/QNTX/issues/578)), peer auth
  ([#579](https://github.com/teranos/QNTX/issues/579)), name attestations
  ([#580](https://github.com/teranos/QNTX/issues/580)).
- Migration of existing attestations, and whether a default namespace exists.
- Whether namespace membership is queryable through `ax`, and with what syntax.

## References

- [vision/identity.md](../vision/identity.md) — decentralized identity, name→DID binding, delegations
- [ADR-010](ADR-010-identity-system.md) — vanity IDs, ASUIDs, node DIDs
- [AXIOMS.md](../AXIOMS.md) — everything flowing through the DAG is an attestation
- [ADR-009](ADR-009-edge-based-composition-dag.md) — edge-based composition DAG
- `ats/signing/signing.go` — ed25519 signing, `SignerDID`
- `server/nodedid/` — node DID infrastructure
