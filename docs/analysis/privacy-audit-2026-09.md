# Privacy Audit — September 2026

Point-in-time audit of the QNTX codebase around the theme of privacy: what
personal data the system collects, stores, logs, and sends off the machine, and
where practice diverges from the stated posture. File references are to the
tree at the audit commit.

## Verdict

The local-first posture is real, and the design intent is unusually explicit —
`SendDefaultPII: false` written out as a decision, "identity is not the third
party's to hold" in the Sentry design, "no directory of people" in ADR-031.
The gaps are between that intent and three forces working against it:

1. **The logging convention defeats the redaction design.** Redaction is
   field-key-based; the codebase convention (CLAUDE.md) mandates interpolating
   in-scope variables into messages and wrapped errors, which redaction never
   touches — and at these call sites the in-scope variable is often the person.
2. **There is no erasure story.** No user deletion exists anywhere, attestations
   are append-only by design, retention code is written but never called, and
   backups outlive deletions.
3. **Secrets and keys rest world-readable.** 0644/0755 are project-wide
   defaults, applied to private keys, configs holding tokens, and logs holding
   identity.

## High severity

### H1. No deletion path for personal data

- `crates/ats-duckdb/src/users.rs` exposes `all`, `by_route`, `put` — no
  delete. No `/auth/user` DELETE route exists (`server/auth/auth.go:212-244`).
  A User record (display name, email addresses, accounts, keys), once written,
  has no removal path in any language in the tree.
- Attestations are append-only and immutable by design; ADR-024 states "No
  distillation, no bounded-storage enforcement, no compaction. Parquet storage
  is unbounded" (`docs/adr/ADR-024-parquet-storage-backend.md:65`). Identity
  events (`identity:admitted`/`identity:refused`, whose subjects are
  root-identity strings) are attested, so an identity audit trail accumulates
  permanently with no erasure mechanism. `DeleteAttestation` exists in the
  storage layer (`ats/storage/duckdbcgo/storage_cgo.go:202`) but
  `/api/attestations` is GET/POST only (`server/routing.go:122`).
- Passkey deletion is real (`server/auth/credentials.go:92`,
  `server/auth/forget.go:131-188` — hard delete, User rewritten without the
  key), but the account's email addresses survive, and:
- **Backups defeat deletion.** The SQLite hot backup
  (`pulse/schedule/ticker.go:200,371,400`,
  `crates/ats-sqlite/src/store.rs:379-400`) is a full byte copy — node private
  key, passkeys, attestations — never aged out, never permission-restricted.
  A "forget" does not reach backups taken before it.

This is architectural, not a missing endpoint: it needs an ADR, not a patch.

### H2. Private keys and secrets rest world-readable

- The node's ed25519 signing key — what signs bindings and admissions — is
  stored unencrypted: `private_key BLOB` in SQLite
  (`db/sqlite/migrations/039_create_node_identity.sql`), plain-JSON
  `private_key_hex` on parquet (`crates/ats-duckdb/src/nodeidentity.rs:133-142`,
  landing in S3 when `location` is a bucket). The DB directory is created 0755
  (`db/connection.go:51`) and nothing chmods the file, so on a multi-user host
  any local user reads the signing key, every token hash, and every User record.
- `DefaultDirPermissions = 0755`, `DefaultFilePermissions = 0644`
  (`internal/config/am.go:261-262`) apply to config writes — and config
  legitimately holds literals: `[code.github] token` and `[meili] key` accept
  raw secrets (`am.example.toml:316`), unlike the Google secret which is
  ref-only. `~/.qntx/am_from_ui.toml` is written 0644 with three 0644 backup
  copies (`internal/config/persist.go:51,116`), preserving rotated-out secrets.
  The one 0750 (`persist.go:76`) is undermined by `load.go:242` creating the
  same directory 0755.
- Log files: dir 0755, file 0644 (`internal/logger/logger.go:170,174`) — see H3
  for what they hold. Watchdog goroutine stack dumps (which carry in-flight
  argument data) at 0644, unbounded, unrotated
  (`cmd/qntx/commands/database.go:123-128`).
