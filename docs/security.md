# Security

## Bind Address

Server binds to `127.0.0.1` by default (loopback only). Configurable via `[server] bind_address` or `QNTX_BIND_ADDRESS` env var. Non-loopback addresses (e.g., `0.0.0.0`) require `auth.enabled = true` — the server refuses to start otherwise.

## Authentication

Opt-in via `[auth] enabled = true`. Access gate — proves identity before the server responds. Not encryption.

Currently supports WebAuthn biometric authentication. Sessions are in-memory, `HttpOnly`, expire after 24h (configurable).

## Shortcomings

No node-to-node authentication. QNTX nodes cannot verify each other's identity — there is no mutual trust establishment, no signed identity exchange, no way for one node to prove it is who it claims to be to another. This blocks any meaningful peer-to-peer connectivity.

See [security/www-readiness.md](security/www-readiness.md) for full audit.

## Transport

Local-first. No built-in TLS — use a reverse proxy for remote access. WebSocket origin validation and CORS on all endpoints.
