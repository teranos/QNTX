# Security

## Authentication

Opt-in via `[auth] enabled = true`. Access gate — proves identity before the server responds. Not encryption.

Currently supports WebAuthn biometric authentication. Sessions are in-memory, `HttpOnly`, expire after 24h (configurable).

## Shortcomings

No node-to-node authentication. QNTX nodes cannot verify each other's identity — there is no mutual trust establishment, no signed identity exchange, no way for one node to prove it is who it claims to be to another. This blocks any meaningful peer-to-peer connectivity.

## Transport

Local-first. No built-in TLS — use a reverse proxy for remote access. WebSocket origin validation and CORS on all endpoints.