- Browser side: laye's ed25519 keypair is saved raw in IndexedDB
  (`crates/laye-p2p/src/wasm.rs:27-30,266`) — a bearer credential with no
  WebCrypto non-extractable protection — alongside bindings carrying provider
  handles including the Google email. Nothing wipes either on logout;
  `/auth/forget` deletes the server-side credential but the browser keeps its
  key and its local attestation mirror.

No `os.Chmod` to 0600 exists anywhere for a secret-bearing file.

### H3. Identity, IP, and user-agent ship to Sentry under the default config

The Sentry sink (`internal/logger/sentry.go`) is off without a DSN, sets
`SendDefaultPII: false`, and unconditionally redacts credential-shaped keys.
But once a DSN is set, everything at `min_level` (default `info`) ships, and
the identity redaction — whole-key match on `["email", "did"]`
(`internal/config/defaults.go:99`) — misses every identity key actually in use:

- **The access log, on every request, at info:** `server/accesslog.go:115-125`
  logs `"ip"` (leftmost `X-Forwarded-For` — `server/ratelimit.go:80-94`),
  `"identity"` (the `auth.root_identities` entry: an email, a DID, a Mastodon
  URL, `google:<sub>`), `"user_agent"`, and `"path"`. A request-by-request
  who/from-where/with-what-browser trail lands in the console, the 0644 log
  file, and Sentry.
- **The Google email under the key `handle`:** `server/auth/google.go:108` sets
  `Handle: who.Email`, logged at `server/auth/sign_binding.go:312,327`
  (`"account bound", "canonical_id", …, "handle", …`) — the default `email`
  rule never sees it.
- Further unredacted identity keys: `identity` and `minted_by`
  (`server/auth/auth.go:157-159,187-189`), `admitted_as`
  (`server/auth/handlers.go:195,233,347,378`), `route`/`owner_did`
  (`server/auth/forget.go:148`), `user`/`display_name`
  (`server/auth/users.go:223,239`, `server/auth/arrive.go:122-123`), `actor`
  (`qntx-plugins/qntx-atproto/handlers.go:118,187,506,515`), `identifier` —
  the Bluesky login handle or email — on auth failure
  (`qntx-plugins/qntx-atproto/plugin.go:81-85`), `remote_addr`/`client`/`peer`
  (`server/handlers.go:35,392-617`, `server/nodedid/handlers.go:12`).
- **Messages and error chains are never scrubbed.** Redaction operates on field
  keys only; `captureIssue` ships the full wrapped error via
  `CaptureException` (`sentry.go:342`), and the house convention mandates
  interpolating URLs, paths, and IDs into exactly those strings.
  `docs/sentry.md` recommends the pattern rather than warning about it.
- Nested field objects escape redaction entirely: `hide()` checks only
  top-level flattened keys (`sentry.go:283-289`), and unmatched values fall
  through `attach`'s `fmt.Sprint` (`sentry.go:432`).
- `QNTX_SENTRY_DSN` in the environment turns shipping on
  (`internal/config/load.go:145-147` — `AutomaticEnv`), outside `am.toml`.

Note the same unredacted stream also exists **on box**: the file core applies
no redaction at all (`internal/logger/logger.go:184-191`), there is no log
rotation anywhere, and the deprecated-but-live `/logs/download` route
(`server/routing.go:94`, `server/handlers.go:409-412`) hands any admitted
caller the complete file.

### H4. Secrets served in plaintext over the API

- `GET /api/config?introspection=true` flattens every viper setting verbatim —
  no redaction list (`internal/config/introspection.go:114-119`): the
  OpenRouter API key (the UI reads it back full and truncates client-side —
  `web/ts/llm-provider-glyph.ts:136-146`), the atproto `app_password`,
  `auth.root_identities`, the Sentry DSN.
- `GET /api/plugins/{id}/config` returns every non-underscore-prefixed key
  verbatim (`server/plugin_config_handlers.go:59-72`); `plugin.ConfigField`
  has no sensitivity flag (`plugin/interface.go:132-141`), so the Bluesky app
  password comes back in plaintext.
