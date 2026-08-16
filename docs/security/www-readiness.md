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

Code: `server/init.go` (safety check), `internal/config/defaults.go` (default + env binding `QNTX_BIND_ADDRESS`).

## Open Issues

### P0 — Must fix before any internet exposure

**~~No TLS.~~** ~~All traffic is cleartext.~~ Done — deployment concern, not application concern. Use a reverse proxy (Caddy, nginx) for TLS termination.

**~~Peer sync has zero authentication.~~** Mitigated — sync is disabled when `bind_address` is non-loopback. The endpoint is not registered and the sync tree is not initialized. Full fix requires a QR-based pairing flow for DID exchange. See [#643](https://github.com/teranos/QNTX/issues/643).

**~~No rate limiting.~~** ~~Zero rate limiting on any endpoint.~~ Done — per-IP token bucket rate limiting across 5 route groups (auth, ws, write, read, public). Configurable via `[server.rate_limit]`. See `server/ratelimit.go`.

### P1 — Significant risk on the open internet

**Origin header now required on WebSocket.** `checkOrigin` in `server/util.go` — empty Origin rejected. Test at `server/util_test.go` exercises the exact bypass condition. Machine access belongs on the bearer token path (ADR-025) served over HTTP, not raw WS.

**`/health` reduced to liveness only.** `HandleHealth` in `server/handlers.go` — returns `{"status":"ok"}` and nothing else. Version, commit, build time, client count, and owner moved to authenticated endpoints. Test `TestHandleHealthStripped` in `server/server_test.go` guards against regressions.

**In-memory passkey sessions.** `server/auth/sessions.go:18` — `sync.Map`. Server restart logs out browser users. Under DoS this amplifies impact. Machine access is unaffected: bearer tokens (ADR-025) persist to SQLite and survive restart. Browser sessions still need SQLite persistence.

**10MB WebSocket messages x 256 buffer depth.** `server/client.go:40,25` — Each client can buffer ~2.5GB. A few malicious clients = OOM.

**Session cookie `Secure` flag now set on deployment.** `server/auth/handlers.go:setSessionCookie/clearSessionCookie` — Cookie carries `Secure` when `server.bind_address` is non-loopback (the same signal that already forces `auth.enabled`). Loopback dev keeps it off so browsers accept the cookie over plain `http://localhost`. Tests in `server/auth/auth_test.go` cover both branches.

**WebAuthn RPID.** `[auth] rp_id` in `internal/config/am.go:54` drives `server/auth/auth.go:59`; `server/init.go` refuses to start when `bind_address` is non-loopback and `rp_id` is unset. Localhost is still the fallback for empty `rp_id`. Not yet confirmed end-to-end against a real domain.

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
- **Plugin gRPC auth** — Constant-time token comparison, ephemeral per-session tokens (`plugin/grpc/services/auth.go`)
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

---

# Second Audit — Identity Providers

Audit date: 2026-08-16. Scope: the `identity-providers` branch — ADR-026 namespaces,
ADR-027 access levels, ADR-030 identity providers, ADR-025 scoped tokens, and laye
shipping in the browser. The first audit predates all of it.

Perimeter at time of writing: a security group restricted to one operator IP, and it
is not being lifted on merge. Nothing below is a merge blocker. The ranking is the
order to work them in.

## P0 — Must fix before the perimeter is ever widened

### A token's namespace is recorded and never enforced

`Middleware` (`server/auth/auth.go:126`) reads `grant.Namespace` off the token and puts
it on the `Caller`. Nothing downstream reads it. `CallerFrom` has five call sites
(`server/attestation_handlers.go:71,170`, `server/embeddings/handlers.go:141`) and each
consults `Grant` scope — predicates — never `Caller.Namespace`.

The process opens exactly one attestation store, and it is hardcoded:

    cmd/qntx/commands/database_parquet.go:66
    duckStore, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceDefault)
    cmd/qntx/commands/database_parquet.go:76
    watcherStore, err := duckdbcgo.NewWatcherStore(location, duckdbcgo.NamespaceDefault)

