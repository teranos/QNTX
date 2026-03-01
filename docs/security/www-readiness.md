# WWW Readiness — Security Audit

Audit date: 2026-02-25. Scope: what breaks when QNTX moves from `127.0.0.1` to the public internet.

## Threat Model Shift

QNTX assumes the network boundary IS the security boundary. Every endpoint is reachable by the machine owner alone. On the www, every endpoint is reachable by everyone.

## Enforced: Bind Address + Auth Gate

`server.bind_address` defaults to `127.0.0.1`. The server refuses to start if bind address is non-loopback and `auth.enabled` is false.

```toml
# am.toml — required for non-localhost deployment
[server]
bind_address = "0.0.0.0"

[auth]
enabled = true
```

Code: `server/init.go` (safety check), `am/defaults.go` (default + env binding `QNTX_BIND_ADDRESS`).

## Open Issues

### P0 — Must fix before any internet exposure

**~~No TLS.~~** ~~All traffic is cleartext.~~ Done — deployment concern, not application concern. Use a reverse proxy (Caddy, nginx) for TLS termination.

**~~Peer sync has zero authentication.~~** Mitigated — sync is disabled when `bind_address` is non-loopback. The endpoint is not registered and the sync tree is not initialized. Full fix requires a QR-based pairing flow for DID exchange. See [#643](https://github.com/teranos/QNTX/issues/643).

**~~No rate limiting.~~** ~~Zero rate limiting on any endpoint.~~ Done — per-IP token bucket rate limiting across 5 route groups (auth, ws, write, read, public). Configurable via `[server.rate_limit]`. See `server/ratelimit.go`.

### P1 — Significant risk on the open internet

**Missing Origin header accepted on WebSocket.** `server/util.go:32` — `if origin == "" { return true }`. Raw WebSocket clients bypass origin checking entirely.

**`/health` leaks reconnaissance data.** `server/handlers.go:411-428` — Public endpoint returns version, git commit, build time, client count, owner name.

**In-memory sessions.** `server/auth/sessions.go:18` — `sync.Map`. Server restart logs out all users. Under DoS this amplifies impact. Sessions need SQLite persistence.

**10MB WebSocket messages x 256 buffer depth.** `server/client.go:40,25` — Each client can buffer ~2.5GB. A few malicious clients = OOM.

**Session cookie missing `Secure` flag.** `server/auth/handlers.go:221-229` — Cookie is `HttpOnly` + `SameSite=Lax` but not `Secure`. Over HTTPS the cookie can still leak via HTTP downgrade.

**WebAuthn RPID hardcoded to "localhost".** `server/auth/auth.go:46` — WebAuthn won't work on a real domain. RPID must come from config.

### P2 — Should fix for hardened deployment

**DNS rebinding on sync connections.** `server/sync_handler.go:122` — Standard `websocket.Dialer` resolves DNS at connect time.

**SQLite database unencrypted at rest.** Anyone with filesystem access reads all attestations, credentials, embeddings.

**Watcher engine doesn't use SaferClient.** `ats/watcher/engine.go:110` — Standard `http.Client` on user-configured URLs. See `docs/security/ssrf-protection.md`.

**Plugin binaries have no integrity verification.** `plugin/grpc/discovery.go:288-294` — Binary found by name in search paths, executed without checksum or signature.

**No request body size limit on most POST endpoints.** File uploads (50MB) and prose (10MB) have limits. Config updates, attestation creation, type creation do not.

## Already Solid

- **File upload/download** — Extension whitelist, MIME detection, UUID naming, path traversal protection with character whitelist (`server/files.go`)
- **Static files** — Embedded via `//go:embed`, no filesystem traversal possible
- **SQL queries** — Parameterized throughout
- **Plugin gRPC auth** — Constant-time token comparison, ephemeral per-session tokens (`plugin/grpc/auth.go`)
- **Outbound SSRF protection** — `SaferClient` blocks private IPs on AI provider requests (`internal/httpclient/safer_client.go`)
- **No sensitive data in logs** — Verified: no tokens, passwords, or keys logged
- **Config file gitignored** — `am.toml` excluded from git, env var overrides available

## Priority Table

| Pri | Item | Effort | Status |
|-----|------|--------|--------|
| P0 | Auth required for non-loopback bind | Low | Done |
| P0 | TLS termination | Low | Done (deployment) |
| P0 | Peer sync authentication | High | Mitigated ([#643](https://github.com/teranos/QNTX/issues/643)) |
| P0 | CORS exact matching | Low | Done |
| P0 | Rate limiting middleware | Medium | Done |
| P1 | WebAuthn RPID from config | Low | Open |
| P1 | Require Origin header on WS | Low | Open |
| P1 | Strip `/health` or auth-gate it | Low | Open |
| P1 | `Secure` flag on session cookie | Low | Open |
| P1 | Persist sessions to SQLite | Medium | Open |
| P1 | WebSocket per-client memory cap | Medium | Open |
| P2 | Request body limits on remaining endpoints | Low | Open |
| P2 | Plugin binary signature verification | Medium | Open |
| P2 | SQLite encryption at rest | Medium | Open |