- Both routes sit behind `wrap` only (`server/routing.go:96,109`): **any**
  admitted bearer, including a minimally-scoped ATTESTOR token, reads them —
  scope narrowing is applied only in the attestation handlers.
- `qntx am show` prints the whole config unredacted
  (`cmd/qntx/commands/am.go:101-116`) — the output people paste into bug
  reports.

The contrast is sharp with the parts that got this right: secretref
literal-rejection for forge tokens and the Google secret
(`internal/config/validate.go:77-97`), and `logger.Redacts` on the Sentry
path. The redaction predicate exists; these endpoints don't ask it.

### H5. Every browser session dials a hardcoded third-party relay

`web/ts/laye.ts:46-48` pins
`/dns4/relaye.sbvh.nl/tcp/443/wss/p2p/12D3KooW…`; `web/ts/main.ts:236` calls
`initLaye()` on startup with no config gate; the swarm dials unconditionally
with `identify` enabled (`crates/laye-net/src/swarm.rs:49-50,155-166`). On
every page load the relay operator learns the visitor's IP, stable libp2p peer
ID / `did:key` — the same key that is the subject of their account bindings —
and the `/qntx/1.0.0` identify string. No content is gossiped (topics are
empty, nothing publishes), but the connection and the stable identifier are
disclosed with no consent step. ADR-030 states "relaye ceases to exist"
(`docs/adr/ADR-030-identity-providers.md:15`); the hardcoded constant did not
follow. First-party to this project's origin deployment; third-party to
everyone else who runs the software.

## Medium severity

### M1. Attestation-controlled URL auto-fetched by the browser

`web/ts/components/glyph/attestation-attrs.ts:312` feeds an attestation
attribute into `loadStructureFromUrl` on render, no click. The gate
(`attestation-attrs.ts:127-128`) is a substring test —
`url.includes('alphafold.ebi.ac.uk/files/AF-') && url.endsWith('.cif')` — so
`https://attacker.example/t?q=alphafold.ebi.ac.uk/files/AF-x-F1.cif` passes.
Anyone who can write an attestation (a synced Bluesky timeline post, an
ix-json poll) gets a render-triggered beacon of the viewer's IP and
user-agent. Fix is parsing the URL and comparing
`hostname === 'alphafold.ebi.ac.uk'`.

### M2. Content in logs

- `plugin/grpc/client.go:796` dumps the **entire raw WebSocket frame** on
  parse failure at error level (`"raw", string(data)`) — the `/ws/llm` proxy
  path, so a malformed frame's full body also becomes a captured Sentry issue.
  The adjacent line at `:791` logs `len(data)`; the raw dump should match it.
- Search queries are logged verbatim at info, tied to identity:
  `server/embeddings/handlers.go:168-170` (`"query", query` next to
  `"identity", admitted.Identity`; also `:105,114,123`), `server/client.go:557-561,592-597`.
- Browser console forwarding (dev mode): `web/ts/dev-debug-interceptor.ts:113-136`
  POSTs all console output to `/api/debug`, re-emitted through zap with the
  full `window.location.href` including query string (`server/init.go:87-102`).
  `web/ts/laye.ts:158,281` put the browser DID and `admittedAs` into that
  stream.
- LLM prompts/responses, attestation bodies, and credential values are **not**
  logged — checked and clean (`ats/so/actions/prompt/handler.go` logs lengths;
  `qntx-plugins/qntx-github/plugin.go:62` logs `token != ""`).

### M3. Retention exists on paper only

- `CleanupOldExecutions` (`pulse/schedule/execution_store.go:475`) and
  `CleanupOldJobs` (`pulse/async/store.go:298`, wrapper
  `pulse/async/queue.go:395`) have zero non-test callers.
- `task_logs` is explicitly untruncated — "full logs stored", `metadata`
  holding API responses — with a promised 3-month TTL "implemented separately"
  that never was (`db/sqlite/migrations/008_create_task_logs_table.sql:20-24`).
- Job `payload` rows (`migrations/002`) and per-call `ai_model_usage` rows
  (`migrations/011` — `entity_id`, `error_message`, metadata) accumulate
  without bound. Actual GC covers only watcher queue entries, a 15,000-row cap
  on `storage_events`, SQLite-only attestation eviction, and the in-memory
  session sweep.

