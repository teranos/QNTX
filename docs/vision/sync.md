# Attestation Sync

**Status:** Wired. Merkle tree, content hashing, symmetric reconciliation protocol, WebSocket peering, observer registration, and startup backfill are all in place. Ready for `make wasm && make dev`.

Attestations are claims. Claims are useful only if they reach the nodes that need them. Sync is how claims propagate — not through a central server pushing updates, but through peers that compare state and exchange what differs.

## What Exists

The sync foundation has three layers:

**Content-addressed attestations** (`crates/qntx-core/src/sync/content.rs`). Every attestation gets a SHA-256 digest of its semantic fields — subjects, predicates, contexts, actors, timestamp, source. The ASID, attributes, and creation time are excluded. Two nodes that independently create the same claim produce the same hash. This is the deduplication primitive: content identity, not storage identity.

**Merkle tree grouped by (actor, context)** (`crates/qntx-core/src/sync/merkle.rs`). Attestations are organized into groups mirroring bounded storage — one group per (actor, context) pair. Each group hashes its leaves (content hashes) into a group hash. All group hashes roll up into a single root hash. Two nodes with identical attestation sets produce the same root. When roots differ, the tree structure narrows the diff: compare group hashes to find which (actor, context) pairs diverge, then exchange only those attestations.

**Symmetric reconciliation protocol** (`sync/peer.go`, `sync/protocol.go`). Neither side is server or client. Both run the same state machine simultaneously:

| Phase | Both send | Purpose |
|-------|-----------|---------|
| 1 | `sync_hello` (root hash) | Quick check — if roots match, done |
| 2 | `sync_group_hashes` | Exchange all (group key hash → group hash) pairs |
| 3 | `sync_need` | Each side says which groups it wants |
| 4 | `sync_attestations` | Each side sends what the other asked for |
| 5 | `sync_done` | Stats: how many sent, how many received |

The `Conn` interface (`sync/peer.go:19`) abstracts transport — tests use channel pairs (`chanConn`), production will use WebSockets. The `SyncTree` interface (`sync/tree.go`) abstracts the Merkle tree — tests use a Go in-memory mock (`memTree`), production delegates to Rust via wazero WASM (`sync/tree_qntx.go`). The `TreeObserver` (`sync/observer.go`) hooks into the storage layer: every new attestation is automatically inserted into the Merkle tree under all its (actor, context) groups.

All crypto lives in Rust. Go never computes hashes — it passes JSON to the WASM module and gets hex strings back.

## The Simplest Peer

Before mesh networking, gossip protocols, or automatic discovery: `git fetch`.

You know your laptop's QNTX address. You know your phone's. You tell one to sync with the other. That's it. No discovery, no persistent connections, no topology.

```
POST /api/sync
{ "peer": "https://phone.local:877" }
```

Both nodes run `Peer.Reconcile()` over a WebSocket. The protocol handles everything — compare roots, narrow to divergent groups, exchange attestations. The connection closes. Next time you want to sync, you do it again.

This is the git-remote model. You maintain a list of peers you care about. You sync when you want to. If a peer is offline, you try later. The Merkle tree makes this cheap: if nothing changed, the root hashes match and zero bytes transfer after the initial hello.

### Why start here

- **No infrastructure**: No registry, no rendezvous server, no DHT. Just two nodes and a URL.
- **Testable**: The entire protocol is tested with in-memory mocks (11 Go tests + 21 Rust tests). Adding a WebSocket handler and a `POST /api/sync` route is plumbing.
- **Covers the most common case**: Multi-device sync. Your laptop and phone are a two-node network. You don't need mesh networking to keep them in sync.
- **Delay-tolerant by design**: Jenny on the Northern Line (see [mobile.md](./mobile.md)) syncs at each station. The protocol doesn't care that 3 minutes passed between connections. Roots differ → exchange what changed → done.

### What this doesn't solve

- How do nodes find each other? (You tell them.)
- What if you want continuous sync instead of point-in-time? (You poll or add WebSocket push later.)
- What about nodes you don't know about? (You don't sync with them.)

These are real problems. The simplest peer is step one — useful immediately for multi-device sync. The answers to these problems live in the network layer.

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

## Trust & Verification

The reconciliation protocol transfers attestations. It doesn't ask whether you *should* accept them. Trust is a layer above sync.

### The question

When a peer sends you an attestation — "user-1 is a member of team-eng, attested by hr-system" — do you store it?

### Accept everything

The simplest model. Useful for personal multi-device sync where both sides are you. Your laptop trusts your phone because both are your devices. You chose to sync with this peer, so you accept what they have.

