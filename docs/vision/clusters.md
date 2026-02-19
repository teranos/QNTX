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
