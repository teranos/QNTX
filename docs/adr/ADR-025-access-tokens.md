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

Its **own `did:key`**. The 32 bytes are an ed25519 seed rather than only a
secret, so the token has a public half and its holder can sign as it.

**`minted_by`** — the `auth.root_identities` entry whose session issued it.
"speaking on behalf of a user who minted them". Revoking the account revokes
what it minted.

**`namespace`** — where it may act, chosen at mint time.

What a token may read and write is not on the token: the roles its DID holds
say, through their WRITE and READ lines (ADR-034).

`Lookup` returns that grant instead of a bool. A bool could carry none of it,
which is why the middleware could only ever say "someone authenticated".

## Consequences

- A leaked token reaches what the roles its DID holds allow, in its namespace,
  until it is revoked or expires.
- Tokens survive restart on both backends. Under parquet, they land at
  `<location>/system/access_tokens/` — not the SQLite scratch.
- QNTX is the authorization server for its own tokens.
- "ory/fosite is what we're going to use" — the flow in Go with fosite, its
  storage where tokens are stored now.