Sufficient for the git-remote model. Trust follows from the explicit decision to add a peer. Breaks down when sync becomes automatic — discovery-based peering means "I synced" no longer implies "I trust."

### Trust on first use (TOFU)

The first time you see an actor, you accept their attestations. After that, you expect consistency. SSH does this with host keys: the first connection is a leap of faith, every subsequent connection validates against the stored fingerprint.

TOFU is pragmatic. Zero configuration, covers the common case where the first introduction is legitimate. Vulnerable to a compromised first contact, but the threat model for most deployments (personal devices, small teams) makes this unlikely.

A QNTX-specific variant: TOFU at the context level. First time you see actor `hr-system` attesting in context `team-eng`, you pin that relationship. If `hr-system` later appears in `team-finance`, that's a new trust decision.

### Actor signatures (Ed25519)

Attestations carry a cryptographic signature from the actor's private key. Any node can verify using the public key. If it checks out, the attestation is authentic regardless of which peer delivered it, which path it took, or how many hops it traversed.

This is the strongest model — attestations become self-authenticating. They can propagate through untrusted intermediaries and still be verified at the destination. On Reticulum, this is especially powerful: an attestation might traverse multiple links and nodes before reaching you, and signature verification means you don't need to trust any of them.

The cost is key management. Where do public keys come from? How are compromised keys handled? If actor identity converges with Reticulum destination identity, Reticulum's key exchange provides a foundation — but the mapping between "I can reach this destination" and "I trust this actor's claims" still needs explicit policy.

### Context-scoped trust

Trust doesn't have to be all-or-nothing. You might accept attestations from actor `hr-system` in context `team-eng` but not in context `team-finance`. The Merkle tree already groups by (actor, context) — trust decisions follow the same structure.

During the Need phase of reconciliation, a node can decline to request groups for contexts it doesn't trust — even if those groups diverge. This means partial sync is possible without protocol changes. The peer sends group hashes for everything; you request only the slices you want.

### Content-addressed deduplication as implicit verification

If two independent nodes attest the same claim with the same semantic content, they produce the same content hash. The Merkle tree deduplicates naturally — inserting a duplicate is a no-op.

This isn't trust in the cryptographic sense. It's convergence: independent observations of the same fact produce the same representation. Two witnesses don't need to coordinate — their attestations match because reality matches.

### Open questions

- **Key distribution**: How does a node learn another actor's public key? If actors are Reticulum destinations, the key is the address. If not, some out-of-band mechanism is needed.
- **Revocation**: Compromised keys need revocation. CRLs? Short-lived keys? Key rotation as an attestation?
- **Delegation**: Can actor A authorize actor B to attest on their behalf? Needed for organizational hierarchies.
- **Partial trust**: Accept an attestation but mark it unverified? Store it in the tree but flag it for the consumer?

## Conflict & Convergence

Attestations are append-only claims. They're not mutable state. This distinction eliminates most "conflict" in the distributed systems sense.

### Why there are no conflicts

Two nodes creating different attestations about the same subject isn't a conflict. It's two claims.

Node A attests: "user-1 has role admin" (by hr-system)
Node B attests: "user-1 has role viewer" (by security-audit)

After sync, both nodes have both attestations. Neither overrides the other. The consumer decides how to interpret multiple claims — that's application logic, not sync logic.

This is fundamentally different from mutable-state replication where two nodes updating the same row creates a conflict. Attestations don't update — they accumulate.

### The Merkle tree is a set

The tree computes hashes over `BTreeSet<[u8; 32]>` — an ordered set of content hashes. Sets under union are commutative and idempotent:

- A ∪ B = B ∪ A. Sync order doesn't matter.
- A ∪ A = A. Syncing twice changes nothing.

Any sync topology converges. A syncs with B, B syncs with C, C syncs with A — everyone ends up with the same set. No vector clocks, no causal tracking. The data structure guarantees convergence.

On a Reticulum mesh, this property is critical. Attestations might reach different nodes through different paths in different orders. The set-union property means the final state is the same regardless of propagation order.

### Revocations

If attestations are append-only, how do you retract a claim? You attest the retraction.

"user-1's membership in team-eng is revoked, attested by hr-system"

This is itself an attestation. It propagates through sync like any other. The consumer sees both the original claim and its revocation and acts accordingly.

The Merkle tree doesn't distinguish between claims and revocations — both are content-hashed leaves. The semantic meaning lives in the attestation's predicates, not in the tree structure. This keeps sync simple: it moves data, it doesn't interpret it.

Open question: should revoked attestations eventually be pruned from the tree? The `MerkleTree::remove()` method exists in Rust. Bounded storage already caps group sizes. The machinery for pruning exists; the policy is the open question.

### Tombstones and garbage collection

