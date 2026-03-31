# Handover: HYP — Poincaré Ball Nebula

Branch: `claude/review-weekend-work-iCiWi`

## What this branch adds

Poincaré ball (hyperbolic) embedding as a second visualization mode for the nebula, switchable at runtime alongside PCA.

Hyperbolic space is the natural geometry for BPE token hierarchies: center = common tokens, boundary (at infinity) = rare/specific tokens, angular position = semantic domain. Live generation on the Poincaré ball sends probability mass toward the periphery during uncertainty, revealing endlessly branching structure.

## Files changed

- `vocab_projection.cpp` — Poincaré embedding: k-NN graph (batched sgemm), Riemannian SGD (200 epochs), cache as `.vocab3d.hyp`
- `vocab_projection_stub.cpp` — non-Apple stubs for all new methods
- `plugin.h` — `ProjectionMode` enum, `vocab_positions_hyp_`, new methods on `InferenceEngine`
- `plugin.cpp` — `POST /projection` endpoint, status includes `projection` field, background HYP precompute after PCA
- `nebula-module.js` — projection toggle button in controls panel, 'p' key shortcut
- `README.md` — HYP limitation entry, updated SIG (resolved) and PAT (resolved) entries

## Missing steps

1. **Not compiled.** This was written on a Linux environment without llama.cpp headers, Metal, or Accelerate BLAS. It has never been compiled. `make llama-cpp-plugin` is the first gate.

2. **`poincare_grad` uses `cblas_sdot` on 3-element vectors.** This works but may be slower than a manual dot product due to BLAS call overhead on tiny vectors. If it causes issues, inline it.

3. **The RSGD loop modifies positions that other threads may read.** `vocab_positions_hyp_` is written by the background thread during optimization and could be read by the HTTP `/projection` endpoint simultaneously. The PCA path has the same pattern (write once, then read-only), but HYP writes continuously during its 200 epochs. May need an atomic flag or mutex guard around the swap.

4. **Memory during k-NN construction.** `sim_block` is `batch_size × n_vocab` floats = 512 × 128K × 4 = 256MB. Plus the full embedding matrix `X` at `128K × 4096 × 4 = 2GB`. This runs in the background thread. Verify it doesn't OOM on machines with 16GB.

5. **No progress reporting during HYP computation.** PCA has status polling (`computing_positions`). HYP computation happens after `pca_ready_` is already set to true, so the UI shows "ready" while HYP is still running. Need a `hyp_ready_` flag or activity status.

## Verification steps

1. `make llama-cpp-plugin` — does it compile?
2. `make dev` — load a model, does PCA still work as before?
3. Wait for HYP computation to finish (watch stdout for epoch logs). How long does it take?
4. Right-click nebula → projection toggle → click or press 'p'. Does the nebula change shape?
5. Run a generation in HYP mode. Do particles move? Does the trail render?
6. Reload page. Does HYP load from `.vocab3d.hyp` cache instantly?
7. Delete the `.vocab3d.hyp` cache file. Does it recompute?

## Difficulties

- **The math is unverified.** The Poincaré distance, gradient, and Riemannian scaling formulas were implemented from research papers without a reference implementation to compare against. If the RSGD diverges or produces garbage, the math is the first place to look.

- **Hyperparameters are guesses.** `HYP_K=15`, `HYP_EPOCHS=200`, `HYP_LR=0.01`, `HYP_BURNIN_LR=0.001`, `HYP_INIT_SCALE=0.5` — these were chosen from the Nickel & Kiela 2017 paper defaults. They may not work well for a 128K token vocabulary. The loss logged every 50 epochs should show whether convergence is happening.

- **Negative sampling rejection loop.** Line 464: `if (neg == u || neg == v) { s--; continue; }` retries if the random sample collides with u or v. With 128K tokens this is extremely unlikely per sample, but the loop has no upper bound. Not a real risk, but technically unbounded.

## Limitations

- **No visual distinction at the boundary.** The Poincaré ball's boundary should feel infinite — tokens near the edge represent vast hyperbolic distances. But the Metal renderer treats all positions as Euclidean 3D. Tokens near the boundary will appear bunched together at the surface of a sphere rather than spread across infinite space. A proper visualization would use a hyperbolic-to-screen projection where the boundary has visual depth (like Escher's Circle Limit). This requires shader changes.

- **No hierarchy signal.** The embedding is purely from cosine similarity of the weight matrix. There's no explicit frequency or hierarchy information — common vs. rare token placement emerges only if the embeddings happen to encode it. Using `llama_token_get_score()` to inject frequency-based radial bias could strengthen the center-common / edge-rare structure.

- **Orbit/drift parameters tuned for PCA.** The orbit period, radius multiplier, and drift step in the renderer were tuned for PCA's coordinate scale. HYP positions are inside the unit ball (||x|| < 1), while PCA positions can have large magnitudes. The renderer's auto-fit (`set_vocab_positions` computes bounding box) should adapt, but orbit and drift may look wrong.

## Open questions

1. **Does it look different from PCA?** If the RSGD converges, the Poincaré layout should cluster semantically related tokens and push rare tokens outward. But if the original embeddings don't have strong hierarchical structure, HYP may look like a compressed version of PCA with everything crammed into a unit ball.

2. **How long does first-run computation take?** The k-NN graph is O(n_vocab² × n_embd) via batched sgemm. For 128K × 4096, estimated 25-60 seconds on M1 via Accelerate. The RSGD is O(epochs × n_vocab × k × neg_samples) = 200 × 128K × 15 × 10 ≈ 38B scalar ops, estimated 5-15 seconds. Total first run: maybe 30-90 seconds. Cached after that.

3. **Should switching modes reset the camera?** Currently `clear_trail()` is called but the camera position is preserved. The bounding box changes (PCA: arbitrary scale, HYP: unit ball), so the camera framing may be wrong after switching.

4. **Is the exponential map initialization good enough?** The PCA→Poincaré mapping via `tanh(||v||·scale/2)·v/||v||` preserves angular structure but the radial mapping is arbitrary. If the RSGD has trouble converging, a better initialization (e.g., frequency-weighted radial placement) might help.

5. **Triangle mesh from the conversation.** Earlier in this session we discussed rendering the trail as a filled triangle strip with per-triangle subdivision and pinched seams between segments. That idea applies to both PCA and HYP but hasn't been implemented. The rotation per token guarantees non-degenerate triangles in both spaces.
