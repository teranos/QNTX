# ADR-027: Permissions

Date: 2026-08-05
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Statements

- SUPER creates namespaces and disables them. Only a SUPER User owns one, and ownership is
  recorded on the namespace (ADR-031).
- The system namespace is not visible at all below SUPER. The default namespace is visible, as the
  default project.
- Data never leaves. A newer record supersedes an older one, and both stay.
- A disabled namespace refuses reads. Re-enabling it opens the same bytes again.
- A disabled namespace refuses reads. A login is a session with the node and stands
  (ADR-031); reach into namespaces is a granted relation.
- **ROOT** goes beyond QNTX. It is a level of access you want on dev and not on prod.
- A User is a human being. They hold keys and accounts — a laye key per browser, an
  authenticator key per device, an account per provider — and `auth.root_identities`
  lists ways to reach one, not the one itself (ADR-030).
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
