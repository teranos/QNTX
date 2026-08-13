# ADR-026: Namespaces

Date: 2026-08-05
Status: Draft — in progress.

## Context

QNTX has no concept of different users.

## Decision

### A namespace is an identity

Namespace is identity.

There is no separate concept of a user.

### Namespaces are their own universes

Namespaces don't mix and mesh. They are their own universes.

Namespaces have nothing to do with the attestation. A USER does not see what namespace or project
something belongs to. It just is, and it is not load-bearing within a namespace.

A watcher in namespace A does not fire on an attestation in namespace B. They are not the same
world.

### Nothing crosses

Things don't cross namespaces. A canvas lives in one namespace and only that one.

The system namespace is the node: `node_identity`, the row keyed `'self'`. It has no canvas.

There is a default namespace. It is the default project, and it is where the canvas lives that is
the default canvas of today.

### A project is a namespace

A project is a namespace. A USER does not see the concept — they experience being part of a project.

### Namespaces are flat

DIDs don't nest.

### `by` is the signer

`by` is the signer. It was never the namespace.

### Edges carry their own origin

Edges get their own origin field.

### Foreign attribution goes to attributes

Attribution on an ingested claim becomes provenance in attributes.