So a token minted for namespace `X` reads and writes `default`. ADR-024's isolation is
real in the storage layout and absent at the API — `<location>/<namespace>/` separates
bytes that no request can be routed to. The danger is not the missing feature, it is
that minting appears to succeed: `handlers_tokens.go:52-70` validates the namespace,
refuses `system`, gates non-`default` on `root_identities`, and stores it. An operator
who scopes a token to a namespace has been told it worked.

Fix: either route the store by `Caller.Namespace`, or refuse any namespace but
`default` at mint time until routing exists.

### A namespace-scoped write cannot be attributed to its namespace

`server/auth/attest.go:41` mints admission attestations into `NamespaceSystem` by
passing it as the ASUID context, and writes them through the one `default` store. The
context field says `system`; the bytes land in `default`. Admission records — the audit
trail ADR-030 introduces so that "admission is a fact about the deployment, not a line
in a log that rotates away" — are in the wrong universe, and the only thing saying
otherwise is a string inside the record.

## P1 — Significant risk on the open internet

### `unsafe-inline` now guards a private key

`server/handlers.go:336` serves
`script-src 'self' 'unsafe-inline' https://d3js.org; object-src 'none';`.

Before this branch, script injection stole a session cookie that a logout invalidates.
Now `laye-p2p` mints an ed25519 keypair into IndexedDB and that key **is** the identity
admitted by `auth.root_identities`. Injection under `unsafe-inline` exfiltrates a
credential no logout revokes and no expiry ends — only striking the account out of
`am.toml` does. The exposure class changed; the header did not.

Note `plugin/grpc/websocket_security.go:154` already serves `default-src 'self'` with no
`unsafe-inline`, so the strict policy exists in the codebase and is not applied here.

### Every ceremony and challenge map is in-process memory

`sessions.go` (`sync.Map`), `pendingLogins` (`pending.go:23`), `layeChallenges`
(`laye.go:21`), `bindingFlows`, `signedBindings` (`auth.go:38`), `ceremonies`
(`auth.go:40`). All swept on a 5-minute ticker by `StartSessionSweep`.

Three consequences. A restart mid-ceremony strands every half-admission. There is no
second instance — none of this is shared, so horizontal scaling silently breaks login.
And `/auth/laye/challenge` is unauthenticated and `GET`: each call stores 32 bytes plus
a timestamp that lives 2 minutes, bounded only by the auth rate limiter. The code says
so itself: *"the only thing bounding this map is time."*

### Enrolment is open on a fresh deployment

`may_register.go:12` — *"A deployment with no credentials is open, because first
enrolment has nobody to ask."* `mayRegister` checks only `creds.exists()`, not
`root_identities`.

`handlers.go:169` does refuse the ownerless credential afterwards, so the window is
narrower than it looks: a stranger reaching a fresh node can run the WebAuthn ceremony
to completion and be refused at save. But the refusal depends on `enrollingIdentity`
returning empty, which depends on there being no pending cookie — and
`/auth/laye/verify` hands out a pending cookie to anyone whose DID is listed. The two
gates are correct together and neither is sufficient alone. Worth an explicit
`identitiesGovern()` check in `mayRegister` so the invariant is stated once.

### Admission attestations are best-effort and silent

`attest.go:33` — *"It never fails the request it describes."* Correct for availability,
but it means the audit trail has no delivery guarantee and a failure appears only in a
`Warnw`. A store that is down during an incident is exactly when the record matters.

## P2 — Should fix for a hardened deployment

### `/auth/status` is unauthenticated and answers before login

