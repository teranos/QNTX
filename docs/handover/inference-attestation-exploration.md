# Inference Attestation — Exploration Handover

**Branch:** `claude/review-weekend-work-lqvxU`
**Date:** 2026-03-24
**Status:** Exploration complete. Not production code — this is a proof-of-concept in D to validate which signals matter before building the real thing in C++.

## What was the idea

When QNTX runs local inference through llama.cpp, we have access to something cloud APIs never give us: the full probability distribution over the vocabulary at every token position. This is raw attestation material — it tells us *how certain the model was* at each decision point.

The goal is to capture per-token probabilities during inference and compute signals (entropy, confidence, top-gap) that become first-class attestations in the QNTX attestation store. These signals answer: "where in this generation was the model unsure?" and "how decisive was each token choice?"

## Why D first, C++ ultimately

D was chosen for rapid prototyping because it has:
- Zero-dependency protobuf/gRPC: compile-time introspection (`@Proto` UDAs + CTFE) generates codecs from struct layout alone — no protoc, no schema files, no code generation step
- Fast iteration: `dub test` runs the full suite in seconds
- Close enough to systems-level that the architecture transfers directly to C++

**The real implementation belongs in C++ as a custom `llama_sampler`.** The D plugin sits *outside* llama.cpp and proxies through llama-server's HTTP API. A C++ sampler sits *inside* the inference loop — it sees every token probability distribution at sampling time with zero overhead, no HTTP serialization, no probability data marshaling. The D exploration validates *which signals are worth computing*; the C++ sampler will compute them at the right layer.

## Architecture as built (D prototype)

```
QNTX prompt
  → gRPC /protocol.LLMService/Chat
  → infer plugin (D)
  → HTTP POST /completion to llama-server (n_probs=20, post_sampling_probs=true)
  → parse per-token probability arrays from response JSON
  → compute signals (entropy, confidence, top-gap, spikes, low-conf spans)
  → write attestation to ATS via gRPC
  → return generation to QNTX
```

The plugin is a standalone gRPC server that registers as an LLM provider. QNTX sends chat requests to it; it forwards them to a running llama-server instance, captures the probability data that llama-server returns, computes signal metrics, writes an attestation, and returns the generated text.

## Signals computed

Three per-token metrics, then aggregate detection:

| Signal | Definition | What it tells you |
|--------|-----------|-------------------|
| **Entropy** (bits) | Shannon entropy H = -sum(p_i * log2(p_i)) over top-N probs | How spread the probability mass is — high entropy = many plausible continuations |
| **Confidence** | Probability of the selected (top-1) token | How committed the model was to its choice |
| **Top-gap** | p_top1 - p_top2 | How decisive the selection was vs the runner-up |

Aggregate detections:
- **Entropy spikes**: token positions where entropy > (mean + 1 stddev) — adaptive threshold per generation
- **Low-confidence spans**: contiguous ranges where confidence < 0.3 (configurable threshold)

These get serialized as attestation attributes: `mean_entropy`, `max_entropy`, `min_entropy`, `mean_confidence`, `min_confidence`, `entropy_spike_count`, `entropy_spike_positions`, `low_conf_span_count`, `low_conf_spans`.

## What the tests cover

All D tests pass (`make test-d` — 8 modules across infer + ix-net):

- **proto.d**: varint codec, string/bytes/nested message round-trips, map encoding, LLM proto encode/decode including `llmProvider` field
- **grpc.d**: gRPC frame/unframe, HTTP/2 SETTINGS frame encode
- **hpack.d**: HPACK integer codec, string encode/decode, Huffman trie
- **http.d**: URL parsing, JSON request body building, chunked HTTP decode, completion response parsing (both probability formats)
- **signals.d**: entropy/confidence computation with three test distributions — confident tokens (low entropy, high confidence), uncertain tokens (high entropy, low confidence, spikes detected), mixed sequences (span detection)

## File inventory

| File | Purpose |
|------|---------|
| `qntx-plugins/infer/source/infer/plugin.d` | Plugin orchestration — handles Chat RPC, calls llama-server, computes signals, writes attestation |
| `qntx-plugins/infer/source/infer/signals.d` | Signal computation — 3-pass algorithm over probability distributions |
| `qntx-plugins/infer/source/infer/http.d` | HTTP client for llama-server /completion endpoint, response parsing |
| `qntx-plugins/infer/source/infer/proto.d` | Zero-dependency protobuf codec via compile-time struct introspection |
| `qntx-plugins/infer/source/infer/grpc.d` | Pure D HTTP/2 gRPC server + client |
| `qntx-plugins/infer/source/infer/hpack.d` | HPACK header compression (HTTP/2 requirement) |
| `qntx-plugins/infer/source/infer/ats.d` | Attestation store client (gRPC wrapper) |
| `qntx-plugins/infer/source/infer/app.d` | Entry point / main |
| `qntx-plugins/infer/source/infer/log.d` | Logging |
| `qntx-plugins/infer/source/infer/version_.d` | Plugin version |
| `web/ts/llm-provider-glyph.ts` | UI toggle — checkbox under llama.cpp to route through infer plugin |

## What transfers to C++

The signal computation algorithm (signals.d) transfers directly — it's a 3-pass loop over an array of probability distributions with no D-specific abstractions. The per-token struct, the entropy/confidence/top-gap formulas, the adaptive spike threshold, and the span detection logic are all portable.

Everything else (gRPC server, protobuf codec, HTTP client, plugin lifecycle) gets *replaced* — a `llama_sampler` in C++ lives inside llama.cpp's sampling pipeline and sees probabilities natively. No network, no serialization, no proxy. The sampler callback receives the logit array directly; you softmax it, compute the same signals, and write the attestation.

## Key numbers

- `n_probs = 20` — top-20 probabilities captured per token (configurable)
- `confidence_threshold = 0.3` — below this, a token is "low confidence" (configurable)
- Spike threshold = mean entropy + 1 standard deviation (adaptive, not configurable)
- Socket timeouts: 120s receive, 10s send (llama-server can be slow on large generations)
