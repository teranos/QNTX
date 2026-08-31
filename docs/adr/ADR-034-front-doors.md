# ADR-034: Front Doors

**Status:** Draft — in progress.
**Date:** 2026-08-30
**Related:** ADR-026 (Namespaces), ADR-027 (Permissions), ADR-030 (Identity Providers), ADR-031 (The User)

## Context

QNTX serves one relying party. `server/auth/auth.go` calls `webauthn.New` once,
with the `auth.rp_id` and `auth.rp_origins` a node is configured with, and every
ceremony runs against that one instance.

A passkey belongs to the domain it was made at. So one relying party is one
domain, and one domain is one place anybody can arrive.

The node is reached from more than one place. Why there is more than one, and
which domains they are, belongs to whoever runs the deployment and is not this
repository's to decide.

The statements that a door belongs to a namespace, that every namespace can be
given an rp id and an origin in am.toml, that a door onto no namespace is said
out loud at startup, and that PUBLIC_REGISTRATION is a rung of its own are made
in ADR-032 of the deployment repository. They are not restated here. This
document is the mechanism they ask for.

## Decision

### A door is a relying party with a namespace behind it

A door is a domain people arrive at. The node holds one relying party per door
and picks between them. That part is built.

Someone who registers at a door being *in* that door's namespace is not:
`levelOf` reads `auth.root_identities` and nothing else, so today a person who
reaches a door and is not on that list gets nowhere at all. What a door means
for who you become is the unbuilt half of this document.

### The origin picks the door

A request carries the origin it came from. That is what selects the relying
party, and there is no parameter that says otherwise.

Where a request acts is a property of the caller, never of the request
(ADR-026). A door names a namespace, so choosing a door by anything the caller
could type would be choosing a namespace by asking.

An origin no door claims reaches no door.

### `auth.rp_id` is the default namespace's door

"sure i want that as well"

The relying party a node already has is the door onto `default`. Nothing moves
and nothing is migrated: what was there is now understood as a door, and the
deployments that have one keep it.

### Other namespaces name their own

```toml
[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://portal.garden.test", "https://app.garden.test"]
```

The rp id must be a registrable domain suffix of every origin under it, which is
the browser's rule and not one this node invents. One rp id can therefore stand
behind several hostnames, and a door with several origins is one door.

A namespace absent from this table has no door. That is every namespace today,
and it stays the ordinary case: a namespace is reached through permission
(ADR-031), and a door is a second, separate way in.

ROOT creates namespaces and ROOT edits am.toml, so both ends of a door are the
same hand.

### A door onto nothing is said out loud at startup

A door naming a namespace that does not exist is a mistake in am.toml, and the
node says so when it starts rather than when somebody arrives at it.

It is a warning and not a refusal. A node that will not start because one door
of several is misnamed takes down the doors that are correct.

### A credential records the door it was made at

`webauthn_credentials` holds what a credential is and who it admits. It does not
hold where it was made, and with one relying party there was nowhere else it
could have been.

With several, a login at one door must be offered only the credentials made
there. Offering the rest would hand a browser keys it will refuse, and would say
out loud that an account exists somewhere else — which is the one thing a door
must not disclose.

### A door names its namespace's SUPER

Only a SUPER User owns a namespace (ADR-031), and a namespace with a door has
one. ROOT creates SUPER Users and ROOT edits am.toml, so the door's own row is
where they are named — the same hand at both ends, and the shape
`auth.root_identities` already has one level down.

```toml
[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://portal.garden.test", "https://app.garden.test"]
super   = ["google:someone@example.com"]
```

"for super email is fine"

An address is what a person recognises, and this is a short list written by
hand and read by the person who wrote it. It carries its provider for the same
reason a Google sub does (ADR-030): unqualified, an address is a claim any
provider on the door could make, and the door takes more than one.

`server/auth/google.go` says a sub is the only thing Google promises never
changes and an address is reassignable. That is true of this list too. It is a
list ROOT maintains, so an address that moves is an edit rather than a breach.

Someone listed here arrives as SUPER rather than arriving lower and being
raised:

"first"

### The level decides what proof is asked for

"the passkey would not be required for this particular path, but for SUPER it is"

A passkey is the second half of an admission today, and it is asked for
unconditionally: a provider ceremony leaves a half-admission and nothing turns
that into a session without one.

A person arriving at a door is not asked for one. The provider is the gate and
it is the only one — a household hiring a tradesman did not come for a second
ceremony. SUPER is asked for both halves, because SUPER runs the namespace.

So the rung says what proof it costs to reach it, and a provider ceremony has to
be able to make a session on its own. Nothing does that today; every path into
`sessions.create` runs after a passkey.

### PUBLIC_REGISTRATION is a level

**Proposed. Nothing below is built, and no quoted line settles it.**

`server/auth/admission.go` declares ROOT, SUPER, ATTESTOR and TOKEN. TOKEN is
declared and never assigned — `mintable` issues SUPER or ATTESTOR and nothing
else sets it — so what the ladder is in practice is a question this touches and
does not answer.

PUBLIC_REGISTRATION would sit under all of them. Every other User the node holds
was put there by somebody; someone who walks up to a door made themselves, and
the level would say so.

What that level may do is log in and be attested. Registering would be written
down as an attestation, and an email address may be part of what it says — the
provider decides what it hands over.

### A door names its own OAuth client

"you would think a separate door could be given its own OAuth client"

`[auth.provider.google]` is one client for the whole node. Someone arriving at a
door sees the consent screen of whatever that one client is called, which is the
node's name and not the name of the thing they came to.

A client belongs to a door for the same reason an rp id does. The door's row
carries it, and `handleBindingStart` picks it — that handler already knows which
door the request reached.

```toml
[auth.door.<namespace>.provider.google]
client_id     = "..."
client_secret = "ssm:///..."
```

A door with no client of its own uses the node's.

This is in scope for the branch that adds doors.

### Registering at one door is not registering at another

The same provider account at two doors is two registrations.

`auth.root_identities` lists the ways one User is reached because there is one
ROOT (ADR-030). A public registration belongs to the namespace it arrived in,
and that is where it ends. Nothing crosses (ADR-026).

## Consequences

`webauthn.New` is called once per door rather than once per node, and the
handler that runs a ceremony takes the relying party as an argument instead of
reading a field.

`credentialStore.getAll` returns every credential the node holds. With doors it
has to answer for one, which makes the door a column and a migration.

`credentialStore.owner` says "Registration admits one owner today, so the first
non-empty answer is the answer." That stops being true the moment a second door
takes a registration.

A provider is what a door admits people through. `server/auth/providers.go`
offers Mastodon and atproto always — they ask the operator for nothing — and
appends Google once its client is configured, so a button that could only fail
is never drawn. Which of those a door should offer is not decided here.
