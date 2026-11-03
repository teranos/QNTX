# agy runtime

> “making a start on agy as another harness”

> “wiring it into node and namespace config as an actual harness”

> “Take ownership of 1456 and correct it for libACP client use for agy”

This package runs agy on top of Parity's agy pin
(`server/parity/agy_1.4.12_841f45d`), beside `internal/claudecode`, `internal/pi`
and `internal/codex`. See [ADR-048](../../docs/adr/ADR-048-Node-Agent.md) for the
agent's identity and session requirements.

agy is run as `agy acp`, its Agent Client Protocol server, through libACP's
client ([`internal/acp`](../acp)): the harness is an ACP agent, and nothing here
reads agy's own output format. 1.4.12 is the first pinned agy that has `agy acp`;
1.4.4 does not.

## As a harness

- The Node Agent runs in agy when am.toml names `[agent.root.agy]` `model`, on a
  node ROOT connected to Google Cloud. It runs as the node's own identity, with no
  key: "i dont want byok". That spends what the node's project spends, so it is off
  unless named. Asked whether it is held to Opus or Fable, which agy never serves:
  “Exempt agy”. `server/agy.go`.
- `model` is the value of the session's `model` option as agy offers it over ACP:
  `gemini-3.8-flash`, not the `gemini-3.8-flash-low` of `agy models`. The thinking
  level is agy's separate `thought_level` option, which this harness does not set.
- Its sigils are the other harnesses': `agent say|am|session` and
  `agents set|am|say|session`, with `harness` `agy`, answering `AgySaid` and `AgyAm`.
- A turn runs under ground: its ground built, and the global hooks.json under
  its HOME (`~/.gemini/config/hooks.json`) running `ground_hook agy <Event>` on
  each of agy's six events. agy's ACP session is kept apart from the node's
  session, which the transcript is written in, in `agy-conversation`.
- The session's rows are written from ACP's updates, as every ACP harness's are
  (`server/acp_rows.go`).

## A turn

`Said.Run` starts `agy acp --sandbox --disable-telemetry --log-file <home>/agy-turn.log`
in the agent's `work`, and over ACP:

1. `initialize`, offering the agent no file system and no terminal.
2. `authenticate` with `agent-platform`: Application Default Credentials on an
   `external_account` file the node writes in the agent's home, with an identity
   token fresh for the turn. agy's other `authMethods` are a person's Google sign-in
   (`oauth-personal`, `oauth-business`) and a key (`gemini-api-key`). Run headless,
   its Google sign-in waited on a code pasted on its terminal.
3. `session/new`, or `session/resume` of the session the agent last answered in.
4. `session/set_config_option` `model`.
5. `session/prompt`, the text whole. Each update reaches the caller as it comes.
6. A `session/request_permission` is answered with agy's reject option: no one is
   asked during a turn. A request for a file or a terminal is answered `-32601`.

agy ends a turn it could not run as `end_turn`, its message “⚠ Agent execution
terminated due to error.”, and writes why only to its log. Run fails that turn
with the log's error lines, and fails any stop but `end_turn`.

Every run sets `AGY_CLI_DISABLE_AUTO_UPDATE=true`. Without it, agy's first run
replaces its own executable with the newest release. Only the value `true` holds
it; `1`, `TRUE` and `yes` each let it update. The variable is read out of the
binary, not documented.

`agy acp` has no `--disable-slash-commands`: a prompt that is one of the session's
`available_commands` (`/goal`, `/plan`, …) may run as that command.

`Pinned().Ensure(ctx, dir)` downloads the platform archive named by Parity,
checks its SHA-256 before extraction, and atomically installs the `antigravity`
member as `agy`. On reuse, it verifies the retained archive and extracts again:
the archive digest is not a digest of the executable. Linux and macOS, amd64 and
arm64, follow the four pins.

The agent's HOME is its own directory, holding agy's `.gemini` state, and its
working directory is `work`. Node credentials are not inherited. This is process
configuration isolation, not an OS identity or filesystem security boundary.

## Remaining integration

- The node's MCP and the agent's system prompt: agy offers no HTTP MCP over ACP
  (`mcpCapabilities` absent), and its `systemPrompt.append` is under `_meta`.
- The live tail.
- The QNTX-Web elements and the TypeScript of `AgySaid` and `AgyAm`.
- A live authenticated model turn: run through `agy acp` 1.4.12 with an invalid
  key only, which reached Gemini and was refused `400 API key not valid`.

## Validation

```sh
make libacp
go test -tags "libacp quickdev" ./internal/acp/... ./internal/agy ./server/parity
QNTX_AGY_ARCHIVE=/absolute/path/to/platform.tar.gz \
  go test -tags quickdev ./internal/agy -run TestReleaseArchive -v
```

The archive test verifies the actual bytes against Parity, installs through
`Ensure`, and checks the executable's version with updates held.