An append-only set grows without bound. Several mechanisms limit growth:

- **Bounded storage**: The existing system caps attestations per (actor, context) group. The Merkle tree mirrors these groups. When bounded storage evicts, the tree removes.
- **Time-based expiry**: Prune attestations older than a threshold. Requires consistent thresholds across peers — if one prunes and another hasn't, the next sync re-sends the pruned attestations.
- **Explicit tombstones**: A revocation marks the original for removal. After propagation, both can be pruned. Knowing when propagation is complete is hard in a decentralized system.

## Topology

The spectrum runs from fully manual to fully automatic. A single QNTX deployment might use different modes for different peers.

### Manual: git-remote

Explicit peer list. Sync on demand. Zero infrastructure beyond the nodes.

```
peers:
  - name: phone
    url: https://phone.local:877
  - name: lab-server
    url: https://lab.university.edu:877
```

Best for: personal devices, small teams, high-trust environments.

### Scheduled: cron-sync

Same peer list, automated on an interval. If a peer is offline during one cycle, the next catches up — the protocol is stateless between reconciliations.

Best for: teams wanting continuous-ish sync without persistent connections.

### Reactive: WebSocket push

Persistent connections. `TreeObserver.OnAttestationCreated()` fires on every insert — adding "notify connected peers" triggers immediate reconciliation.

Best for: multi-device real-time sync. Changes propagate within seconds.

### Interest-based: context clustering

Nodes sharing contexts form natural sync groups. A node carrying `team-eng` attestations discovers other `team-eng` nodes and syncs selectively — requesting only groups matching shared contexts, ignoring the rest.

On Reticulum, this could use announce/discover: a node announces a hash of its context list. Other nodes with overlapping contexts respond. The Merkle tree's group structure supports partial sync — you exchange group hashes for everything but only request groups you care about.

Best for: organizational boundaries. Engineering shares `team-eng` with the company instance but keeps `team-eng-internal` local.

### Gossip: epidemic dissemination

Each node periodically picks a random known peer and syncs. Attestations spread probabilistically. The Merkle tree makes each round efficient — matching roots cost one message, diffs transfer only the delta.

On a Reticulum mesh, gossip happens naturally. Every node routes, so attestations propagate through the mesh as nodes reconcile with their neighbors. A field researcher syncs with a local relay; the relay syncs with the university server; the university syncs with a collaborator's instance. No node is configured as a "relay" — every node that syncs becomes one.

Best for: large or loosely-connected networks.

### Multi-device as the simplest mesh

Your laptop, phone, and tablet form a three-node network. Any two that can see each other sync directly. If your laptop syncs with your phone and your phone syncs with your tablet, all three converge.

On Reticulum, these three devices are three destinations on a mesh. They find each other automatically if they share a transport (local WiFi, Bluetooth). The reconciliation protocol runs over Reticulum links. No URLs to configure, no IP addresses to know — just cryptographic identities that discover each other.

Jenny's commute: her phone is a Reticulum destination. The office server is a Reticulum destination. When she reaches a station, her phone discovers the server over WiFi (or cellular, or whatever link is available), establishes a Reticulum link, and reconciles. The transport changed three times during her commute — WiFi at Morden, cellular at Stockwell, WiFi again at Old Street. The reconciliation protocol didn't notice.

## What Comes Next

The server wiring is complete:
- ~~WebSocket handler wrapping `gorilla/websocket` as a `Conn`~~ → `server/sync_handler.go`
- ~~`POST /api/sync` route initiating reconciliation with a peer URL~~ → `server/routing.go`
- ~~`TreeObserver` registered at server startup~~ → `server/init.go:setupSync()`
- ~~Backfill Merkle tree from existing attestations~~ → `server/init.go:backfillSyncTree()`

Next:
1. **Frontend sync trigger**: Button or command that calls `POST /api/sync {"peer":"..."}` — the protocol is ready, needs a way for users to invoke it
2. **Scheduled sync**: Use Pulse to periodically reconcile with configured peers (`am.toml` sync.peers)
3. **Reactive push**: On `TreeObserver.OnAttestationCreated()`, notify connected sync peers to trigger immediate reconciliation
4. **Reticulum integration**: A parallel `Conn` implementation over Reticulum links. The protocol doesn't change — only the transport beneath `Conn`

## Related

- [Mobile Vision](./mobile.md) — Jenny's tube journey depends on sync across intermittent connectivity
- [Glyphs](./glyphs.md) — attestable glyph state syncs through the same Merkle tree
- [Time-Travel](./time-travel.md) — navigating attestation history across synced nodes
- [Reticulum](https://reticulum.network/) — the cryptographic mesh network QNTX aims to operate on
