# Security

## Bind Address

Server binds to `127.0.0.1` by default (loopback only). Configurable via `[server] bind_address` or `QNTX_BIND_ADDRESS` env var. Non-loopback addresses (e.g., `0.0.0.0`) require `auth.enabled = true` — the server refuses to start otherwise.

## Authentication

Opt-in via `[auth] enabled = true`. Access gate — proves identity before the server responds. Not encryption.

「[Opus 5.5] Two auth paths: WebAuthn biometric sessions (in-memory, `HttpOnly` cookie, expire after 24h configurable) for browser access, and, for machine access, a request signed by a key the node holds a grant for (`Authorization: Signed …`): "A signature proves the key, and nothing reusable leaves the node." A third-party OAuth client carries what the OAuth flow issued it.」

## Shortcomings

No node-to-node authentication. QNTX nodes cannot verify each other's identity — there is no mutual trust establishment, no signed identity exchange, no way for one node to prove it is who it claims to be to another. This blocks any meaningful peer-to-peer connectivity.

What a given deployment exposes is that deployment's own question, and is
audited where the deployment is configured.

## Transport

Local-first. No built-in TLS — use a reverse proxy for remote access. WebSocket origin validation and CORS on all endpoints.
