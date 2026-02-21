# Security

## Authentication

Opt-in via `[auth] enabled = true`. Access gate — proves identity before the server responds. Not encryption.

Currently supports WebAuthn biometric authentication. Sessions are in-memory, `HttpOnly`, expire after 24h (configurable).

## Transport

Local-first. No built-in TLS — use a reverse proxy for remote access. WebSocket origin validation and CORS on all endpoints.
