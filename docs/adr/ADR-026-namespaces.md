# ADR-026: Namespaces

Date: 2026-08-05
Status: Half-implemented. See Not done.

## Decision

### A namespace is named

A namespace is a name. An identity inside QNTX owns it. A DID outside QNTX
proves you have access to that identity.

An owner holds many.

"Creating a namespace, makes the writer the owner of one."

"`system` and `default` are namespaces that exist by default."

"A namespace is defined by it's `ns.toml`"

### A namespace is enabled or disabled

Data never leaves. A newer record supersedes an older one, and both stay.

A namespace is created enabled and can be disabled. A disabled namespace refuses
reads. Enabling it again opens the same bytes.

### Reach is granted

A User reaches a namespace through a permission granted and struck from the
root side — a relation between the User and the namespace (ADR-031).
Disabling a namespace refuses reads. A login is a session with the node and
stands regardless.

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

## Won't do

Namespaces on SQLite. If it happens it is its own ADR and its own scope.

## Not done

There is no `ns.toml`. The record is `self.json`, holding an owner did, who
minted it and when.

Existence is derived from what is stored. `list()` globs for objects and calls
whatever it finds a namespace, so a prefix holding data and no record is one,
and asking storage for a record that is not there makes a 404 the answer.

Nothing carries an enabled state. A namespace is created and listed; the record
has no field for disabled, and no read path consults one.

Clicking a namespace highlights a tile in the namespaces bar. A session acts in
the default namespace, whatever is highlighted.

Attestations are stored per namespace. Watchers, schedules, the canvas and the
glyph handlers are stored once and every namespace reads the same ones, so a
watcher in A fires on an attestation in B — the thing this ADR decided against.

Reach is a granted relation (ADR-031). What grants and strikes it is unbuilt;
disabling a namespace refuses reads, and a login stands.
