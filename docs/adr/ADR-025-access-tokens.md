

Date: 2026-10-19
Status: Debated


Access Tokens for Machine and Agent Access

"so, in a sense, the true goal is"

"eliminate access tokens before we 1.0.0"

"kill the entire concept"

## Context

Auth is passkey-only. `Handler.Middleware` in `server/auth/auth.go` gates the API on the `qntx_session` cookie, which is `HttpOnly` and lives in an in-memory `sync.Map`. Scripts, plugins, and CI cannot authenticate.

## Decision

Add a second auth path: **access tokens**, presented as `Authorization: Bearer <token>`.

- Raw token is 32 random bytes, hex-encoded, `qntx_`-prefixed. Shown once at creation.

- Persisted per backend: on SQLite via a new `access_tokens` table (`internal/db/sqlite/migrations/`); on parquet as one object per token under `<location>/system/access_tokens/`, matching the "small config" shape in ADR-024. Only the SHA-256 hash is stored.

- 「[Opus 5.5] Issued from a passkey-authenticated session via `/auth/tokens` (POST create / GET list / DELETE revoke / POST enable). A bearer token mints nothing there; the one token a bearer is handed is its agent's, below.」

- Issued to a machine with no browser 
	- Through the device flow (RFC 8628): `POST /auth/device` 
	- Person approves at `/auth/device/approve` with passkey. 
	- Machine polls the token endpoint 
		- `grant_type=urn:ietf:params:oauth:grant-type:device_code` 
	- The token's DID is its own client, 
		- Admitted as the person, for 30 days
		- Lands in the same store as one minted at `/auth/tokens` (`server/auth/device.go`).

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
say, through their WRITE and READ lines ([[ADR-034-roles-as-attestations]]).

`Lookup` returns that grant instead of a bool. A bool could carry none of it,
which is why the middleware could only ever say "someone authenticated".

## A ROOT token

"So what we have right now, is too granular for me as a person,"

"the model is more designed to be used by what would be the real users in the future, but we are actualy way ahead of it,"

"i need a ROOT token"

"and removing this will be a 1.0.0 blocker"

A ROOT session mints a ROOT token at `/auth/tokens`. It names no namespace.

## 「[Opus 5.5] An agent a token starts」

"「[Opus 5.5] 2. fine change it, fix it」"

「[Opus 5.5] An agent has its own DID and its own credential ([[ADR-048-Node-Agent]]), and」
「[Opus 5.5] an agent started under a token is handed one without anybody minting it.」

- 「[Opus 5.5] A TOKEN or ATTESTOR token asks `POST /auth/agent`, naming the agent. The node」
  「[Opus 5.5] derives the agent's token from its own key for `agent-of:<the asking token's」
  「[Opus 5.5] DID>:<agent>`, the way it derives its own agents'. Asked again for the same」
  「[Opus 5.5] agent, it is the same token and the same DID.」
- 「[Opus 5.5] The derived token is the asking token under a DID of its own. Its level, its」
  「[Opus 5.5] namespaces and the roles it holds are the asking token's, read on every」
  「[Opus 5.5] request, so it reaches no more than that token does, and it is never ROOT.」
- 「[Opus 5.5] It speaks for whoever minted the asking token. It stops when the asking token」
  「[Opus 5.5] is revoked or expires, or when that person is switched off.」
- 「[Opus 5.5] ROOT's kind, SUPER, a token issued through a client, a derived token and a」
  「[Opus 5.5] session are handed nothing here.」
- 「[Opus 5.5] `token:minted` records it, naming the asking token's DID as `derived_by`」
  「[Opus 5.5] and the agent.」

## CI by OIDC

"Another question is, how much of a place should access tokens have, if we are so OAuth native"

"to you this may seem like security or compliance,"

"but to me this is shadow IT"

"i would feel a lot better if our ci used oicd for example."

"can this be another 1.0.0 blocker please"

A CI job presents its own OIDC ID token at the token endpoint as an RFC 7523
JWT-bearer grant. QNTX checks it against the issuer's keys; a REACH line names
the repository the node admits. Nothing is minted, pasted or rotated.

## Consequences

- A leaked token reaches what the roles its DID holds allow, in its namespace,
  until it is revoked or expires.
- Tokens survive restart on both backends. Under parquet, they land at
  `<location>/system/access_tokens/` — not the SQLite scratch.
- QNTX is the authorization server for its own tokens.
- "ory/fosite is what we're going to use" — the flow in Go with fosite, its
  storage where tokens are stored now.
