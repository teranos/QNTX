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
- The first User to prove they hold a root identity is the first User, and the first User
  is always the ROOT User. Proving a listed route is what creates them — there is no
  separate act, and nothing has to be seeded ahead of it.
- A User is never an ATTESTOR. ATTESTOR is a token that can attest, minted by the User
  that owns it.
- Every User has provenance: the record names the User that created it, whatever the
  level. The ROOT User is to name the node that signed its first admission — a node is
  the one thing that exists before any User does.
- A User carries the signed binding for each account it holds. Storing a binding is not
  storing a verdict — the binding names its own signer, and `auth.binding_signers` is
  asked about that signer every time it is used, so striking a signer out still reaches
  bindings already written down.
- A User is enabled or disabled, and a disabled User still exists. ROOT or SUPER disables
  one and enables it again. Data never leaves, and a person is not an exception.
- A disabled User cannot log in, and what they minted stops with them — a token speaks for
  whoever minted it (ADR-025), so it is disabled by the same switch.
- That switch is why revoking a person is one act. Striking a route out of
  `auth.root_identities` closes one way in, and a User holds several; disabling the User
  is the person, not the door.
- A User does not live in a namespace. They have permission to reach namespaces, and
  permission is a relation rather than a home.
- Only a SUPER User owns namespaces.
- Ownership is recorded on the namespace, not on the User. The namespace's own record is
  what refuses a second create; a list on the User would be a second answer.
- A User record lives in `system`, where the token objects already are (ADR-027).
- No User is visible below SUPER, because `system` is not (ADR-026). An ATTESTOR sees no
  User record: not another's, and not their own. There is no directory of people at that
  level. What an ATTESTOR sees inside a namespace is what signed something, and `by` is
  the signer (ADR-026) — never the person behind it.

## The name

A User has a display_name, and it is a name rather than half a login. Not unique, never
asked for at a door, no password beside it. What identifies a User is a key they hold or
an account they proved.

"display_name of root cannot be changed anymore when set"

"if display_name of root is unset, it becomes root"

"regardless of root_identity setting it or not, root is never an available display name except for the one root identity user, they dont need to set their display name as root"

So the ROOT User is `root` from the moment they exist, without choosing it, and `root` is
the one name no other User may take.

## Collision

ADR-026 says "Namespace is identity. There is no separate concept of a user." Naming the
User retires that, and resolves ADR-026 against itself: it also says an identity lives in
a namespace, which is the half that survives.

## What is recorded

A User is one object per person under `<location>/system/users/` on the parquet
backend, holding an id, a display_name, any number of emails, the level, the
keys it holds and the accounts that reach it. A sqlite deployment keeps none,
the way it keeps no tokens.

`POST /auth/user/arrive` is where a person says a name and an email, and
requires neither. The name is settled once; an email that arrives later is added
rather than refused, because a User has any number of them.

## Not done

A User holds no last name and no phone number.

A User does not live in a namespace. They have permission to reach namespaces,
and nothing records which yet.

`created_by` is empty on ROOT, because the node that signed its first admission
is not written down.

A namespace's owner is a string, and so is a credential's `admitted_as`.

The ROOT User's provenance is the node that signed the first admission, and the first
admission is what creates them — so the two land together, or neither does. ADR-030
records that the path has never been run. Until it is, `created_by` is empty on the ROOT
User, and that emptiness is a placeholder for the node rather than the answer.

`system` holds the User records, and only the parquet backend has namespaces at all
(ADR-026, "Not done") — so on SQLite there is nowhere for them to go yet.
