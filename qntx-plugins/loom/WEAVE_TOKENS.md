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
