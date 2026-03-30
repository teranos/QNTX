# Weave Token Structure (llama-cpp)

Weaves from `llama-cpp` embed per-token signal data in `attributes.tokens`.
Standard loom weaves (from graunde) do not have this field.

## Identifying llama-cpp weaves

- `predicate: ["Weave"]` (same as all weaves)
- `attributes.weave_source: "llama-cpp"`
- `actors: ["llama-cpp"]`

## Top-level attributes

| Field              | Type   | Description                        |
|--------------------|--------|------------------------------------|
| `prompt`           | string | User prompt                        |
| `text`             | string | Full generated response            |
| `model`            | string | Model name (e.g. "Llama 3.2 3B")  |
| `token_count`      | number | Total tokens generated             |
| `mean_confidence`  | number | Mean P(chosen) across all tokens   |
| `mean_entropy`     | number | Mean Shannon entropy (bits)        |
| `weave_source`     | string | Always `"llama-cpp"`               |
| `tokens`           | list   | Per-token signal array (see below) |

## Token signal structure

Each entry in `attributes.tokens`:

| Field        | Type   | Description                              |
|--------------|--------|------------------------------------------|
| `text`       | string | Token text (UTF-8, U+FFFD for bad bytes) |
| `position`   | number | 0-indexed position in generation         |
| `confidence` | number | P(chosen) from raw softmax distribution  |
| `entropy`    | number | Shannon entropy in bits                  |
| `top_gap`    | number | P(top1) - P(top2)                        |
| `top_k`      | list   | Top-k candidates (optional, see below)   |

### top_k entry

| Field  | Type   | Description          |
|--------|--------|----------------------|
| `text` | string | Candidate token text |
| `prob` | number | Softmax probability  |

## Context linking

All llama-cpp weaves use a context like `stream:<epoch_ns>` or `chat:<epoch_ns>`.
One weave per generation — no separate Token attestations.

## Bounded storage

One attestation per generation avoids the 16-per-(actor, context) eviction limit.
Token data lives inside the weave's attributes, not as separate attestations.

## Performance attestation

llama-cpp weaves (v0.23.0+) include `attributes.performance`:

| Field             | Type   | Description                           |
|-------------------|--------|---------------------------------------|
| `prompt_eval_ms`  | number | Prompt decode into KV cache           |
| `generation_ms`   | number | Total generation loop                 |
| `decode_ms`       | number | llama_decode calls only               |
| `signal_ms`       | number | capture_signal (softmax + partial sort) |
| `callback_ms`     | number | Token callback (proto + renderer + gRPC) |
| `tokens_per_sec`  | number | Computed: completion_tokens * 1000 / generation_ms |

Loom renders this as `{tok/s} ({generation_ms}ms)` next to the model name. Hover for the full breakdown.

## Limitations

- **MWP** — Model warp placement. llama-cpp weaves use `model:X` as their attestation subject (e.g. `model:Llama 3.2 3B Instruct`). Loom treats subjects as project/column keys, so these weaves get lumped under a column called `model` — mixed in with graunde conversation weaves. Local model generations should get their own warp, grouped per model.

- **TBR** — Token branch exploration. Token weaves bypass turn-level selection, so click-to-select and CMD+C copy don't work. Will tie into loom branch exploration — clicking a low-confidence token to explore the alternative path the model didn't take.

- **TDO** — Token DOM overhead. Each token is a separate `<span>` element. Will become an issue at high token counts across many weaves. Can adopt the virtualized approach used by the stream glyph in QNTX.

- **TCS** — Token color scale. Hardcoded brown/amber confidence scale. Will change when sampler chain data is available — different samplers (top-k, top-p, penalties) produce different signal profiles that need distinct visual treatment.

- **TWC** — Token word count. `word_count` is 0 for llama-cpp weaves. The header metadata is misleading. Should derive word count from the token text or from `attributes.text`.

- **TPA** — Token payload in API. Full token arrays are included in every `/api/weaves` response. No lazy loading or pagination — large generation histories will bloat the response.
