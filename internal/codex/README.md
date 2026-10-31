# Codex runtime and namespace agent

> “making an early start on the Codex Agent runtime”

This package runs beside `internal/claudecode` and `internal/pi`, using the
Parity pin introduced in #1364 and the executable runtime in #1365. Codex is
selectable as a namespace harness. The Node Agent harness landed in #1400. See [ADR-048](../../docs/adr/ADR-048-Node-Agent.md)
for the agent's identity and session requirements.

`Pinned().Ensure(ctx, dir)` downloads the platform archive named by Parity,
checks its SHA-256 before extraction, and atomically installs `codex`. On reuse,
it verifies the retained archive and extracts again: the archive digest is not
a digest of the executable. Only the expected regular executable member is
accepted. Linux and macOS, amd64 and arm64, follow the four existing pins.

`Said.Run` executes one `codex exec --json` turn. It requires an explicit model
and sandbox (`read-only` or `workspace-write`), sends the prompt over stdin,
and resumes only the supplied thread ID. Codex chooses a new thread ID; callers
must persist `Answer.Session`, including when a turn returns a partial answer
and an error. The namespace harness serializes calls for one agent.

The agent's HOME is its own directory, CODEX_HOME is its `codex` subdirectory,
and its working directory is `work`. Node credentials are not inherited.
`APIKey`, when supplied, becomes CODEX_API_KEY; otherwise Codex uses login state
in the agent's own CODEX_HOME. No login flow is implemented here. This is process
configuration isolation, not an OS identity or filesystem security boundary.

Each event reaches the callback. The namespace adapter writes signed session
start, prompt, tool use, completion and failure rows using the agent DID. A successful answer requires a thread ID, `turn.completed`,
and a zero process exit. Malformed output, provider errors, missing completion,
and cancellation return errors. stderr retention is bounded at 4 KiB. Event
records are limited to 16 MiB; larger records fail explicitly.

The CLI arguments and event fields were checked against Codex 0.163.0 source:
[codex-rs/exec/src](https://github.com/openai/codex/tree/83086188ae0614bd882818ff1aa23f3194962012/codex-rs/exec/src).
See also [non-interactive mode](https://developers.openai.com/codex/noninteractive).

## Namespace use

> “make Codex selectable and runnable as a namespace agent, including configuration, protocol responses, session persistence, and signed transcripts.”
>
> “Auth would be another PR, but hold off on it”

Use the existing `agents set` operation with `namespace`, `harness=codex`, an
explicit `model`, and `sandbox=read-only` or `sandbox=workspace-write`. The
namespace owner sets the AGENT line. `agents am`, `agents say` and `agents
session` accept the same namespace and harness, under existing namespace REACH.
See [ADR-053](../../docs/adr/ADR-053-Namespace-Agents.md).

The node prepares the agent identity, home, token and Ground even if no Node
Agent is configured. A turn requires Ground and a pre-provisioned, nonempty
regular `codex/auth.json` inside that agent's home. The CLI validates the
credential. No login, key entry, token refresh flow or node credential fallback
is added. `am.not_ready` reports missing prerequisites. The pinned executable
is fetched on the first authorized turn, or reused if the Node Agent already
holds it.

Codex receives the agent's instructions and MCP token. Its lifecycle hooks run
that agent's Ground executable. QNTX persists its transcript UUID in
`codex-session` before starting the process and the CLI thread ID in
`codex-thread` on `thread.started`. They are distinct: transcript lookup uses
the former; CLI resumption uses the latter, including after a failed turn or
server reload. Ground's own hook records carry the CLI thread ID.

The API returns `NamespaceAgentAm.codex` and `NamespaceAgentSaid.codex`.
Frontend selection in QNTX-Web, namespace authentication setup,
interactive approvals and the complete isolation proof in ADR-053 are outside
this increment. No authenticated external namespace turn was validated for this increment.

## Validation

```sh
go test -tags quickdev ./internal/codex ./server/parity
QNTX_CODEX_ARCHIVE=/absolute/path/to/platform.tar.gz \
  go test -tags quickdev ./internal/codex -run TestReleaseArchive -v
```

The archive test verifies the actual bytes against Parity, installs through
`Ensure`, checks the executable version, and checks both CLI invocation shapes.
It does not infer runtime integrity from GitHub metadata alone.

The opt-in local-model check runs the actual pinned executable, starts and
resumes a thread, and records Ground hook invocations without real credentials:

```sh
QNTX_CODEX_BINARY=/absolute/path/to/verified/codex \
  go test -tags quickdev ./internal/codex -run TestPinnedCLIWithLocalModelAndHooks -v
```
