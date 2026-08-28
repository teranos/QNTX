# ADR-027: Permissions

Date: 2026-08-05
Status: Stub, except 27-1. The statements are made; the phases say what is decided.

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

## The credential does not carry the permission

"i want to be able to change it at will"

`Grant` fuses who the caller is with what they may do, so changing what a token
may do means minting a different one. A credential says who, a policy says what,
and they are edited apart — change the policy and every credential under it
changes at once, untouched.

The token record keeps identity and loses scope. Minting asks for a label, and
`515bedc5` removed the scope boxes because every answer was the same answer —
this is why they do not come back.

## A permission is Effect, Action, Resource, Condition

Action and Resource are separate axes. `ScopeRead`/`ScopeWrite` collapses them
into one list with two verbs baked into the field names, so what may be done and
what it may be done to cannot be said apart.

## Phases

### 27-1 — a token attests as itself

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

Open: whether the token's DID replaces the actors a request sends or is prepended
to them.

### 27-2 — next

### 27-3 — every part of QNTX behind it
