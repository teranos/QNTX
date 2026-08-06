# ADR-027: Access Levels

Date: 2026-08-05
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Context

[ADR-026](ADR-026-namespaces.md) closes the namespace: nothing crosses. This ADR is about who may
cross it anyway, and what a credential can do inside one.

[ADR-025](ADR-025-access-tokens.md) gave QNTX bearer tokens with "the same trust envelope as a
passkey session — no scoping in v1". That line is what this ADR replaces. ADR-025 also already
draws one of the distinctions below without naming it: "Bearer tokens cannot mint new tokens."

## Statements

- **SUPER** crosses namespaces and stays inside QNTX. It is enough for a break-glass procedure.
- Namespace management is a SUPER capability. At SUPER a namespace is an object like any other; a
  USER never meets the concept and is in a project instead.
- SUPER creates and deletes namespaces.
- The system and default namespaces cannot be deleted.
- The system namespace is not visible at all below SUPER. The default namespace is visible, as the
  default project.
- Deleting a namespace deletes everything inside it — attestations, watchers, all of it.
- Before deletion a namespace can be drained or distilled into another namespace. Deletion drains
  into the default namespace unless told otherwise.
- **ROOT** goes beyond QNTX. It is a level of access you want on dev and not on prod.
- **TOKEN** is what a token gets. **USER** is a logged-in user.
- A token writes into the namespace the token belongs to.
- Visibility is per-namespace. Someone granted one namespace does not get the system namespace.
- QNTX should be able to receive publicly — probably a long hashed URL, or a non-SUPER access token.

## References

- [ADR-026](ADR-026-namespaces.md) — namespaces
- [ADR-025](ADR-025-access-tokens.md) — access tokens
- [security.md](../security.md) — auth paths, and that the gate is not encryption
