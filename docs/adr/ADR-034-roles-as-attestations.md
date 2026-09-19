# ADR-034: Roles as Attestations

**Status:** Accepted
**Date:** 2026-09-06
**Related:** ADR-025 (Access Tokens), ADR-027 (Permissions), ADR-031 (The User), #899, #909, #914

## Decision

"a way yet to granularize based on roles we can define through attestations, like reach has
been accomplished"

"we also granularise to what predicate WORKER would be allowed to write"

A role is not in the binary. It is what lines in the system namespace say about an
upper-case word, and the node reads those lines the way it reads a plugin's routes from the
store. Four kinds of line, one grammar:

| Line | Says |
|---|---|
| grant | who holds a role, in which namespace, by whom |
| REACH | which doors a role reaches, and who besides ROOT may grant it |
| WRITE | which predicates a role may write |
| READ | which predicates a role may read, and whether beyond its own rows |

"root outranks everything else yes"

A line settles per pair: a path with a role, a word with a role, a role with a name in a
namespace. Per pair a ROOT line outranks every other actor, then the latest line in time is
the whole truth. A line about a different pair is untouched:

"REACH is /api/namespaces of WORKER and REACH is /api/namespaces of NOBODY and REACH is
/api/namespaces of ANOTHERWORKER should all work simultaneously and not 'overwrite' each
other"

There is no deny, no negation and no `is not`. "but also, i need to be able to attest the
inverse somehow": the inverse is a line, with a marker beside the thing it takes away, the
way `role:revoked` sits beside a role. `reach:revoked` beside the paths, `words:revoked`
beside the words. A pair whose latest line is a revoke is gone; the rest stand.

```
REACH is reach:revoked /api/namespaces of NOBODY
WRITE is words:revoked visit:done of WORKER
```

A name holds something when any pair of its still stands, and nothing takes a name away:
the lines that mention it are the record.

"ground writes into system"

Only ROOT writes these, or a SUPER token ROOT minted: a SUPER token is ROOT's own reach handed
to a token. An ATTESTOR token is the narrow one and writes no policy however it was minted;
every token on a node is ROOT's, and one granting itself a role would be the narrowing undone.
There is no new endpoint and no promotion handler: `POST /api/attestations` with the system
namespace is the whole interface.

"DEFAULT DENY"

"root should be allowed to do anything, the rest not"

The absence of a line is a refusal. Below ROOT nothing reaches a path, a namespace, a
predicate, or another actor's rows unless a line says so. A READ line reads the holder's own
rows; `by all` on it reads everyone's.

"an attestation is meant to be read out loud"

`all` is said in the actor slot, after the writer's own, because whose rows may be read is a
question about actors. A flag in the attributes is not read out loud, and widens nothing.

"the part where you specify predicates in the ui should not exist, because we are replacing
with with our system fully"

A token carries no predicate scope. What it may read and write is what the roles its name
holds say. A SUPER token is ROOT handing its own reach to a token it made, and is narrowed
by nothing.

## Levels at runtime

"this is for plugin routes, the runtime configurable part, the reach table remains static"

"and runtime can never supersede the coompiled in reach table"

"book/new is actually more open than PUBLIC_REGISTRATION, its pretty much PUBLIC"

```
REACH is '/api/{plugin}/book/new' of ANYONE
```

## Example: a worker in `garden`

The names are the ones #899 and the tests use. `garden` is a namespace, WORKER and
COORDINATOR are roles, and neither word exists anywhere before ROOT writes it.

ROOT says what a WORKER reaches and what a WORKER may say:

```
REACH is '/api/attestations'              of WORKER COORDINATOR   by ROOT COORDINATOR
WRITE is 'visit:started' 'visit:done'     of WORKER
READ  is 'visit:assigned' 'visit:done'    of WORKER
READ  is 'visit:assigned' 'visit:done'    of COORDINATOR   by all
```

Then ROOT hands a person the role, by any route that reaches their User (ADR-031):

```
google:110169484474386276334 is WORKER of garden by ROOT
```

Those five lines are the onboarding. On the next request the person's admission holds
WORKER in `garden`, so:

- they reach `/api/attestations` in `garden` and are refused in `orchard`;
- they write `visit:done` and are refused `visit:assigned`, because WRITE named one and not
  the other;
- they read `visit:assigned` and `visit:done` narrowed to lines they wrote, because no
  word on their READ line says otherwise;
- a COORDINATOR reads both of everyone's, because their READ line says `by all`;
- `/api/config` stays ROOT's, because no runtime line names it and the const table never
  shrinks.

Offboarding is one more line, the same grant with `role:revoked` as the predicate beside
the role. Both lines stay, and the history of who could do what, when, and who said so is
the store.

## Example: a coordinator grants

`by ROOT COORDINATOR` on the REACH line above is what lets a COORDINATOR write the grant. A
COORDINATOR in `garden` posts:

```
google:110169484474386276334 is WORKER of garden by COORDINATOR
```

and it holds. The same line from a WORKER is refused, because `by` did not name WORKER. The
same line from a COORDINATOR of `orchard` is refused in `garden`, because a role holds in
exactly one namespace. A role no REACH line names after `by` is ROOT's alone to grant.

## The same lines on the wire

Each line is one attestation. A grant:

```json
{"subjects":["google:110169484474386276334"],"predicates":["role:granted","WORKER"],"contexts":["garden"]}
```

Reach, with the granters as actors after the writer's own:

```json
{"subjects":["REACH"],"predicates":["/api/attestations"],"contexts":["WORKER","COORDINATOR"],"actors":["ROOT","COORDINATOR"]}
```

Words, with `all` as an actor after the writer's own:

```json
{"subjects":["WRITE"],"predicates":["visit:started","visit:done"],"contexts":["WORKER"]}
{"subjects":["READ"],"predicates":["visit:assigned","visit:done"],"contexts":["WORKER"]}
{"subjects":["READ"],"predicates":["visit:assigned","visit:done"],"contexts":["COORDINATOR"],"actors":["all"]}
```

A revoke is the other predicate: `role:revoked` where `role:granted` was. The inverse of a
reach line or a word line is the marker beside what it takes away:

```json
{"subjects":["REACH"],"predicates":["reach:revoked","/api/namespaces"],"contexts":["NOBODY"]}
```

## A token holds a role the same way

The subject of a grant is any route that reaches a User, or a token's label. "yes the label is
the token's name", so one live token holds a name, and minting refuses a name already held.
The DID is the token's signature and rides as the actor on what it writes; no grant names it.
The day
dispatching is a program, it is one grant line and the same lines: not a human version and
a machine version.

## Limitations

- The lines are read with the store's ceiling, `storage.MaxAttestationLimit`, newest first. A
  node holding more lines than that loses its oldest from the read, and an old grant stops
  holding without a line saying so. A correctness edge, not a performance one.
- A leaked SUPER token is ROOT's reach until it is revoked. Every line it writes is on the
  record, which is how the leak is seen, and after the fact.
- A namespace on a grant is met at its slug, the way a door and a step meet one. The role on a
  line is met uppercased. Nothing else about a line is case-insensitive.

Post-1.0.0:

- The lines are cached per node and dropped on a write through that node. A second node
  writing the same store serves what it read until its own next write; ADR-024 names the same
  hazard for tokens.
- Who may write policy is ROOT and SUPER, and `by` on a REACH line is the whole of delegation.
  One operator's shape.

## Not here

Which namespace a plugin acts in (#900), bytes beside an attestation (#901), a fetch naming
its door (#902), the namespace on every log line (#903), `/auth/user` (#904).
