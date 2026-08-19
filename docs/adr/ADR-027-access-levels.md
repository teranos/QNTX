# ADR-027: Access Levels

Date: 2026-08-05
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Statements

- The ladder is scope. **ATTESTOR** acts inside a namespace, **SUPER** crosses namespaces,
  **ROOT** goes beyond QNTX.
- A new User is an ATTESTOR. Nothing has to grant it, and the level a record decodes to
  when it says nothing is the level a User starts with.
- **SUPER** crosses namespaces and stays inside QNTX. It is enough for a break-glass procedure.
- For SUPER, namespaces are at the same level of abstraction as everything else. Namespace
  management becomes possible.
- SUPER creates namespaces and disables them. Only a SUPER User owns one, and a SUPER User
  is in a namespace while owning it.
- SUPER has no business in the system namespace. That is ROOT's. The default namespace is visible,
  as the default project.
- Data never leaves. A newer record supersedes an older one, and both stay.
- A disabled namespace refuses reads. Re-enabling it opens the same bytes again.
- Disabling reaches identity. A User whose only home is that namespace cannot log in while it is
  disabled.
- **ROOT** is device or node access. It goes beyond QNTX. It is a level of access you want on dev
  and not on prod.
- There is one ROOT User. A SUPER User is created by it and by nobody else.
- A User is a human being. They hold keys and accounts — a laye key per browser, an
  authenticator key per device, an account per provider — and `auth.root_identities`
  lists ways to reach one, not the one itself (ADR-030).
- **TOKEN** is what a token gets. **ATTESTOR** is a signed-in User, acting in their namespace —
  the level is what they may do, and User is who they are.
- A token grants access to one or more namespaces. They are named when the token
  is minted and the record carries them — a bearer names none until it has been
  resolved, so resolution happens above namespaces and the token objects live in
  `system`.
- A token is scoped to predicates, read and write separately. Reading narrows the
  query rather than refusing it; writing refuses and names the predicate.
- Visibility is per-namespace.
- QNTX should be able to receive publicly — probably a long hashed URL, or a non-SUPER access token.
