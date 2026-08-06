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

### A namespace is an identity

Not a container that has an owner. An identity.

A namespace is a keypair. Its name is what people call it — an alias bound to the key by attestation,
carrying no authority of its own.

There is no separate concept of a user, and there does not need to be.

### Signature says who, namespace says where

Two facts, not one. An attestation is **in** the namespace it was written to, and **signed by**
whoever wrote it. `SignerDID` keeps meaning the signer; the namespace is its own field.

So a project can have members without anyone sharing a key. Alice signs with her key and the
attestation lives in the project. Authorship stays exact — every claim says who made it — and the
project still holds its own data.

The namespace is part of the signed payload. Moving an attestation to another namespace would break
its signature, so the namespace is decided at write time and decided forever.

### A project is a namespace

`namespace` is the system word. `project` is the human word. Same object.

A USER never meets the concept — they are in a project. Namespaces are visible, and manageable, at
SUPER (see [ADR-027](ADR-027-access-levels.md)).

### Nothing crosses

A namespace is closed. A canvas lives in one namespace and only that one — not shared, not visible
from another, not partly in two.

The system namespace is the node: `node_identity`, the row keyed `'self'`. It has no canvas.

A node therefore starts with two namespaces — the system one, and a default one where a person
works. The default namespace is the default project, and today's canvas is its canvas.

### Namespaces are flat

A namespace is a keypair, and keypairs have no hierarchy. `SBVH` and `SBVH-WORK` are two unrelated
namespaces whose names happen to look similar. There is no parent, no inheritance, no path syntax,
no derived-key ceremony.

### `by` is the signer

The `by` slot stops being a string claiming who made a thing and becomes the identity that provably
did. Asserted becomes proven.

`Actors` was plural, free text and unverifiable. It is the signer.

`Contexts` is untouched. Context is a grammatical slot, the object of the claim: in *"ENTITY-A is
member of ORG-1"*, `ORG-1` is the context. It has never been a scope, despite the field comment that
said so.

### Edges carry their own origin

Per-glyph provenance moves off `by` onto a dedicated origin key.

Today a canvas meld edge's subscription is filtered on actor — `glyph/handlers/canvas.go:499` sets
`w.Filter.Actors = []string{fmt.Sprintf("glyph:%s", edge.From)}`. That makes actor a routing key on
the canvas meld path, and routing keys there are per-glyph, numerous, and ephemeral. Identity is
none of those things.

### Foreign attribution is provenance, not identity

Ingested claims name sources whose keys we will never hold — `ats/doc.go:11-13` carries
`by hr-system@company` and `by dr.smith@hospital` as canonical examples. Under this decision the
signer is ours, and the attributed source becomes provenance metadata alongside the `source` and
`source_version` fields that already exist on `AsCommand` (`ats/types/attestation.go:40-41`).

## References

- [vision/identity.md](../vision/identity.md) — decentralized identity, name→DID binding, delegations
- [ADR-010](ADR-010-identity-system.md) — vanity IDs, ASUIDs, node DIDs
- `ats/signing/signing.go` — ed25519 signing, `SignerDID`
- `server/nodedid/` — node DID infrastructure