`handlers.go:36-80` returns `registered`, `owner_did`, and `binding_signers` to any
caller. The signer list is public keys and laye genuinely needs it — the comment is
right that a browser skipping the check believes any peer. But `owner_did` names the
deployment's owner to an unauthenticated stranger, and `registered` reports whether a
node is fresh, which is the one state where enrolment is open.

### `laye-relaye` is in the workspace and ADR-030 says it does not exist

`Cargo.toml` lists `crates/laye-relaye`. Its own `docs/gateway.md` states *"No auth. Any
client can connect… No rate limiting until we see abuse."* ADR-030 states *"relaye
ceases to exist."* If the crate ships, an unauthenticated WebSocket gateway ships. If it
does not, the doc should stop describing a live service. The doc also cites its own
former path, `crates/relaye/docs/gateway.md`.

### Two identity mechanisms in one tree

ADR-030 anchors on the node DID with `auth.root_identities` as the allowlist.
`crates/laye-me/docs/signed-chat.md` describes peer-authored Ed25519 with attribution
via a `BindingTable` and no allowlist. Both are shipped shapes and neither references
the other.

### Carried forward, still open

The first audit's P1 and P2 items are unchanged by this branch: WebSocket per-client
memory cap (`server/client.go:40,25` — 10MB × 256), request body limits on remaining
POST endpoints, plugin binary signature verification, storage unencrypted at rest, and
DNS rebinding on sync connections.

## Verified sound

Checked and found correct, recorded so a later audit does not re-litigate them:

- **Provider SSRF is properly closed.** `normalizeHost` (`providers.go:120`) rejects
  single-label hosts and non-public literals, and `guardDial` (`providers.go:170`) runs
  as the dialer's `Control` — a post-DNS check, so a public hostname resolving to a
  private address is refused at connect. Body reads are capped by `maxProviderBodyBytes`
  and every call is bounded by a 10s timeout.
- **Ownerless credentials are refused.** `handlers.go:169` returns 403 when the
  enrolling session names no identity, enforcing ADR-030 rather than only stating it.
- **Replay is closed on both challenge paths.** `layeChallenges.redeem` and
  `pendingLogins.close` both use `LoadAndDelete`, so a captured signature is spent once.
- **Every `/auth/*` route is rate-limited**, including the provider callback —
  `RegisterRoutes` wraps all of them in `corsWrap`, which `sub_auth.go:28` composes as
  `rateLimitAuthMiddleware(corsMiddleware(...))`.
- **Cookie flags are consistent.** Session and pending cookies both carry `HttpOnly`,
  `SameSite=Lax`, and `Secure` when bound non-loopback.
- **Binding trust is anchored.** `verifyBinding` (`binding.go:41`) checks the signer
  against `auth.binding_signers` before checking the signature, so a self-consistent
  binding from an untrusted signer is refused.
- **Revocation lands without a restart.** `SetIdentities` is called by the config
  watcher, and `stillAdmitted` re-reads on use, so striking an account takes its
  enrolled devices with it.
- **Tokens cannot mint tokens.** `/auth/tokens*` is wrapped in `sessionOnly`.
- **A token with no scope grants nothing.** Mint refuses an empty scope; pre-scoping
  tokens resolve to a grant that permits nothing.

## Priority Table — Second Audit

| Pri | Item | Effort | Status |
|-----|------|--------|--------|
| P0 | Route the store by `Caller.Namespace`, or refuse non-default at mint | High | Open |
| P0 | Admission attestations land in the namespace they name | Medium | Open |
| P1 | Drop `unsafe-inline` from the web CSP | Medium | Open |
| P1 | Persist sessions and ceremony state | High | Open |
| P1 | `mayRegister` consults `root_identities` | Low | Open |
| P1 | Surface failed admission attestations | Low | Open |
| P2 | Gate `owner_did` on `/auth/status` | Low | Open |
| P2 | Resolve `laye-relaye`: ship it with auth, or remove it | Low | Open |
| P2 | Reconcile signed-chat trust with ADR-030 | Low | Open |
