# Claude as a QNTX Plugin

QNTX already knows Claude — from the outside. Three seams observe it:

| Seam | What it captures | Where |
|---|---|---|
| [Ground](https://github.com/teranos/ground) | Hook events (`UserPromptSubmit`, `Stop`, `PreToolUse`, `SessionStart`, `PreCompact`, `SubagentStart/Stop`) as attestation JSON over UDP | `qntx-plugins/loom`, port 19470 |
| loom | Stitches those turns into embedding-sized weaves, writes to ATS, serves a timeline | `qntx-plugins/loom` |
| ix-net | MITM proxy on `api.anthropic.com` — model, token usage, prompt text, images | `qntx-plugins/ix-net`, captured in [claude-api-wire-format.md](../research/claude-api-wire-format.md) |

Ground also runs the other way: `ground_db_path` in am.toml and `GroundService` in [ground.proto](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/ground.proto) let plugins write deferred news into a Claude Code session on `Stop`.

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

## Conversation is a DAG, not a thread

From [ADR-014](../adr/ADR-014-llm-as-plugin-provided-service.md):

> Conversation state lives on the canvas — QNTX has no linear chat session.

`ConversationAssembler` (`server/conversation.go`) walks the composition DAG upstream from the current glyph, queries prompt-result attestations for each ancestor, and builds an ordered message array sorted by timestamp. Neither `scry` nor OpenRouter stores conversation state; they receive a snapshot and execute.

A Claude plugin inherits that. It has no session. Its history is assembled by traversing composition edges, and its position in the graph decides what it sees. The [AXIOMS](../AXIOMS.md) then govern when it runs:

- **One attestation, one execution.** A melded Claude glyph fires once per incoming attestation. Five upstream attestations, five executions.
- **Watching, not polling.** The meld edge is a live subscription.
- **Subscriptions compile eagerly.** The DAG is live from the moment it is assembled — not on play.

Claude as a node in a DAG, not a chat window.

## Output is attestation

Everything the plugin concludes becomes `[Subject] is [Predicate] of [Context] by [Actor] at [Time]`. [attestation.md](../attestation.md) already has the properties this needs:

- **Actor-bearing.** Every attestation knows who said it. Two actors can make contradictory claims about the same subject — both are valid. A model's judgment is *a claim by an actor*, never a fact of record. The substrate says so structurally; no convention required.
- **No retraction.** To supersede a claim, attest a new one. Revision without erasure.
- **Convergent.** Sync is set union. Two nodes each running the plugin do not conflict; they accumulate.

And memory decays rather than being deleted: bounded storage distills evicted attestations into sigmas (Σ) that preserve statistical shape — min/max/sum/count, histograms, frequencies — and sigmas are themselves attestations, recursively meta-distillable ([ADR-020](../adr/ADR-020-attestation-distillation.md)). The wire format handles the same pressure with `clear_thinking_20251015`. QNTX handles it with Σ.

## Continuity: turns end, schedules don't

A Claude Code turn ends and the process stops. A plugin does not.

`GetHandlerNames() []string` and `ExecuteJob(ctx, handlerName, jobID, payload) (result, []*protocol.JobLogEntry, error)` are duck-typed at the gRPC boundary (`plugin/grpc/server.go:240`, `:485`). `ScheduleService.Create(handlerName, intervalSecs, payload, metadata)` creates a recurring Pulse schedule at runtime. `LLMChatRequest.priority` already distinguishes `0 = interactive` from `10 = background`.

That is the structural gain: work outlives the turn, and the queue knows the difference between a person waiting and a job that isn't.

## Manifestation

`UIPlugin.RegisterGlyphs() []GlyphDef` (`plugin/interface.go:146`) lets a plugin ship its own glyph type: `Symbol`, `Title`, `Label`, and either a `ContentPath` (server-rendered HTML) or a `ModulePath` (a TS/JS module exporting `render(glyph, ui)`). Claude in QNTX is not a chat pane — it is a glyph in the GlyphRun that manifests when you approach it, and remembers where it was.

The symbol is an open question, not a proposal. `GlyphDef.Symbol` must not collide with the `sym` package, and [SYMBOLS.md](../SYMBOLS.md) is the register.

## What doesn't fit

Honest list. These are the reasons this is a vision document and not a plan.

**1. Tool use has no wire representation.** `ChatMessage` is `{role, content}` — two strings. There is no `tool_use`, no `tool_result`, no way for a provider to hand back "call this, then ask me again". A Claude *agent* plugin therefore does not use `LLMService`; it needs a different contract or none at all. This is the load-bearing gap.

**2. No token signal.** `TokenSignalProto` carries confidence, entropy, top-gap, top-k candidates, the full softmax distribution, and per-stage sampler snapshots. `scry` exists to extract exactly this — issues #715–#717 build on it. The captured wire format shows usage counts in `message_start`/`message_delta` and nothing else; a hosted API exposes no distribution. Anything QNTX builds on signal is structurally blind here.

**3. Version discipline.** CLAUDE.md: *any* edit to a plugin bumps `Metadata().Version`, because the version is the only way to confirm new code is running. A plugin whose behavior is a model changes without a source edit. Semver over a nondeterministic process is a contract that does not describe what it names.

**4. Actor identity.** Attestations are actor-bearing today; DID *signing* of attestations is [issue #576](https://github.com/teranos/QNTX/issues/576), step 2 of the [identity](identity.md) path. When it lands: who signs? The plugin's process identity, the node DID, the user's delegated key? "By whom" is the field that makes a claim situated, and for this actor it is genuinely unsettled.

**5. Resource allocation.** This one is already open, in `plugin/README.md`, in the user's own words:

> Werf nominating a lot of matches, and then Voor who decides what should be something we spend an LLM call on, and then there is Pulse who will also manage a queue of work and will decide how to allocate resources to each. […] Resource allocation has domain implications, so how do you manage this?

A Claude plugin makes the most expensive calls in the system and sits directly in that contested zone. It does not answer the question. It sharpens it.

**6. Credentials.** [ADR-025](../adr/ADR-025-access-tokens.md) and `[[plugin.access_token]]`: values are references, never secrets — `ssm://` or `env:` — and a literal is rejected at config load. Claude Code's captured traffic authenticates with an OAuth token (`sk-ant-oat01-…`), not an API key, so the reference resolves to something with a lifetime.

## Related

- [Continuous Intelligence](continuous-intelligence.md) — always ingesting, always processing, always available
- [Glyphs](glyphs.md) — the universal manifestation primitive
- [Identity](identity.md) — DIDs, delegations, and who gets to sign
- [ADR-001](../adr/ADR-001-domain-plugin-architecture.md), [ADR-014](../adr/ADR-014-llm-as-plugin-provided-service.md) — plugin architecture, LLM as plugin-provided service
