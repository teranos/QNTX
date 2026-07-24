# ADR-025: Access Tokens for Machine Access

Date: 2026-07-20
Status: Accepted
Target: v0.30.0

## Context

Auth is passkey-only. `server/auth/auth.go:77-92` gates the API on the `qntx_session` cookie, which is `HttpOnly` and lives in an in-memory `sync.Map`. Scripts, plugins, and CI cannot authenticate.

## Decision

Add a second auth path: **access tokens**, presented as `Authorization: Bearer <token>`.

- Persisted per backend: on SQLite via a new `access_tokens` table (`db/sqlite/migrations/`); on parquet as one object per token under `<location>/access_tokens/`, matching the "small config" shape in ADR-024. Only the SHA-256 hash is stored.
- Raw token is 32 random bytes, hex-encoded, `qntx_`-prefixed. Shown once at creation.
- Issued from a passkey-authenticated session via `/auth/tokens` (POST create / GET list / DELETE revoke). Bearer tokens cannot mint new tokens.
- Middleware at `server/auth/auth.go:77-92` gains a bearer-header path before the cookie check. Same trust envelope as a passkey session — no scoping in v1.
- UI surfaces create / list / revoke.

## Consequences

- A leaked token is user-equivalent until revoked. Revocation is the only defense.
- Tokens survive restart on both backends. Under parquet, they land at `<location>/access_tokens/` — not the SQLite scratch.
- No forced rotation, no per-token scopes, no OAuth. Future ADRs.
