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
  names is not served — not ROOT's, absent.

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
