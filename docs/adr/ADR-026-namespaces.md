# ADR-026: Namespaces

Date: 2026-08-05
Status: Draft — in progress.

## Context

QNTX has no concept of different users.

## Decision

### A namespace is an identity

Namespace is identity.

There is no separate concept of a user.

### A namespace is named, and the DID says whose it is

A namespace is a name. The DID it carries is ownership, not identity. Keying a
namespace by a DID would let an owner hold exactly one, and SUPER creates them in
the plural.

Creating one writes that ownership, and the write is what makes it exist. A
namespace is the top-level prefix at the storage location — there is nothing
else to create, and nothing under the prefix means nothing on disk.

### A namespace is enabled or disabled

Data never leaves. A newer record supersedes an older one, and both stay.

A namespace is created enabled and can be disabled. A disabled namespace refuses
reads. Enabling it again opens the same bytes.

### A namespace is a home

An identity lives in a namespace. While the only namespace it lives in is
disabled, it cannot log in — disabling reaches identity, not only data.

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

## Not done

Only the parquet backend has namespaces. Nothing in `db/sqlite/migrations/` or
`crates/ats-sqlite/` mentions one, so a SQLite node has a single universe and
the word is decoration there.

Nothing carries an enabled state. A namespace is created and listed; the record
has no field for disabled, and no read path consults one.

A token names a single namespace. `tokens.rs` holds `namespace: String` and
`handlers_tokens.go` reads one `req.Namespace`, so one-or-more has nowhere to
live yet. Minting also names it without checking it against anything, and
`Middleware` puts it on the `Caller` where no handler reads it.

A token minted for `X` is now refused rather than served `default`. The process
opens one attestation store and pins it to `NamespaceDefault`, so nothing routes
a caller anywhere else; reading and writing the wrong namespace while reporting
success was worse than an absent control. Refusing is what the boundary costs
until the store is resolved per caller instead of at construction.

An identity has no home. Nothing records which namespace an identity lives in,
so disabling one cannot yet reach a login.