### M4. Unauthenticated and spoofable edges

- `GET /auth/status` is CORS-wrapped but unauthenticated (`server/auth/auth.go:213`)
  and returns `owner_did` (`server/auth/handlers.go:51-84`) — a stable
  per-person device identifier, to strangers. `/setup` goes out of its way not
  to leak exactly this ("What is public is how, never who" —
  `server/auth/setup.go:16-20`); the two postures should match.
- `X-Forwarded-For` is trusted unconditionally (`server/ratelimit.go:80-87`):
  spoofable per-request rate-limit buckets on the auth/ceremony endpoints, and
  a poisoned `ip` field in the access log.
- `/auth/binding/start` is unauthenticated by design
  (`server/auth/sign_binding.go:157`): an anonymous caller can drive the node
  into contacting an arbitrary public host with attacker-supplied credentials.
  Host validation bounds it (`server/auth/providers.go:146-186`), but it is an
  unauthenticated outbound-request primitive.
- Cookie `Secure` is derived from whether any `auth.rp_origins` entry is
  `https://` (`server/sub_auth.go:94`, `server/exposure.go:15-28`). Behind a
  TLS-terminating proxy with only `http://` origins listed, session cookies
  silently ship without `Secure`.

### M5. Content flows that are opt-in once, then automatic

Not defects — the design is honest about them — but each is a standing consent
decision the operator makes once and per-event data flows follow:

- A `glyph_execute`→`prompt` watcher forwards **the full triggering
  attestation JSON** to the registered LLM provider on every match
  (`ats/watcher/engine_execute.go:250-256`); the `prompt.execute` job does the
  same for every attestation an `ax` query matches
  (`ats/so/actions/prompt/handler.go:221-233`).
- Cluster labeling samples attestation text to the LLM on an interval
  (`server/embeddings_labeling.go`, default sample 5, interval nil = off).
- Watcher webhooks POST the **complete attestation object** to an arbitrary
  operator URL — no allowlist, no signature, no scheme restriction
  (`ats/watcher/engine_execute.go:322-345`).
- ix-net captures Anthropic API prompts and images into local attestations
  (`qntx-plugins/ix-net/source/ixnet/extract.d:130-179`), which then become
  eligible for auto-embedding (`server/embeddings/observer.go:34-38`) and the
  labeling pipeline above. Its own docs note the API key is visible in
  captured headers (`proxy.d:26`).
- The hardcoded provider fallback name is `"openrouter"` — a cloud gateway —
  when neither request nor config names one (`server/prompt_handlers.go:94-102`,
  `server/routing.go:327`). Inert while unregistered, but the honest default
  for a local-first system is the local provider.
- On a deployment without a system store, identity admission attestations fall
  back to the default store (`server/sub_auth.go:49-54`), where a token with
  `scope_read: ["*"]` can read the admission history.
- Plugin update polling contacts `api.github.com` unconditionally at runtime
  for every plugin declared by repo URL (`plugin/grpc/update_poll.go:37-53`) —
  coordinates and a PAT, no local data.

### M6. Frontend third-party and storage residue

- Mol* is injected from `https://cdn.jsdelivr.net/npm/molstar@4/…`
  (`web/ts/components/glyph/bioviz/molstar-loader.ts:15,27`) — IP/UA/referrer
  to jsDelivr on structure render. This contradicts the CSP, which allows
  `https://d3js.org` (which nothing uses) and not jsDelivr
  (`server/handlers.go:341`). One of the two is stale. Everything else is
  self-hosted (vendored font, no external fetches).
- `qntx-canvas-sync-queue-corrupt` (`web/ts/api/canvas-sync.ts:54-55`): on a
  parse failure, pending canvas mutations (user content) are copied to a
  localStorage key that nothing ever deletes — surviving logout and
  `clearStorage()` (`web/ts/state/ui-impl.ts:791-792` removes only
  `qntx-ui-state`).
