# Claude as a QNTX Plugin

QNTX already knows Claude — from the outside. Three seams observe it:

| Seam | What it captures | Where |
|---|---|---|
| [Ground](https://github.com/teranos/ground) | Hook events (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, `SessionStart`/`SessionEnd`, `PreCompact`, `SubagentStart`/`SubagentStop`) as attestation JSON, fire-and-forget UDP | `source/loom.d` → `qntx-plugins/loom`, port 19470 |
| loom | Stitches those turns into embedding-sized weaves, writes to ATS, serves a timeline | `qntx-plugins/loom` |
| ix-net | MITM proxy on `api.anthropic.com` — model, token usage, prompt text, images | `qntx-plugins/ix-net`, captured in [claude-api-wire-format.md](../research/claude-api-wire-format.md) |

Ground also runs the other way: `ground_db_path` in am.toml and `GroundService` in [ground.proto](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/ground.proto) let plugins write deferred news into Ground's database, and Ground drains it on `Stop` — `readProjectDeferredMessage` in its `source/stop.d`, under a comment that names QNTX as the source.

All three are observation. A plugin is participation. This document is about what changes when Claude is inside the process boundary rather than behind it.

## The obvious implementation is the wrong one

`plugin.LLMProvider` (`plugin/interface.go:159`) is one method: `Chat(ctx, LLMRequest) (*LLMResponse, error)`. `qntx-openrouter`, `scry`, and `gaze` all implement it. Registering Claude the same way is a morning's work.

It is also a demotion. Compare what the contract carries against what the wire actually carries:

| `llm.proto` `ChatMessage` | Observed in `POST /v1/messages` |
|---|---|
| `role`, `content` (string) | `text`, `image`, `thinking`, `tool_use`, `tool_result` content blocks |
| — | `tools` array with JSON Schema per tool |
| — | `context_management.edits`, `output_config.effort` |

The service contract carries turns. Claude Code carries *tool-using* turns. A completion endpoint is what `scry` and `gaze` are — model in, tokens out. Claude Code is a process that reads, edits, greps, and runs commands between turns. Flattening it through `LLMService.Chat` produces a very expensive `gaze`.

## The inversion: ServiceRegistry is the tool surface

The interesting direction is the other one. `ServiceRegistry` (`plugin/services.go:227`) is what core hands a plugin at `Initialize`:

| Service | What it becomes as a tool |
|---|---|
| `ATSStore()` | Read and write attestations — ⋈ ax over the claim graph |
| `Search()` | Full-text over indexed documents (qntx-meili) |
| `VectorSearch()` | Nearest-neighbour over dense vectors (ADR-016) |
| `Queue()`, `Schedule()` | Defer work past the end of a turn |
| `FileService()` | Read files stored on the core server |
| `LLM()` | Call a model — including a local one |
| `Config()`, `Logger()` | Configuration with source traceability; a named, versioned logger |

Claude Code's tools reach a filesystem. A Claude plugin's tools reach an attestation graph. That is the whole substitution, and it runs both ways: a plugin *consumes* `services.LLM()`, and `LLMRequest.Provider` targets a specific backend — so cheap subtasks dispatch to `gaze` or `scry` locally while the expensive judgment stays remote.
