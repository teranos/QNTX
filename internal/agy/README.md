# 「[Opus 5.5] agy runtime」

> “making a start on agy as another harness”

> “wiring it into node and namespace config as an actual harness”

> “Take ownership of 1456 and correct it for libACP client use for agy”

「[Opus 5.5] This package runs agy on top of Parity's agy pin」
「[Opus 5.5] (`server/parity/agy_1.4.12_841f45d`), beside `internal/claudecode`, `internal/pi`」
「[Opus 5.5] and `internal/codex`. See [ADR-048](../../docs/adr/ADR-048-Node-Agent.md) for the」
「[Opus 5.5] agent's identity and session requirements.」

「[Opus 5.5] agy is run as `agy acp`, its Agent Client Protocol server, through libACP's」
「[Opus 5.5] client ([`internal/acp`](../acp)): the harness is an ACP agent, and nothing here」
「[Opus 5.5] reads agy's own output format. 1.4.12 is the first pinned agy that has `agy acp`;」
「[Opus 5.5] 1.4.4 does not.」

## 「[Opus 5.5] As a harness」

- 「[Opus 5.5] The Node Agent runs in agy when am.toml names `[agent.root.agy]` `model`, on a」
  「[Opus 5.5] node ROOT connected to Google Cloud. It runs as the node's own identity, with no」
  「[Opus 5.5] key: "i dont want byok". That spends what the node's project spends, so it is off」
  「[Opus 5.5] unless named. Asked whether it is held to Opus or Fable, which agy never serves:」
  「[Opus 5.5] “Exempt agy”. `server/agy.go`.」
- 「[Opus 5.5] `model` is the value of the session's `model` option as agy offers it over ACP:」
  「[Opus 5.5] `gemini-3.8-flash`, not the `gemini-3.8-flash-low` of `agy models`. The thinking」
  「[Opus 5.5] level is agy's separate `thought_level` option, which this harness does not set.」
- 「[Opus 5.5] A namespace's agent runs in agy when it is set with `agents set`, harness `agy`」
  「[Opus 5.5] and a model. It runs as its namespace at its owner's Google Cloud, on no key: "A」
  「[Opus 5.5] Namespace Owner logs in with their own oicd for their namespace access to agy, like how」
  「[Opus 5.5] Root does for Node". The owner connects it at」
  「[Opus 5.5] `/auth/google/cloud?thing=agy&namespace=<namespace>`, and the node issues its」
  「[Opus 5.5] identity token for the namespace's agent, with the namespace as a claim its owner's」
  「[Opus 5.5] provider maps. It spends what the owner's project spends, which the node does not」
  「[Opus 5.5] meter. It takes no Task over A2A, so it is not served there. `server/google_namespace.go`,」
  「[Opus 5.5] `server/namespace_agy.go`.」
- 「[Opus 5.5] Its sigils are the other harnesses': `agent say|am|session` and」
  「[Opus 5.5] `agents set|am|say|session`, with `harness` `agy`, answering `AgySaid` and `AgyAm`.」
- 「[Opus 5.5] A turn runs under ground: its ground built, and the global hooks.json under」
  「[Opus 5.5] its HOME (`~/.gemini/config/hooks.json`) running `ground_hook agy <Event>` on」
  「[Opus 5.5] each of agy's six events. agy's ACP session is kept apart from the node's」
  「[Opus 5.5] session, which the transcript is written in, in `agy-conversation`.」
- 「[Opus 5.5] The session's rows are written from ACP's updates, as every ACP harness's are」
  「[Opus 5.5] (`server/acp_rows.go`).」

## 「[Opus 5.5] A turn」

「[Opus 5.5] `Said.Run` starts `agy acp --sandbox --disable-telemetry --log-file <home>/agy-turn.log`」
「[Opus 5.5] in the agent's `work`, and over ACP:」

