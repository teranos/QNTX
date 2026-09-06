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

Per route, role and namespace a ROOT line outranks every other actor, then the latest line
in time is the whole truth. There is no deny, no negation and no `is not`: a newer line
supersedes, nothing is taken back by a word.

"ground writes into system"

Only ROOT writes these, or a token ROOT minted. There is no new endpoint and no promotion
handler: `POST /api/attestations` with the system namespace is the whole interface.

"DEFAULT DENY"

"root should be allowed to do anything, the rest not"

The absence of a line is a refusal. Below ROOT nothing reaches a path, a namespace, a
predicate, or another actor's rows unless a line says so. A READ line reads the holder's own
rows; `all` on it is the word that reads everyone's.

"the part where you specify predicates in the ui should not exist, because we are replacing
with with our system fully"

A token carries no predicate scope. What it may read and write is what the roles its DID
holds say. A SUPER token is ROOT handing its own reach to a token it made, and is narrowed
by nothing.

## Example: a worker in `garden`

The names are the ones #899 and the tests use. `garden` is a namespace, WORKER and
COORDINATOR are roles, and neither word exists anywhere before ROOT writes it.

ROOT says what a WORKER reaches and what a WORKER may say:

```
REACH is '/api/attestations'              of WORKER COORDINATOR   by ROOT COORDINATOR
WRITE is 'visit:started' 'visit:done'     of WORKER
READ  is 'visit:assigned' 'visit:done'    of WORKER
READ  is 'visit:assigned' 'visit:done'    of COORDINATOR   all
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
- a COORDINATOR reads both of everyone's, because their READ line says `all`;
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

Words, with `all` as an attribute:

```json
{"subjects":["WRITE"],"predicates":["visit:started","visit:done"],"contexts":["WORKER"]}
{"subjects":["READ"],"predicates":["visit:assigned","visit:done"],"contexts":["WORKER"]}
{"subjects":["READ"],"predicates":["visit:assigned","visit:done"],"contexts":["COORDINATOR"],"attributes":{"all":true}}
```

A revoke is the other predicate: `role:revoked` where `role:granted` was.

## A token holds a role the same way

The subject of a grant is any route that reaches a User, or a token's DID. The day
dispatching is a program, it is one grant line and the same lines: not a human version and
a machine version.

## Not here

Which namespace a plugin acts in (#900), bytes beside an attestation (#901), a fetch naming
its door (#902), the namespace on every log line (#903), `/auth/user` (#904).
