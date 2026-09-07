# ADR-027: Permissions

Date: 2026-08-05
Status: Stub, except TOKATTEST. The statements are made; the phases say what is decided.

## Statements

- SUPER creates namespaces and disables them. Only a SUPER User owns one, and ownership is
  recorded on the namespace (ADR-031).
- The system namespace is not visible at all below SUPER. The default namespace is visible, as the
  default project.
- Data never leaves. A newer record supersedes an older one, and both stay.
- A disabled namespace refuses reads. Re-enabling it opens the same bytes again.
- A login is a session with the node and stands (ADR-031); reach into namespaces
  is a granted relation.
- **ROOT** goes beyond QNTX. It is a level of access you want on dev and not on prod.
- Visibility is per-namespace.
- Which levels reach which route is one table: `server/reach`. A path no line
  names is ROOT's and nobody else's.
- A role is lines in system, not a level in the binary (ADR-034).

## The credential does not carry the permission

"i want to be able to change it at will"

`Grant` fuses who the caller is with what they may do, so changing what a token
may do means minting a different one. A credential says who, a policy says what,
and they are edited apart — change the policy and every credential under it
changes at once, untouched.

The token record keeps identity and loses scope. Minting asks for a label, and
`515bedc5` removed the scope boxes because every answer was the same answer —
this is why they do not come back.

## Phases

### TOKATTEST — a token attests as itself

ATTESTOR is a token that can attest, minted by the User that owns it (ADR-031).

- Bound to creation through the API: `POST /api/attestations`. Watchers, plugins,
  the CLI and the WebSocket are untouched.
- The predicate list is a mutable field on the token record, edited after
  minting. Minting asks for a label. A stepping stone — the field leaves the
  credential in a later phase.
- `Grant.Namespace` becomes a list.
- Tokens minted before this keep their reach and keep naming their own actors.
  The list says which ones those are.
- The list shows the DID, the namespaces and the predicates. It fetches all four
  today and draws none.

"each token is its own actor in the predicate by"

Its own, which says whose it is rather than how many there are. Two actors can
make contradictory claims about the same subject and both are valid
(docs/attestation.md), so the token's DID leads and what a caller names stands
after it.

A node opens a namespace on the first request that names it, so a token is
minted for any namespace its minter is admitted to. A token reaching several
says which one a request is; a write lands somewhere definite or nowhere.

### 27-2 — ground

Policy is declared in ground's controls and attested into the node. The mutable
field from TOKATTEST leaves the credential and becomes one of those.

Blocked on: nothing in ground's evaluation path takes an actor. `scopeMatches`
takes a cwd, `evaluatePermission` takes a cwd and a command, `CheckFn` takes a
cwd and an input, and the actor on every attestation it emits is the literal
`ground`.

### 27-3 — every part of QNTX behind it

## Not conformed

The statements govern the admission path: a request is admitted, `storeFor`
decides its namespace from that admission, and a non-ROOT write cannot land in
`system` (`storeFor` refuses it without `MaySeeSystem`). A handler that wrote
through `storeIn` directly, rather than through admission, sidestepped this —
it wrote as the node into whatever namespace it named. Naming a namespace was
ambient: `storeIn` was a method on the server, and every handler holds the
server.

That is closed. `server/namespaces` holds the stores and `storeIn` is
unexported inside it, so no handler can name a namespace — it asks at a door,
and each door is a different promise about what stands behind the write.

- `Read` hands back something that cannot write at all. Every lookup the node
  makes in `system` — roles, words, reach lines, stand definitions — holds
  this, so none of them can write there. That is by type rather than by
  inspection.
- `Write` takes the admission as a parameter, so a write cannot be asked for
  without passing the thing that would refuse it. `system` needs
  `MaySeeSystem`.
- `WriteWhatTheNodeKnowsOfItself` is for a line about the node rather than
  about the world — a grant, a reach line, a word line, a stand's definition.
  These land in `system` whatever namespace their writer acts in, because
  writing there is not seeing there: a coordinator who may grant a role in
  their own namespace does not thereby see `system`. Who may write one is
  settled before the door, by the reach table for the route and by
  `mayGrantEvery` and `MayGrantRoles` in the handler.
- `WriteAsPublic` is the one door for input no admission stands behind — a
  stand's arrivals off the `ANYONE` route at `/s/` (ADR-035). It refuses
  `system` and `default` before there is a store to write to, so a second
  public writer cannot be added without that refusal.

`server/store_writers_test.go` reads the package and fails on a function that
asks for a store it can write and is not written down with what stands behind
it. Five do: `storeFor`, `handleCreateAttestation`, `writeStaandDef`,
`HandleStaand`, and `systemAttestor`.

That last one is the residue, and no boundary removes it.
`auth.Handler.attest` writes into `system` on the `/auth/…` routes, which are
`ANYONE` because logging in cannot ask you to be logged in. What bounds it is
not admission and not this package: the predicates are a closed vocabulary the
node writes about itself, and the actor is the node's own DID.