- Glyph content (source code, prompts, results) persists in browser storage
  via UI state (`web/ts/state/ui-impl.ts:741-753`); embeddings — invertible
  enough to leak source text — in a second IndexedDB
  (`web/ts/embedding-store.ts:4`).

## Low severity / doc drift

- ADR-025 says tokens persist on SQLite via an `access_tokens` migration; no
  such migration exists and non-parquet backends get no bearer auth at all
  (`server/token_store_duckdb.go:19-22` → 503). Fail-closed, but the ADR is
  stale.
- `docs/sentry.md` is linked from nowhere in the repo, and understates what
  leaves (it does not mention the access-log identity/IP stream or
  interpolated error text). There is no PRIVACY.md and no privacy section in
  README.
- `server/auth/sign_binding.go:198-202` logs the provider `confirm` error;
  whether `req.Secret` leaks depends on providers never embedding it in error
  text — holds today, unenforced.
- `web/types/config.ts:116,187,350` declare `enableAnalytics`/`shareAnalytics`
  with no reader anywhere — dead config that suggests behavior which doesn't
  exist.
- Plugin gRPC uses `insecure.NewCredentials()` throughout
  (`plugin/grpc/client.go:87-88`) — fine on loopback; an off-box plugin
  address would carry prompts and tokens in cleartext.

## What is already right

Worth recording, because these are deliberate decisions holding up:

- **No analytics, no phone-home, no update-check beacon, no hardcoded DSN.**
  Verified across Go, Rust, and TS; the frontend loads no third-party scripts
  except Mol* (M6). Embedded MeiliSearch runs with `--no-analytics` on
  loopback; embeddings are local ONNX; no cloud LLM client is in-tree.
- **Sentry is genuinely opt-in and announced** — empty DSN is off, the node
  says on its first line that logs are leaving, credential-word redaction has
  no off switch, and metrics dimensions are deliberately bounded — no actor,
  DID, path, or ID may become a series (`internal/measure/measure.go:52-58`).
- **Tokens are hashed (SHA-256), shown once, never in URLs**; the summary type
  strips the hash at the FFI boundary with a test asserting it; minting is
  passkey-session-only and bearers cannot mint.
- **Passkeys store no PII** — fixed WebAuthn user constants, no name or email
  reaches the authenticator or the table; migration 054 dropped ownerless
  credentials with a CHECK constraint.
- **No directory of people** — ADR-031's rule holds: no route lists Users,
  emails return only to their owner, and login refusals are
  enumeration-resistant ("nothing presented is listed").
- **Sessions are in-memory, HttpOnly, SameSite=Lax**, never persisted to disk,
  and every request re-checks `root_identities`, so striking an identity kills
  live sessions.
- **The access log logs `URL.Path`, not `RawQuery`** — a query-string
  credential would not land in it.

## Recommended order of work

1. Extend the default `redact_keys` to the identity keys actually in use
   (`identity`, `admitted_as`, `handle`, `minted_by`, `route`, `owner_did`,
   `canonical_id`, `actor`, `identifier`, `ip`, `user_agent`) and apply the
   redactor to the file core, not only Sentry — the file is what
   `/logs/download` serves. Rename the Google `handle` field to `email` so the
   existing rule catches it. (H3)
2. Route `/api/config` introspection and plugin config reads through
   `logger.Redacts`; add a sensitivity flag to `plugin.ConfigField`; redact
   `qntx am show`. (H4)
3. Chmod 0600/0700 for the config, DB, log, and parquet system paths; flip
   the two defaults in `internal/config/am.go:261-262` for secret-bearing
   writes. (H2)
4. Gate the relay dial behind config, defaulting off — or at minimum surface
   it the way Sentry is surfaced, on the node's first line. (H5)
5. Wire `CleanupOldExecutions`/`CleanupOldJobs` into the ticker; deliver the
   promised task-log TTL; age out DB backups. (M3, H1)
6. Fix the AlphaFold hostname check; drop `"raw"` from
   `plugin/grpc/client.go:796`; reconcile the CSP with Mol*. (M1, M2, M6)
7. Write the erasure ADR: what deletion means for Users, attestations, and
   backups on an append-only store. Everything above is a patch; this one is a
   decision. (H1)
