# Security

## Bind Address

Server binds to `127.0.0.1` by default (loopback only). Configurable via `[server] bind_address` or `QNTX_BIND_ADDRESS` env var. Non-loopback addresses (e.g., `0.0.0.0`) require `auth.enabled = true` — the server refuses to start otherwise.

## Authentication

Opt-in via `[auth] enabled = true`. Access gate — proves identity before the server responds. Not encryption.

Two auth paths: WebAuthn biometric sessions (in-memory, `HttpOnly` cookie, expire after 24h configurable) for browser access, and persistent revocable bearer tokens (`Authorization: Bearer …`) for machine access — see [ADR-025](adr/ADR-025-access-tokens.md). Bearer tokens cannot mint or revoke other tokens; token management is gated on a passkey session.

To manage tokens in the UI, open the **⍟ Self** glyph and click **⚿ Access Tokens** — there is no URL route.

## Shortcomings

No node-to-node authentication. QNTX nodes cannot verify each other's identity — there is no mutual trust establishment, no signed identity exchange, no way for one node to prove it is who it claims to be to another. This blocks any meaningful peer-to-peer connectivity.

Authorization is asked per handler, and three ask. `Middleware` answers who a caller is; whether that caller may reach a route is checked only where a handler consults the admission — attestations, namespaces, statusline. Everywhere else an admitted bearer has ROOT's reach: a token scoped to one predicate still reads `/api/config`, every plugin's config, and `/logs/download`, and creates pulse schedules. ADR-027 names the phase that closes this ("27-3 — every part of QNTX behind it"); the phase is unwritten.

What a given deployment exposes is that deployment's own question, and is
audited where the deployment is configured.

## Transport

Local-first. No built-in TLS — use a reverse proxy for remote access. WebSocket origin validation and CORS on all endpoints.
