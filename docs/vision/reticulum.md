# Sync Vision: Reticulum & Beyond

> For the current implementation, see [../sync.md](../sync.md).

The sync protocol is transport-agnostic by design. The `Conn` interface abstracts the wire — tests use channels, production uses WebSockets. This document is about what comes after WebSockets.

## Reticulum

[Reticulum](https://reticulum.network/) is Mark Qvist's cryptographic mesh networking stack. It's not an inspiration for QNTX sync — it's the network layer QNTX wants to live on. A QNTX node should be a Reticulum destination.

### Why Reticulum

The git-remote model works but it carries assumptions: nodes have IP addresses, DNS names, or URLs. You can reach them over HTTP. They're online when you want to sync. These assumptions hold for laptops and phones on the same WiFi network. They break everywhere else.

Reticulum eliminates these assumptions. Nodes are identified by cryptographic keys, not network addresses. Links form over whatever transport is available — TCP, UDP, I2P, LoRa, serial, pipes. Routing is automatic. If a link goes down, traffic reroutes. If a node is unreachable now, messages wait. There is no distinction between "infrastructure" and "endpoint" — every node routes.

This is the network that attestation sync needs. Not because QNTX is building a LoRa mesh (though it could), but because the properties Reticulum provides — cryptographic identity, transport agnosticism, delay tolerance, self-configuration — are exactly the properties that make decentralized attestation propagation work.

### QNTX nodes as Reticulum destinations (proposed)

A Reticulum destination is a cryptographic identity (X25519 for key exchange, Ed25519 for signing) that can receive messages over any Reticulum link. A QNTX node running on Reticulum would be a destination that accepts sync protocol messages.

The `Conn` interface is already transport-agnostic:

```go
type Conn interface {
    ReadJSON(v interface{}) error
    WriteJSON(v interface{}) error
    Close() error
}
```

A Reticulum link implements `Conn`. The reconciliation protocol runs over it unchanged. The same `Peer.Reconcile()` that works over WebSocket channels in tests works over a Reticulum link between two nodes on opposite sides of the planet — or across a LoRa radio in a field station with no internet.

The sync protocol was designed symmetric for exactly this reason. There is no "server" to connect to. Two destinations find each other on the Reticulum mesh, establish a link, and reconcile. Either side can initiate.

### Identity convergence (open question)

Reticulum identifies destinations by Ed25519/X25519 keypairs. QNTX identifies actors by name strings today, but the actor model is designed around the concept that an actor is an identity that can sign attestations.

The open question: should a QNTX actor *be* a Reticulum identity?

**Unified identity**: One keypair serves both as the Reticulum destination address and the QNTX actor signing key. Your node's identity on the mesh IS your identity as an attestor. This is the purest model — no mapping, no translation, no split between "network identity" and "application identity." An attestation signed by actor `abc123` can be verified by anyone who can address Reticulum destination `abc123`.

**Bridged identity**: QNTX actors and Reticulum destinations use separate keypairs, linked by an attestation ("Reticulum destination X belongs to QNTX actor Y, attested by Y"). This allows a single actor to operate multiple Reticulum destinations (laptop, phone, server — different network identities, same logical actor). It's more flexible but adds a layer of indirection.

**Hierarchical**: An actor has a root keypair (Ed25519, offline). Each device gets a sub-key derived from or signed by the root. The sub-key serves as the Reticulum destination identity. Attestations are signed by the sub-key but chain to the root actor identity. This is how Signal handles multi-device — and it means a compromised device key can be revoked without rotating the actor identity.

This doesn't need to be decided now. The sync protocol is identity-agnostic — it transfers attestations and checks content hashes. Identity verification is a layer above. But the architecture should leave room for Reticulum identity integration rather than making assumptions that preclude it.

### Delay tolerance

Reticulum is built for links that come and go. LoRa radios, intermittent WiFi, satellite uplinks with minutes of latency. Messages are stored and forwarded when links become available.

The Merkle reconciliation protocol already has this property. The tree encodes "what I have," not "what changed since last sync." Two nodes that haven't spoken in a week reconcile just as efficiently as two that spoke a minute ago. Each `Reconcile()` call is stateless — no session to maintain between syncs.

On Reticulum, this means a QNTX node on an intermittent link (a field station, a mobile device, a sensor network) participates in sync whenever its link is up. No special "offline mode" — the protocol inherently handles gaps. Jenny's tube journey isn't a special case to engineer around; it's the normal mode of operation for any Reticulum link.

### Transport diversity

A single QNTX node could be reachable over multiple Reticulum interfaces simultaneously: WiFi at home, cellular on the train, LoRa at the field station. Reticulum handles the routing — the node is the same destination regardless of which physical link carries the traffic.

This means attestation sync works across transport boundaries without QNTX knowing or caring. A field researcher's attestations might travel over LoRa to a gateway, then over TCP to a university server, then over WiFi to a colleague's laptop. The reconciliation protocol sees a `Conn` at each hop. The attestations arrive with the same content hashes regardless of path.

### What Reticulum doesn't solve

Reticulum provides the network. QNTX still needs to decide:

- **What to sync**: Which attestations to share with which peers (context-scoped trust).
- **When to sync**: On demand, on schedule, on change, or continuously.
- **Who to trust**: Reticulum authenticates the link (you're talking to the real destination). QNTX must decide whether to accept the attestations that arrive over it.

These are QNTX-layer decisions. Reticulum is the plumbing; QNTX is the semantics.

## Future Topology

### Interest-based: context clustering

Nodes sharing contexts form natural sync groups. A node carrying `team-eng` attestations discovers other `team-eng` nodes and syncs selectively — requesting only groups matching shared contexts, ignoring the rest.

On Reticulum, this could use announce/discover: a node announces a hash of its context list. Other nodes with overlapping contexts respond. The Merkle tree's group structure supports partial sync — you exchange group hashes for everything but only request groups you care about.

### Gossip: epidemic dissemination

Each node periodically picks a random known peer and syncs. Attestations spread probabilistically. The Merkle tree makes each round efficient — matching roots cost one message, diffs transfer only the delta.

On a Reticulum mesh, gossip happens naturally. Every node routes, so attestations propagate through the mesh as nodes reconcile with their neighbors. A field researcher syncs with a local relay; the relay syncs with the university server; the university syncs with a collaborator's instance. No node is configured as a "relay" — every node that syncs becomes one.

### Multi-device as the simplest mesh

Your laptop, phone, and tablet form a three-node network. Any two that can see each other sync directly. If your laptop syncs with your phone and your phone syncs with your tablet, all three converge.

On Reticulum, these three devices are three destinations on a mesh. They find each other automatically if they share a transport (local WiFi, Bluetooth). The reconciliation protocol runs over Reticulum links. No URLs to configure, no IP addresses to know — just cryptographic identities that discover each other.

Jenny's commute: her phone is a Reticulum destination. The office server is a Reticulum destination. When she reaches a station, her phone discovers the server over WiFi (or cellular, or whatever link is available), establishes a Reticulum link, and reconciles. The transport changed three times during her commute — WiFi at Morden, cellular at Stockwell, WiFi again at Old Street. The reconciliation protocol didn't notice.

## Open Questions

### Trust models

- **Key distribution**: How does a node learn another actor's public key? If actors are Reticulum destinations, the key is the address. If not, some out-of-band mechanism is needed.
- **Revocation**: Compromised keys need revocation. CRLs? Short-lived keys? Key rotation as an attestation?
- **Delegation**: Can actor A authorize actor B to attest on their behalf? Needed for organizational hierarchies.
- **Partial trust**: Accept an attestation but mark it unverified? Store it in the tree but flag it for the consumer?

### Garbage collection

An append-only set grows without bound. Several mechanisms limit growth:

- **Bounded storage**: The existing system caps attestations per (actor, context) group. The Merkle tree mirrors these groups. When bounded storage evicts, the tree removes.
- **Time-based expiry**: Prune attestations older than a threshold. Requires consistent thresholds across peers — if one prunes and another hasn't, the next sync re-sends the pruned attestations.
- **Explicit tombstones**: A revocation marks the original for removal. After propagation, both can be pruned. Knowing when propagation is complete is hard in a decentralized system.

## Related

- [Mobile Vision](./mobile.md) — Jenny's tube journey depends on sync across intermittent connectivity
- [Glyphs](./glyphs.md) — attestable glyph state syncs through the same Merkle tree
- [Time-Travel](./time-travel.md) — navigating attestation history across synced nodes
- [Reticulum](https://reticulum.network/) — the cryptographic mesh network QNTX aims to operate on
