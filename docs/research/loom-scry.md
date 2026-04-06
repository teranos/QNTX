# Loom x Scry: Token Confidence in the Timeline

How LLM inference signal data (confidence, entropy, top-gap, top-k) could flow into loom's stitching system and timeline UI.

## 1. How Loom Works Today

Loom is an OCaml plugin that receives conversation events and chunks them into "weaves" -- embedding-sized text blocks stored as attestations in ATS.

**Data ingestion.** Two paths:

- **UDP listener** (`lib/udp_listener.ml`, port 19470): Graunde sends a JSON attestation datagram on every Claude Code hook event (UserPromptSubmit, Stop, PreToolUse, SessionStart/End, etc.). Fire-and-forget. This is the primary live path.
- **JSONL import** (`lib/http_api.ml`, POST /api/import): Reads historical Claude Code session files (`~/.claude/projects/{slug}/{uuid}.jsonl`) and feeds them through the same stitcher.

**Stitching** (`lib/stitcher.ml`). The stitcher buffers incoming turns per branch+session key. Each turn gets a `[speaker]` prefix label (human, assistant, tool, edit, read, etc.) and is appended to a buffer. When the buffer exceeds `max_chunk_words` (150 words), or a session boundary occurs (SessionStart/SessionEnd), the buffer is flushed as a weave. Branch changes also trigger a flush. The emitted block is a flat string of `[speaker] text` entries joined by `\n\n`.

**Weave persistence** (`lib/ats_client.ml`). Each weave is written to ATS as an attestation with:
- `subjects`: the branch name
- `predicates`: `["Weave"]`
- `contexts`: the session context
- `attributes`: text, word_count, turn_count, paths (file tail -> full path mapping), weave_source ("graunde" or "jsonl")

**HTTP API** (`lib/http_api.ml`, port 5178). Read-only JSON endpoints: GET /api/weaves returns all weaves grouped by branch, GET /api/weaves/branch returns weaves for a single branch, GET /api/sessions lists discoverable session files with import state.

**Frontend** (`frontend/src/`). Svelte 5 app. `App.svelte` fetches weaves and cluster data, groups by project (branch prefix before `:`), renders vertical chronological columns per project. `Weave.svelte` parses the flat `[speaker] text` format via `turns.ts` and renders each turn with speaker-colored labels. `Warp.svelte` is a minimap/scrollbar with branch, session, and cluster color lanes. `BranchBar.svelte` and `ClusterBar.svelte` show metadata in project headers. Time spacers create visual gaps between temporally distant weaves.

Key point: loom's unit of data is the **turn** (a single speaker utterance within a weave), and its unit of storage is the **weave** (a 150-word chunk of turns). The frontend's smallest interactive element is a turn within a weave.

## 2. How Stream Results Work Today

The scry plugin streams tokens over WebSocket as `LLMStreamMessage` (`server/types.go`). Each message carries:
- `content`: the token text
- `signal`: optional `LLMTokenSignal` with `confidence` (P(chosen)), `entropy` (Shannon entropy in bits), `top_gap` (P(top1) - P(top2)), `top_k` (candidate tokens with probabilities)

**Stream glyph** (`web/ts/components/glyph/stream-glyph.ts`). Renders tokens as `<span>` elements with confidence-to-color mapping: high confidence (>0.9) is transparent, low confidence glows amber/orange. Signal data stored in `data-*` attributes on each span. Tokens persist to canvas state for page refresh survival.

**Token popup** (`web/ts/components/glyph/token-popup.ts`). Hover overlay shows P (confidence), H (entropy), delta (top_gap), and a bar chart of top-K candidates with the chosen token highlighted.

**Multiplexer pattern.** One WebSocket handler routes `llm_stream` messages to stream glyph instances by `job_id`. Each stream glyph subscribes with its prompt glyph's ID as key.

The inference-internals checklist (`docs/research/inference-internals.md`) includes: "Write per-generation attestations with signal attributes to ATS." This is the bridge item -- once generations become attestations, they become consumable by loom.

## 3. Integration Paths

### 3A. Generation as weave source: a new predicate

Today loom consumes attestations with predicates like UserPromptSubmit, Stop, PreToolUse. A generation from scry produces two levels of attestation: the generation as a weave, each token as an individual attestation within it.

**Two attestation levels.** The generation is a weave. Each token is an attestation within it.

**Weave attestation** (one per generation):
```json
{
  "subjects": ["model:qwen-2.5-7b"],
  "predicates": ["Weave"],
  "contexts": ["glyph:stream-abc-123"],
  "attributes": {
    "prompt": "...",
    "model": "qwen-2.5-7b",
    "token_count": 247,
    "mean_confidence": 0.83,
    "mean_entropy": 1.42,
    "weave_source": "scry"
  }
}
```

