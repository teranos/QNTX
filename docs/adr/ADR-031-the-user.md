# ADR-031: The User

Date: 2026-08-18
Status: Stub — the statements below are made. Nothing beyond them is decided.

## Statements

- A User is a human being.
- A User's id is an ASUID under the `US` prefix (ADR-010). The name segment is a snapshot
  taken at minting; the random suffix is what makes it unique, so renaming does not
  re-identify anyone.
- A User holds keys and accounts. laye mints a key per browser, an authenticator derives
  one per device, and a provider names an account its own way. A User holds several of
  each, so none of them is the User.
- A User has a first name, a last name, any number of email addresses, and any number of
  phone numbers.
- `auth.root_identities` lists ways to reach a User, not Users (ADR-030).
- There is one ROOT User. A SUPER User is created by it and by nobody else (ADR-027).
- A new User is an ATTESTOR, which acts inside a namespace. The level says what a User
  may do; User says who they are, and the two stopped sharing a word (ADR-027).
- Every User has provenance: the record names the User that created it, whatever the
  level. The ROOT User names nobody, because there was nobody before it, and that is
  how it is told apart.
- A User lives in namespaces, plural (ADR-026). Disabling one reaches a login only for a
  User whose only home it was.
- Only a SUPER User owns namespaces, and a SUPER User is in a namespace while owning it —
  so a SUPER User is at home in every namespace it owns.
- Ownership is recorded on the namespace, not on the User. The namespace's own record is
  what refuses a second create; a list on the User would be a second answer.

## Collision

ADR-026 says "Namespace is identity. There is no separate concept of a user." Naming the
User retires that, and resolves ADR-026 against itself: it also says an identity lives in
a namespace, which is the half that survives.

## Not done

Nothing records a User. Every place the system means one it stores a way in — a
credential's `admitted_as`, a token's `minted_by`, a namespace's owner. See ADR-030's
"Not done".
