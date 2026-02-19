# Cluster Visualization

## Clusters on Canvas

Clusters are visible on the canvas as a live updating map. As new attestations are embedded and assigned, the map reflects the current state of the cluster topology.

Individual clusters can appear as their own [glyph](./glyphs.md), [melded](./glyph-melding.md) with the semantic search glyph. When you select a cluster in ⊨, the cluster glyph shows that cluster's shape and where the semantic matches land within it — so you can see how reliable a match actually is.

## Cluster Labeling

Clusters are labeled automatically using LLM summarization of their contents. Labels can be manually corrected. The label itself is an attestation — queryable, syncable, part of the knowledge graph.

Labels persist across re-clustering. When clusters merge, split, or shift, the system tracks lineage — what a new cluster used to be, what clusters merged into each other.

## Cluster Lifecycle

After re-clustering, the system diffs before/after state and emits events for births, deaths, merges, and splits. These events feed into the watcher system so users can define reactive rules — automatically analyze a new cluster's data, surface next steps, send notifications. Threshold alerts fire when clusters grow or shrink past a configured size. All of this wires through the existing meld system.

Re-clustering runs on a Pulse schedule so cluster lifecycle events happen automatically, not only on manual trigger.

## See Where Data Lands

When semantic search returns results, those points are highlighted on the cluster map — their position relative to the centroid and to each other. You see how central or scattered your matches are.

When a new attestation is embedded and predicted into a cluster, its position flashes briefly on the map.

## Cluster Timeline

The embeddings window shows the evolution of cluster understanding over time. Each clustering run is a point on a timeline. Clusters appear as horizontal bars spanning from birth to death (or still active). Member count and noise are visible per run — you see clusters growing, shrinking, appearing, dissolving.

Labels annotate the timeline when they're assigned. Because labels are attestations, you can see when the LLM's understanding changed — a cluster that was "Rust Build Errors" last week is now "CI Infrastructure" after its membership shifted.

This turns the embeddings window from a snapshot into a story: how the system's understanding of your data evolved. What patterns emerged, which disappeared, what stayed stable.

## Distributed Cluster Consensus

Cluster labels are attestations from `qntx@embeddings`. Through the sync protocol, nodes can share their understanding of what clusters mean. Two nodes with overlapping data might independently discover similar clusters — their labels, synced as attestations, reveal convergence or divergence in understanding.

A node receiving another node's cluster label attestation doesn't overwrite its own — both coexist in the attestation graph. The user sees multiple perspectives: "my node calls this cluster 'Deployment Configs', the lab server calls it 'Infrastructure YAML'." Agreement strengthens confidence; disagreement surfaces ambiguity worth investigating.

This is eventually consistent shared semantics over decentralized data — not a consensus protocol, but attestation propagation applied to the embeddings layer.

## Label Quality

The current labeling pipeline is simple: sample N texts, ask an LLM for a 2-5 word label. Quality can improve by:

- **Evaluation**: compare labels across re-labeling cycles. If a stable cluster gets wildly different labels each time, the prompt or sampling is insufficient.
- **Richer sampling**: instead of random, sample texts near the centroid (typical members) and near the boundary (edge cases). Both inform a better label.
- **User correction**: a manual label override is just another attestation with a different actor. The system can learn from corrections — what did the LLM get wrong, and why?

None of this needs to happen now. The current pipeline produces useful labels. Optimization is worth pursuing once there's enough data to evaluate against.