**Token attestation** (one per token):
```json
{
  "subjects": ["model:qwen-2.5-7b"],
  "predicates": ["Token"],
  "contexts": ["glyph:stream-abc-123"],
  "attributes": {
    "text": " key",
    "position": 14,
    "confidence": 0.41,
    "entropy": 3.1,
    "top_gap": 0.08,
    "top_k": [
      { "text": " main", "prob": 0.38 },
      { "text": " core", "prob": 0.12 }
    ]
  }
}
```

The weave uses the same `["Weave"]` predicate loom already queries — no new code path needed. The tokens are individual attestations linked to the weave by shared context. Loom renders the weave in the timeline; token attestations provide the signal data for confidence coloring and hover popups within it. A single prompt + generation already feels like an entire weave given the amount of output it produces — no stitcher chunking needed.

**How loom consumes it.** No new code path needed. The weave attestation uses the standard `["Weave"]` predicate with `weave_source: "scry"`. Loom's existing ATS query for `["Weave"]` picks it up automatically. Token attestations (`["Token"]` predicate) are fetched on demand when the frontend renders a generation weave — query by shared context to get the per-token signal data.

### 3B. Each generation as a weave, each token as an attestation

The structural parallel: a **weave** is a chunk of conversation turns. A **generation** is a chunk of tokens. Each generation becomes a weave, each token becomes its own attestation — not a turn inside the weave's flat text, but an independent attested event linked by shared context.

This sidesteps the flat-string format problem entirely. The weave attestation carries aggregate stats (mean confidence, mean entropy, token count). The per-token attestations carry the full signal data. The weave text field can hold the plain response text for display; the signal data lives in the token attestations, not inline.

### 3C. No stitcher involvement

The stitcher chunks conversation turns because conversations are open-ended streams of events with no natural boundary. A generation has a natural boundary: it starts when sampling begins and ends at EOS or max tokens. One generation = one weave. The stitcher doesn't touch it.

Confidence-driven weave boundaries (splitting a generation at entropy spikes or low-confidence spans) are deferred. They're interesting for very long generations but require threshold tuning per model and add complexity before the basic flow works.

### 3D. Frontend visualization: confidence in the timeline

The loom frontend today renders turns as colored-by-speaker text blocks. For generation weaves, the same vertical timeline could show tokens colored-by-confidence, reusing the stream glyph's `confidenceToColor` mapping (amber for low confidence, transparent for high).

**Concrete UI changes:**

- **WGIT** — Generation weaves in the timeline. A new visual treatment for weaves that contain token signal data. Instead of `[speaker] text` turns, render a token flow with confidence heatmap coloring. This is the stream glyph's rendering (`renderToken` in `stream-glyph.ts`) transplanted into loom's `Weave.svelte`.

- **TPIL** — Token popup in loom. The `createTokenPopup` from `token-popup.ts` shows P, H, delta, and top-K candidates. The same popup works in loom -- hover a token in a generation weave to see its signal data. The popup code is standalone (no QNTX-specific dependencies beyond types), extractable to a shared component.

### 3E. Prerequisites

The C++ plugin writes directly to ATS after a generation completes — it has the tokens, the signal data, the model name, and it already receives `ats_store_endpoint` during Initialize (currently ignored). One `["Weave"]` attestation for the generation, one `["Token"]` attestation per token. No Go server involvement in the write path. Loom's existing `["Weave"]` query picks up generation weaves automatically. The frontend fetches `["Token"]` attestations by shared context when rendering a generation weave.

## 4. Open Questions

**Weave text format for generations.** Resolved. The weave attestation carries the plain response text in its `text` attribute — same format as conversation weaves. Per-token signal data lives in separate `["Token"]` attestations, not inline. No format change needed.

**Data volume.** Each token is its own attestation. A 500-token generation produces 501 attestations (1 weave + 500 tokens). Each token attestation is small (~200 bytes without top-k, ~500 bytes with top-5). Total: ~250KB for a 500-token generation. ATS handles this — it's designed for high-volume attestation writes. The loom frontend fetches token attestations on demand per weave, not all at once.

**Transport path.** Resolved. The C++ plugin writes both the weave and token attestations to ATS directly after the generation completes. Loom reads weaves via its existing ATS query. Token attestations are fetched by the loom frontend on demand when rendering a generation weave.

**Token-level granularity vs. weave-level.** Resolved. Each token is its own attestation, not a turn in the stitcher sense. The stitcher is not involved. One generation = one weave, no splitting. Token attestations are linked to the weave by shared context.

**Model-specific confidence baselines.** Different models have different confidence distributions. A 0.4 confidence from a 7B model means something different than 0.4 from a 70B model. Should loom normalize confidence per model before visualization, or show raw values and let the user learn each model's baseline? The stream glyph currently shows raw values.
