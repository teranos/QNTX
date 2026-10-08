# ADR-026: Namespaces

Date: 2026-08-05
Status: Half-implemented. See Not done.

## Decision

### A namespace is named

A namespace is a name. An identity inside QNTX owns it. A DID outside QNTX
proves you have access to that identity.

"Creating a namespace, makes that user the owner of one."

"A namespace is defined and configured by it's `ns.toml`"

"`system` and `default` are namespaces that come into being during the [[ADR-033-first-time-setup]]"

"A namespace is created disabled and can be enabled by one of it's owners."

### Reach is granted

A User can reach a namespace through REACH granted and struck by ROOT, [[ADR-031-the-user]] 

Disabling a namespace refuses reads by all non-owners and writes for everyone. 
### Namespaces are their own universes

Namespaces don't mix and mesh. They are their own universes.

A USER does not see what namespace or project something belongs to. 
A watcher in namespace A does not fire on an attestation in namespace B. 
They are not the same world.

#### Nothing crosses

A canvas lives in one namespace and only that one.

The `system` namespace is the node: `node_identity`, the row keyed `'self'`.

"system namespace should have no canvas"

"For non-default namespaces the canvas needs to be explicitly created and named."

"watcher should be per namespace"
"schedules should be per namespace"

"not all namespaces need watchers enabled."
"not all namespaces need schedules enabled."

"not all namespaces need to have access to all plugins"
"not all plugins need to have access to all namespaces"

### Namespaces are flat

DIDs don't nest.

### `by` is the signer

`by` is the signer. 

### Foreign attribution goes to attributes

Attribution on an ingested claim becomes provenance in attributes.

## Not done

Nothing consults the enabled state. `ns.toml` carries it, and no read path
asks.

`list()` still globs for objects, so a prefix holding data and no `ns.toml` is
listed as a namespace nobody defined. The ones written before `ns.toml` are
those.

Clicking a namespace highlights a tile in the namespaces bar. A session acts in
the namespace its person stands in, which `i step` moves.

The canvas is one for the node, in `qntx-operational.db`.

Schedules are one table for the node. A schedule created during a sigil call
keeps its creator's namespace, and each run carries it to the plugin. Every
other schedule has none.

Embeddings are one store for the node.

Attestations and observers are per namespace.

Reach is a granted relation (ADR-031). What grants and strikes it is unbuilt;
disabling a namespace refuses reads, and a login stands.
