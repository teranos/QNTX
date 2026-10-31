# agy runtime foundation

> “making a start on agy as another harness”

This package is the first runtime increment on top of Parity's agy pin
(`server/parity/agy_1.4.4_99b8981`), beside `internal/claudecode`, `internal/pi`
and `internal/codex`. It is not yet registered as a Node Agent or namespace
harness. See [ADR-048](../../docs/adr/ADR-048-Node-Agent.md) for the agent's
identity and session requirements.

`Pinned().Ensure(ctx, dir)` downloads the platform archive named by Parity,
checks its SHA-256 before extraction, and atomically installs the `antigravity`
member as `agy`. On reuse, it verifies the retained archive and extracts again:
the archive digest is not a digest of the executable. Linux and macOS, amd64 and
arm64, follow the four pins.

Every run sets `AGY_CLI_DISABLE_AUTO_UPDATE=true`. Without it, agy's first run
replaces its own executable with the newest release: run headless, 1.4.4 became
1.4.12. Only the value `true` holds it; `1`, `TRUE` and `yes` each let it update.
The variable is read out of the binary, not documented.

`Said.Run` executes one turn with `--input-format stream-json --output-format
stream-json`: the prompt goes in on stdin as one `{"event":"user",...}` message,
never on the command line, and agy ends once stdin closes and the turn completes.
`--disable-slash-commands` keeps a prompt that starts with `/` text. `--sandbox`
is always on. It requires an explicit model (agy's IDs, e.g.
`gemini-3.8-flash-low`) and resumes only the supplied conversation ID. For an ID
it does not have, agy warns and starts another conversation; Run refuses that.
Callers must persist `Answer.Session`, including when a turn returns a partial
answer and an error. Calls for one agent must be serialized by the eventual harness.

The agent's HOME is its own directory, holding agy's `.gemini/antigravity-cli`
state and conversations, and its working directory is `work`. Node credentials
are not inherited. `APIKey` is required and becomes `GEMINI_API_KEY`, with
`modelProvider` set to `gemini` in the agent's agy settings, the other keys kept:
agy reads the key only under that provider. Without a key agy reaches for a
Google sign-in, in a browser and the OS keyring, which are the node's and not
the agent's. This is process configuration isolation, not an OS identity or
filesystem security boundary.

Each event reaches the callback, with tool and subagent payloads retained as JSON
for a future transcript adapter. A successful answer requires a conversation ID
that stays the requested one, a `result` with status `SUCCESS`, and a zero process
exit. Malformed output, an `ERROR` result, a missing result, and cancellation
return errors; agy's `AGY_ERROR` line on stderr is carried in the error. stderr
retention is bounded at 4 KiB. Event records are limited to 16 MiB.

The CLI arguments and event fields were checked against agy 1.4.4's own output
and [headless mode](https://antigravity.google/docs/cli/headless/). agy's source
is not published.

## Remaining integration

- Node and namespace configuration, boot-time binary holding, and harness selection.
- Protocol responses and signed transcript mapping.
- Agent-owned MCP credentials, Google sign-in or ADC, and Ground hooks.
- Permission interaction: headless agy soft-denies a tool it would ask about.
- A live authenticated model turn; current tests use fake subprocesses, plus an
  opt-in real release installation/version check. A real turn was run with an
  invalid key only: it reached Gemini and was refused.

## Validation

```sh
go test -tags quickdev ./internal/agy ./server/parity
QNTX_AGY_ARCHIVE=/absolute/path/to/platform.tar.gz \
  go test -tags quickdev ./internal/agy -run TestReleaseArchive -v
```

The archive test verifies the actual bytes against Parity, installs through
`Ensure`, and checks the executable's version with updates held.
