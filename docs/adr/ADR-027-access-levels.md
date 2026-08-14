# ADR-027: Access Levels

Date: 2026-08-05
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Statements

- **SUPER** crosses namespaces and stays inside QNTX. It is enough for a break-glass procedure.
- For SUPER, namespaces are at the same level of abstraction as everything else. Namespace
  management becomes possible.
- SUPER creates and deletes namespaces.
- The system and default namespaces cannot be deleted.
- The system namespace is not visible at all below SUPER. The default namespace is visible, as the
  default project.
- Deleting a namespace deletes everything inside it — attestations, watchers, all of it.
- Before deletion a namespace can be drained or distilled into another namespace. Deletion drains
  into the default namespace unless told otherwise.
- **ROOT** goes beyond QNTX. It is a level of access you want on dev and not on prod.
- **TOKEN** is what a token gets. **USER** is a logged-in user.
- A token writes into the namespace the token belongs to. The namespace is named
  when the token is minted, and the record carries it — a bearer names none until
  it has been resolved, so resolution happens above namespaces and the token
  objects live in `system`.
- A token is scoped to predicates, read and write separately. Reading narrows the
  query rather than refusing it; writing refuses and names the predicate.
- Visibility is per-namespace.
- QNTX should be able to receive publicly — probably a long hashed URL, or a non-SUPER access token.
