# ADR-031: The User

Date: 2026-08-18

## Statements

- A User in QNTX is a human being with an ASUID under the `US` prefix (ADR-010). The name segment is a snapshot
  taken at registration; the random suffix is what makes it unique,
- A User holds keys and accounts. laye mints a key per browser, an authenticator derives
  one per device, and a provider names an account its own way. A User holds several of
  each, so none of them is the User.
  phone numbers.
- `auth.root_identities` lists ways to reach a User, not Users (ADR-030).
- There is one ROOT User. A SUPER User is created by it and by nobody else (ADR-027).
- The first User to prove they hold a root identity is the first User, and the first User
  is always the ROOT User. Proving a listed route is what creates them — there is no
  separate act, and nothing has to be seeded ahead of it.
- Every User has provenance: the record names the User that created it, whatever the
  level. The ROOT User is to name the node that signed its first admission — a node is
  the one thing that exists before any User does.
- A User carries the signed binding for each account it holds. Storing a binding is not
  storing a verdict — the binding names its own signer, and `auth.binding_signers` is
  asked about that signer every time it is used, so striking a signer out still reaches
  bindings already written down.
- A User is enabled or disabled, and a disabled User still exists. ROOT or SUPER disables
  one and enables it again. Data never leaves, and a person is not an exception.
- "user should be able to disable their acc, but reawaken (enable) it later as well"
- The record says who switched a User off, and only they switch it on. What a person did
  to themselves is theirs to undo; what ROOT did is not.
- A disabled User is admitted at no gate. They still prove a route and still get a
  session, because the switch is the one thing a disabled User has to reach. What they
  minted stops with them — a token speaks for whoever minted it (ADR-025), so it is
  disabled by the same switch, read on every request rather than at login.
- That switch is why revoking a person is one act. Striking a route out of
  `auth.root_identities` closes one way in, and a User holds several; disabling the User
  is the person, not the door.
- Ownership is recorded on the namespace, not on the User. The namespace's own record is
  what refuses a second create; a list on the User would be a second answer.
- A User record lives in `system`, where the token objects already are (ADR-027).
- No User is visible below SUPER, because `system` is not (ADR-026). An ATTESTOR sees no
  User record: not another's, and not their own. There is no directory of people at that
  level. What an ATTESTOR sees inside a namespace is what signed something, and `by` is
  the signer (ADR-026) — never the person behind it.

## name

A User has a display_name, and it is a name rather than half a login. Not unique,

What identifies a User is a key they hold or an identity they control.

## root

"display_name of root cannot be changed anymore when set"

"if display_name of root is unset, it becomes root"

"regardless of root_identity setting it or not, root is never an available display name except for the one root identity user, they dont need to set their display name as root"

So the ROOT User is `root` from the moment they exist, without choosing it, and `root` is
the one name no other User may take.

You claim the ROOT User by proving you own a root identity. It happens once and
cannot happen again, so it is attested as `node:claimed`. Recording never fails
the thing it records (ADR-030), which makes this the one attestation whose loss
cannot be made up later.

## What is recorded

A User is one object per person under `<location>/system/users/` on the parquet
backend, holding an id, a display_name, any number of emails and phone numbers,
the level, who switched it off, the keys it holds and the accounts that reach it.
The record is [`auth.User`](https://github.com/teranos/QNTX/blob/main/server/auth/users.go),
mirrored by [`UserRecord`](https://github.com/teranos/QNTX/blob/main/crates/ats-duckdb/src/users.rs)
on the object. A sqlite
deployment keeps none, the way it keeps no tokens.

`POST /auth/user/arrive` is where a person says a name, an email and a phone
number, and requires none of them. The name is settled once; an email or a phone
number that arrives later is added rather than refused, because a User has any
number of each. A phone number is kept as its digits and the leading `+` the
person typed, so one number is one string however it was spaced.

`POST /auth/user/disable` and `POST /auth/user/enable` are the switch, reached
by the person's own session. Off, `disabled_by` names them, every gate refuses
them as switched off, and the Self glyph offers to reawaken. Set by anyone else,
enable is refused naming who, and the person stays off. The act is attested as
`identity:disabled` and `identity:enabled`.

## Not done

A User holds no first name and no last name. A Dutch name has three parts, and
the middle one is the person's to write: `van Doorn` files under D, `Van Doorn`
under V, and the string alone does not say which.

A User reaches namespaces through permission, and nothing records which yet.

A namespace's owner is a string, and so is a credential's `admitted_as`.

`created_by` is empty on ROOT. The node that signed the first admission is
written down — `node:claimed` names it — but on the attestation rather than on
the User, so the provenance is a record to go and find instead of a field to
read.

root Glyph for User management and overview

GDPR delete, PR 911

Creating Users manually as root

ROOT switching a person off. The record and the gate are ready for it, and no
route lets ROOT flip anyone but themselves yet.

reassignment of e-mail address
