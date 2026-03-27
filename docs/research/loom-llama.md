# Loom x llama-cpp: Token Confidence in the Timeline

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

The llama-cpp plugin streams tokens over WebSocket as `LLMStreamMessage` (`server/types.go`). Each message carries:
- `content`: the token text
- `signal`: optional `LLMTokenSignal` with `confidence` (P(chosen)), `entropy` (Shannon entropy in bits), `top_gap` (P(top1) - P(top2)), `top_k` (candidate tokens with probabilities)

**Stream glyph** (`web/ts/components/glyph/stream-glyph.ts`). Renders tokens as `<span>` elements with confidence-to-color mapping: high confidence (>0.9) is transparent, low confidence glows amber/orange. Signal data stored in `data-*` attributes on each span. Tokens persist to canvas state for page refresh survival.

**Token popup** (`web/ts/components/glyph/token-popup.ts`). Hover overlay shows P (confidence), H (entropy), delta (top_gap), and a bar chart of top-K candidates with the chosen token highlighted.

**Multiplexer pattern.** One WebSocket handler routes `llm_stream` messages to stream glyph instances by `job_id`. Each stream glyph subscribes with its prompt glyph's ID as key.

The inference-internals checklist (`docs/research/inference-internals.md`) includes: "Write per-generation attestations with signal attributes to ATS." This is the bridge item -- once generations become attestations, they become consumable by loom.

## 3. Integration Paths

### 3A. Generation as weave source: a new predicate

Today loom consumes attestations with predicates like UserPromptSubmit, Stop, PreToolUse. A generation from llama-cpp could produce attestations with a new predicate (e.g., `"LLMGeneration"`) carrying the full token stream with signal data in attributes.

**Attestation shape:**
```json
{
  "subjects": ["model:qwen-2.5-7b"],
  "predicates": ["LLMGeneration"],
  "contexts": ["glyph:stream-abc-123"],
  "attributes": {
    "prompt": "...",
    "model": "qwen-2.5-7b",
    "token_count": 247,
    "mean_confidence": 0.83,
    "mean_entropy": 1.42,
    "low_confidence_count": 12,
    "tokens": [
      { "text": "The", "confidence": 0.97, "entropy": 0.3, "top_gap": 0.85 },
      { "text": " key", "confidence": 0.41, "entropy": 3.1, "top_gap": 0.08, "top_k": [...] }
    ]
  }
}
```

This maps directly to `LLMTokenSignal` from `server/types.go`. The `tokens` array is the same data that today lives ephemerally in WebSocket messages and `data-*` attributes on spans.

**How loom consumes it.** Loom would need a new code path alongside the Graunde UDP path. Options:
1. **ATS query**: loom already queries ATS for `["Weave"]` predicates. It could additionally query for `["LLMGeneration"]` and serve them through the HTTP API as a separate data type.
2. **UDP extension**: the llama-cpp plugin (or the server) sends the generation attestation to loom's UDP port, same as Graunde does today. Loom's stitcher would need a new parser alongside the Graunde attestation parser and the JSONL reader.
3. **Direct ATS reads**: the loom frontend fetches generation attestations directly from QNTX's ATS API, bypassing the OCaml backend entirely for this data type.

Option 1 is cleanest -- loom already does ATS reads, adding a second predicate filter is minimal OCaml work.

### 3B. Each generation as a weave, each token as a turn

The structural parallel: a **weave** is a chunk of conversation turns. A **generation** is a chunk of tokens. If each generation becomes a weave, each token becomes a turn.

Today a turn is `[speaker] text`. A token-turn would be `[token] text` with signal metadata. But the flat-string weave format (`[speaker] text\n\n[speaker] text`) cannot carry structured signal data (confidence, entropy, top-k per token). Two options:

1. **Structured weave format.** The weave `text` field becomes JSON instead of flat text. This is a breaking change to the weave format but the README already identifies "structured turns" as an upstream gap: "A structured format (array of typed turns) would eliminate parsing fragility." If turns become structured objects, token signal data fits naturally as fields on each turn object.

2. **Separate attributes, not inline.** Keep weave text as flat text for conversation weaves. For generation weaves, store the token array in a dedicated `tokens` attribute (as in 3A above), not in the `text` field. The frontend distinguishes by predicate or by presence of the `tokens` attribute.

Option 2 is more practical because it avoids breaking the existing weave format and acknowledges that generation data is structurally different from conversation data.

### 3C. Confidence-driven weave boundaries in the stitcher

Today the stitcher emits a weave when the buffer hits 150 words or a session boundary occurs. For generation weaves, confidence could drive boundary decisions:

- **Low-confidence spans as natural boundaries.** A run of tokens where confidence drops below a threshold (e.g., < 0.3) marks a region of model uncertainty. The stitcher could treat sustained low confidence as a semantic boundary -- the model is "changing its mind" about where the generation is going. Emit the weave at the end of the low-confidence span.

- **Entropy spikes as weave breaks.** A sudden entropy increase (e.g., > 2x the running mean) signals a transition point in the generation. The inference-internals doc already identifies "entropy spikes" and "low-confidence spans" as signal patterns worth porting from the D prototype to C++.

