# SE → SE Meldability: Semantic Query Composition

## SE → SE (right): Intersection

When SE₁ melds rightward to SE₂, the downstream glyph narrows the upstream search space. SE₁ ("science") defines a broad semantic region; SE₂ ("about teaching") intersects it. Only attestations matching **both** queries appear in SE₂. SE₁ continues to show its own unfiltered results independently.

Implementation: compound watcher with `UpstreamSemanticQuery`. Both similarity thresholds must pass. Historical queries search by upstream (broader), post-filter by downstream. The downstream similarity score is reported to the user. Engine-level suppression in `loadWatchers` removes the downstream SE's standalone watcher from the in-memory map while a compound watcher targets it — the compound watcher is the sole result source.

## SE → SE → SE chaining (future, #535)

Current implementation supports pairwise intersection: SE₁→SE₂ checks both queries. For chains of 3+ (SE₁→SE₂→SE₃), each edge creates an independent compound watcher — SE₂→SE₃ uses SE₂'s standalone query as upstream, not the SE₁∩SE₂ intersection. True transitive intersection (SE₁∩SE₂∩SE₃) requires propagating the full ancestor chain. This is because compound watchers store only two queries (upstream + downstream), not the full ancestor chain.

Path forward: replace the single `UpstreamSemanticQuery` field with a JSON array of `[{query, threshold}]` ancestors. The engine searches by the broadest (first ancestor), then post-filters through each subsequent query. `compileSubscriptions` walks the meld graph to collect the full chain. Schema migration + engine loop extension — the core search-then-filter pattern already works for 2 queries and generalizes to N.

## SE ↓ SE (bottom): Union (future, #536)

Vertical SE composition would merge disjoint semantic regions — "machine learning" ↓ "gardening" shows attestations matching either. This is the dual of intersection: spatial union rather than refinement.

Not yet implemented. When it is, bottom-direction SE edges will create watchers that fire when **either** query passes its threshold, deduplicating by attestation ID.
