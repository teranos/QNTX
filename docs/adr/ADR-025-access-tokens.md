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
report a result cannot manufacture one. An empty scope grants nothing, and a
token minted with neither is refused rather than issued useless.

`Lookup` returns that grant instead of a bool. A bool could carry none of it,
which is why the middleware could only ever say "someone authenticated".

## Consequences

- A leaked token reaches its scope in its namespace until revoked. Revocation is
  the only defense.
- Tokens survive restart on both backends. Under parquet, they land at
  `<location>/system/access_tokens/` — not the SQLite scratch.
- Tokens minted before scoping have no namespace and no scope, so they resolve
  to a grant that permits nothing. They are re-minted, not migrated.
- No forced rotation and no OAuth. Future ADRs.
