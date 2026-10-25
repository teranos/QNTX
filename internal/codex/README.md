# Codex runtime foundation

> “making an early start on the Codex Agent runtime”

This package is the first runtime increment on top of PR #1364's Parity pin,
beside `internal/claudecode` and `internal/pi`. It is not yet registered as a
Node Agent or namespace harness. See [ADR-048](../../docs/adr/ADR-048-Node-Agent.md)
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
and an error. Calls for one agent must be serialized by the eventual harness.

The agent's HOME is its own directory, CODEX_HOME is its `codex` subdirectory,
and its working directory is `work`. Node credentials are not inherited.
`APIKey`, when supplied, becomes CODEX_API_KEY; otherwise Codex uses login state
in the agent's own CODEX_HOME. No login flow is implemented here. This is process
configuration isolation, not an OS identity or filesystem security boundary.

Each event reaches the callback, with item payloads retained as JSON for a future
transcript adapter. A successful answer requires a thread ID, `turn.completed`,
and a zero process exit. Malformed output, provider errors, missing completion,
and cancellation return errors. stderr retention is bounded at 4 KiB. Event
records are limited to 16 MiB; larger records fail explicitly.

The CLI arguments and event fields were checked against Codex 0.163.0 source:
[codex-rs/exec/src](https://github.com/openai/codex/tree/83086188ae0614bd882818ff1aa23f3194962012/codex-rs/exec/src).
See also [non-interactive mode](https://developers.openai.com/codex/noninteractive).

## Remaining integration

- Node and namespace configuration, boot-time binary holding, and harness selection.
- Protocol responses and signed transcript mapping; keep Codex's thread ID distinct
  from QNTX's current preallocated session ID.
- Agent-owned MCP credentials, login ceremony, and Ground hooks.
- Approval interaction if the eventual runtime needs it: exec is headless.
- A live authenticated model turn; current tests use fake subprocesses, plus an
  opt-in real release installation/version/CLI check. No model was called.

## Validation

```sh
go test -tags quickdev ./internal/codex ./server/parity
QNTX_CODEX_ARCHIVE=/absolute/path/to/platform.tar.gz \
  go test -tags quickdev ./internal/codex -run TestReleaseArchive -v
```

The archive test verifies the actual bytes against Parity, installs through
`Ensure`, checks the executable version, and checks both CLI invocation shapes.
It does not infer runtime integrity from GitHub metadata alone.
