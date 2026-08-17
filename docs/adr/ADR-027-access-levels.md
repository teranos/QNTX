# ADR-027: Access Levels

Date: 2026-08-05
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Statements

- **SUPER** crosses namespaces and stays inside QNTX. It is enough for a break-glass procedure.
- For SUPER, namespaces are at the same level of abstraction as everything else. Namespace
  management becomes possible.
- SUPER creates namespaces and disables them.
- The system namespace is not visible at all below SUPER. The default namespace is visible, as the
  default project.
- Data never leaves. A newer record supersedes an older one, and both stay.
- A disabled namespace refuses reads. Re-enabling it opens the same bytes again.
- Disabling reaches identity. A user whose only home is that namespace cannot log in while it is
  disabled.
- **ROOT** goes beyond QNTX. It is a level of access you want on dev and not on prod.
- A SUPER user is created by the root identity and by nobody else. An identity is a
  `did:key` (ADR-030), so the root identity is one key.
- **TOKEN** is what a token gets. **USER** is a logged-in user.
- A token grants access to one or more namespaces. They are named when the token
  is minted and the record carries them — a bearer names none until it has been
  resolved, so resolution happens above namespaces and the token objects live in
  `system`.
- A token is scoped to predicates, read and write separately. Reading narrows the
  query rather than refusing it; writing refuses and names the predicate.
- Visibility is per-namespace.
- QNTX should be able to receive publicly — probably a long hashed URL, or a non-SUPER access token.
