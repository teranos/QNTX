# ADR-025: Access Tokens for Machine Access

Date: 2026-07-20
Status: Accepted
Target: v0.30.0

## Context

Auth is passkey-only. `Handler.Middleware` in `server/auth/auth.go` gates the API on the `qntx_session` cookie, which is `HttpOnly` and lives in an in-memory `sync.Map`. Scripts, plugins, and CI cannot authenticate.

## Decision

Add a second auth path: **access tokens**, presented as `Authorization: Bearer <token>`.

- Persisted per backend: on SQLite via a new `access_tokens` table (`db/sqlite/migrations/`); on parquet as one object per token under `<location>/system/access_tokens/`, matching the "small config" shape in ADR-024. Only the SHA-256 hash is stored.
- Raw token is 32 random bytes, hex-encoded, `qntx_`-prefixed. Shown once at creation.
- Issued from a passkey-authenticated session via `/auth/tokens` (POST create / GET list / DELETE revoke / POST enable). Bearer tokens cannot mint new tokens.
- `Handler.Middleware` gains a bearer-header path before the cookie check.
- UI surfaces create / list / revoke / enable.
- Rejected bearer attempts are recorded per token, so a revoked token shows whether it is still being presented.
- Revocation is a switch, not a one-way door: kill the token, watch whether anything is still presenting it, turn it back on if that was you. While revoked it is dead for everyone; enabling is a deliberate act by the owner, not a way back in for whoever held it.

## What a token is

"the token route will be the way things get to access qntx publicly"

A token carries four things beyond its hash, and each answers a question a bare
secret could not.

Its **own `did:key`**. The 32 bytes are an ed25519 seed rather than only a
secret, so the token has a public half and its holder can sign as it.

**`minted_by`** — the `auth.root_identities` entry whose session issued it.
"speaking on behalf of a user who minted them". Revoking the account revokes
what it minted.

**`namespace`** — where it may act, chosen at mint time.

**`scope`** — predicates, read and write listed separately, so a token that may
report a result cannot manufacture one.

`Lookup` returns that grant instead of a bool. A bool could carry none of it,
which is why the middleware could only ever say "someone authenticated".

## A client is a token

"I'm not really seeing the issue in elevating QNTX to be a real authorization
server"

An app that wants in the way claude.ai's connector does asks for a client id
and a client secret, and gets them the way QNTX got its own:

"Like how i had to create one at Apple and Google for QNTX"

The roles flip. QNTX is the console.

"Isnt it a type of credential token"

A client is one more kind a token is minted as, beside SUPER and ATTESTOR. Its
`did:key` is the client id. Its raw value is the client secret, shown once and
hashed. Minted from a session at a door, and bound to that door. Listed,
revoked and enabled like every other token.

A client authenticates at the token endpoint and nowhere else. The middleware
refuses it as a bearer: the secret pasted into a phone is not a credential
that reaches `/api/attestations`.

## A consent mints

The node is the authorization server for its own tokens. Two metadata
documents say so; the authorize endpoint sends the person through the passkey
door on a ticket (ADR-030) and asks yes or no on the node's own domain; the
token endpoint spends the code and mints.

What it mints is the token above: ATTESTOR, minted by the person who said yes,
in the namespace of the door the app walked up to (ADR-032). The consent asks
nothing else. Namespaces are their own universes (ADR-026) and the credential
does not carry the permission (ADR-027), so there is no scope to ask for and
no namespace to choose.

The token expires and is refreshed by rotation: the new pair invalidates the
old one in the same answer. Revoking the token ends the connection. Revoking
the client ends every connection made through it.

## Minting names a kind

"Making a selection between two mutually exclusive options and it being
instantiated having neither selected"

The mint glyph opened with SUPER already chosen. A kind is pressed, one row
per kind, and none is pressed until somebody presses it. `mintable` refusing
an empty kind was already the node's answer; the glyph stops sending one it
was never asked for.

## Consequences

- A leaked token reaches its scope in its namespace until revoked. Revocation is
  the only defense.
- Tokens survive restart on both backends. Under parquet, they land at
  `<location>/system/access_tokens/` — not the SQLite scratch.
- No forced rotation on hand-minted tokens. A consent-minted token rotates.