- **Fixed token count with confidence annotation.** Simpler: chunk every N tokens (matching the 150-word conversation chunk size), but annotate each generation weave with aggregate signal stats (mean confidence, max entropy, low-confidence token count). The frontend uses these aggregate stats for visual weight in the timeline without changing the chunking logic.

The third option is the pragmatic starting point. Confidence-driven boundaries are interesting but require tuning thresholds per model (different models have different confidence distributions), which is research work on top of integration work.

### 3D. Frontend visualization: confidence in the timeline

The loom frontend today renders turns as colored-by-speaker text blocks. For generation weaves, the same vertical timeline could show tokens colored-by-confidence, reusing the stream glyph's `confidenceToColor` mapping (amber for low confidence, transparent for high).

**Concrete UI changes:**

1. **Generation weaves in the timeline.** A new visual treatment for weaves that contain token signal data. Instead of `[speaker] text` turns, render a token flow with confidence heatmap coloring. This is the stream glyph's rendering (`renderToken` in `stream-glyph.ts`) transplanted into loom's `Weave.svelte`.

2. **Warp lane for confidence.** The TimeWarp minimap has lanes for branch, session, and cluster. A fourth lane could show confidence: each weave segment colored by mean confidence (green = high, amber = low). Low-confidence weaves would visually stand out in the minimap, making "where did the model struggle?" scannable across the full timeline.

3. **Token popup in loom.** The `createTokenPopup` from `token-popup.ts` shows P, H, delta, and top-K candidates. The same popup works in loom -- hover a token in a generation weave to see its signal data. The popup code is standalone (no QNTX-specific dependencies beyond types), extractable to a shared component.

4. **Confidence filtering.** Add a slider or threshold control: "show only tokens where confidence < X." This highlights the interesting parts of a generation -- where the model hesitated, where alternatives were close, where entropy was high. Pairs with the existing turn selection mechanism (click to select, CMD+C to copy).

5. **Cross-column confidence comparison.** Loom already aligns multiple projects/branches temporally. If two branches ran the same prompt through different models (or different sampling parameters), their generation weaves appear in parallel columns. Confidence heatmaps make the comparison visual -- you see where each model struggled.

### 3E. Connection to "attestation with signal attributes"

The inference-internals checklist item "Write per-generation attestations with signal attributes to ATS" is the prerequisite for everything above. Once a generation is an attestation in ATS:

- Loom can query it (it already queries ATS for weave attestations)
- The signal attributes (confidence, entropy, top_gap, top_k) become part of the attestation's `attributes` struct
- Loom's existing infrastructure (ATS reads -> JSON serialization -> HTTP API -> frontend fetch) carries the data without new transport
- The attestation's `contexts` field links it to the glyph that triggered it (e.g., `"glyph:stream-abc-123"`), providing the same kind of session/context grouping that conversation weaves use

This is the same pattern loom already implements: Graunde writes attestations to ATS with signal data in attributes (prompt text, assistant message, tool commands). The llama-cpp plugin would write attestations to ATS with different signal data (token confidence, entropy, top-k). Loom reads both.

## 4. Open Questions

**Weave text format for generations.** The current flat `[speaker] text\n\n` format cannot carry per-token signal data. Do generation weaves use a different format (JSON token array in attributes), or does this push the entire weave format toward structured turns? The README already identifies this as an upstream gap.

**Data volume.** A 500-token generation with full top-k (k=10) per token is ~50KB of signal data. Conversation weaves are small (150 words of text). Generation weaves with full signal data are 100x larger. Does ATS handle this? Does the loom frontend's "no virtualization" limitation (README: "every weave and turn is in the DOM") become a blocker when generation weaves contain hundreds of tokens each?

**Transport path.** Should generation attestations flow through loom's UDP listener (like Graunde events), through direct ATS queries (loom reads from ATS), or should the loom frontend fetch them directly from QNTX's API? The UDP path requires the llama-cpp plugin or server to send datagrams to loom. The ATS query path is simpler but means loom needs to poll or be notified of new attestations.

**Token-level granularity vs. weave-level.** Loom's smallest unit today is a turn. Making each token a "turn" in the stitcher sense means hundreds of turns per generation weave. The stitcher's dedup logic (`Skip duplicate` in `stitch_turn`) and word-count chunking don't apply to tokens. A generation needs its own chunking logic or none at all (one generation = one weave, no splitting).

**Model-specific confidence baselines.** Different models have different confidence distributions. A 0.4 confidence from a 7B model means something different than 0.4 from a 70B model. Should loom normalize confidence per model before visualization, or show raw values and let the user learn each model's baseline? The stream glyph currently shows raw values.

**Shared components.** The token popup (`token-popup.ts`) and confidence-to-color mapping (`confidenceToColor` in `stream-glyph.ts`) would be duplicated between the QNTX web frontend and the loom Svelte frontend. The loom README already notes "Share components with QNTX/web" as a missing feature. This integration increases the urgency.

**Live streaming into loom.** Loom has no live update mechanism today (README: "No live updates: data fetched once on load"). Generation weaves written to ATS during active inference won't appear until refresh. Adding WebSocket or SSE push from loom's HTTP API is a separate piece of work but necessary for the experience of watching a generation appear in the timeline as it streams.