1. 「[Opus 5.5] `initialize`, offering the agent no file system and no terminal.」
2. 「[Opus 5.5] `authenticate` with `agent-platform`: Application Default Credentials on an」
   「[Opus 5.5] `external_account` file the node writes in the agent's home, with an identity」
   「[Opus 5.5] token fresh for the turn. agy's other `authMethods` are a person's Google sign-in」
   「[Opus 5.5] (`oauth-personal`, `oauth-business`) and a key (`gemini-api-key`). Run headless,」
   「[Opus 5.5] its Google sign-in waited on a code pasted on its terminal.」
3. 「[Opus 5.5] `session/new`, or `session/resume` of the session the agent last answered in.」
4. 「[Opus 5.5] `session/set_config_option` `model`.」
5. 「[Opus 5.5] `session/prompt`, the text whole. Each update reaches the caller as it comes.」
6. 「[Opus 5.5] A `session/request_permission` is answered with agy's reject option: no one is」
   「[Opus 5.5] asked during a turn. A request for a file or a terminal is answered `-32601`.」

「[Opus 5.5] agy ends a turn it could not run as `end_turn`, its message “⚠ Agent execution」
「[Opus 5.5] terminated due to error.”, and writes why only to its log. Run fails that turn」
「[Opus 5.5] with the log's error lines, and fails any stop but `end_turn`.」

「[Opus 5.5] Every run sets `AGY_CLI_DISABLE_AUTO_UPDATE=true`. Without it, agy's first run」
「[Opus 5.5] replaces its own executable with the newest release. Only the value `true` holds」
「[Opus 5.5] it; `1`, `TRUE` and `yes` each let it update. The variable is read out of the」
「[Opus 5.5] binary, not documented.」

「[Opus 5.5] `agy acp` has no `--disable-slash-commands`: a prompt that is one of the session's」
「[Opus 5.5] `available_commands` (`/goal`, `/plan`, …) may run as that command.」

「[Opus 5.5] `Pinned().Ensure(ctx, dir)` downloads the platform archive named by Parity,」
「[Opus 5.5] checks its SHA-256 before extraction, and atomically installs the `antigravity`」
「[Opus 5.5] member as `agy`. On reuse, it verifies the retained archive and extracts again:」
「[Opus 5.5] the archive digest is not a digest of the executable. Linux and macOS, amd64 and」
「[Opus 5.5] arm64, follow the four pins.」

「[Opus 5.5] The agent's HOME is its own directory, holding agy's `.gemini` state, and its」
「[Opus 5.5] working directory is `work`. Node credentials are not inherited. This is process」
「[Opus 5.5] configuration isolation, not an OS identity or filesystem security boundary.」

## 「[Opus 5.5] Remaining integration」

- 「[Opus 5.5] The node's MCP and the agent's system prompt: agy offers no HTTP MCP over ACP」
  「[Opus 5.5] (`mcpCapabilities` absent), and its `systemPrompt.append` is under `_meta`.」
- 「[Opus 5.5] The live tail.」
- 「[Opus 5.5] The QNTX-Web elements and the TypeScript of `AgySaid` and `AgyAm`.」
- 「[Opus 5.5] A live authenticated model turn: run through `agy acp` 1.4.12 with an invalid」
  「[Opus 5.5] key only, which reached Gemini and was refused `400 API key not valid`.」

## 「[Opus 5.5] Validation」

```sh
「[Sonnet 5.5] make libacp」
「[Opus 5.5] go test -tags "libacp quickdev" ./internal/acp/... ./internal/agy ./server/parity」
「[Opus 5.5] QNTX_AGY_ARCHIVE=/absolute/path/to/platform.tar.gz \」
  「[Opus 5.5] go test -tags quickdev ./internal/agy -run TestReleaseArchive -v」
```

「[Opus 5.5] The archive test verifies the actual bytes against Parity, installs through」
「[Opus 5.5] `Ensure`, and checks the executable's version with updates held.」
