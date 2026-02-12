# ADR-009: Edge-Based Composition DAG for Multi-Directional Melding

## Status
Phase 3 Complete (Composition extension + cross-axis sub-containers)

## Context

Glyph melding in QNTX creates spatial compositions through proximity. Phase 1 implemented N-glyph compositions using flat arrays (`glyphIds: string[]`) for horizontal chains: `[ax|py|prompt]`.

However, QNTX requires multi-directional melding beyond linear chains:

**Horizontal (right):** Data flow chains
- `ax → py → prompt` (query drives script drives template)
- `py → py` (sequential Python pipelines)

**Vertical (top):** Configuration injection
- `system-prompt ↓ prompt` (system prompt modifies prompt behavior)
- `config ↓ glyph` (runtime configuration)

**Vertical (bottom):** Result attachment
- `chart ↑ ax` (real-time monitoring feeds back into query)
- `output ↑ processor` (downstream results influence upstream behavior)

Flat arrays cannot represent DAG topologies. We need arbitrary graph structures with directional edges.

## Decision

Migrate from flat `glyphIds: string[]` to edge-based composition DAG structure.

**Data model:**
```protobuf
message CompositionEdge {
  string from = 1;        // source glyph ID
  string to = 2;          // target glyph ID
  string direction = 3;   // 'right', 'top', 'bottom'
  int32 position = 4;     // ordering
}

message Composition {
  string id = 1;
  repeated CompositionEdge edges = 2;
  reserved 3;  // formerly glyph_ids (removed for DAG-native approach)
  double x = 4;
  double y = 5;
}
```

**Proto as source of truth:**
- Defined in `glyph/proto/canvas.proto`
- Follows ADR-006 (proto as single source of truth)
- Follows ADR-007 (TypeScript interfaces only)

## Rationale

**Why edges instead of alternatives?**

1. **Nested structures** (`children: Composition[]`): Doesn't support multiple parents (DAG requires this)
2. **Adjacency matrix**: O(N²) space, sparse for typical compositions
3. **Port-based connections**: Over-engineered for current needs, can add later
4. **Edge list**: Flexible, SQL-friendly, supports arbitrary topologies ✓

**Why breaking change instead of migration?**
- No production compositions exist yet (melding just added)
- Clean slate simpler than dual-format compatibility
- Follows Phase 1 precedent

**Why remove `type` field?**
- With arbitrary DAG, computing type becomes ambiguous
- Edges ARE the type information
- Simplifies schema, reduces maintenance

**Why remove `glyph_ids` field?**
- During Phase 1bb, removed derived `glyph_ids` for true DAG-native approach
- Traverse edges to find glyphs (e.g., `composition.edges.some(e => e.from === glyphId)`)
- Proto field 3 reserved to prevent accidental reuse
- Reduces duplication and maintains single source of truth (edges)

## Implementation

**Phase 1ba:** Backend (DB + storage)
- Migration `021_dag_composition_edges.sql`
- Drop `composition_glyphs` junction table
- Create `composition_edges` table with `from`, `to`, `direction`, `position`
- Update Go storage layer to use proto edges

**Phase 1bb:** API + Frontend state ✅
- API handlers accept/return proto edges
- TypeScript state uses proto `Composition` directly
- Remove `type` field from all layers
- Remove `glyph_ids` field - DAG-native edge traversal only
- All 728 tests passing (352 Go + 376 TypeScript)

**Phase 1bc:** UI integration
- Meld system creates edges with `direction: 'right'`
- Reconstruction loads from edges
- Vertical melding support deferred (structure ready)

**Phase 2:** Port-aware meldability + multi-directional melding ✅
- MELDABILITY registry restructured: each glyph class maps to `PortRule[]` with direction + targets
- Proximity detection, performMeld, reconstructMeld all respect edge direction
- Edges created with actual direction (`'right'`, `'bottom'`) not just `'right'`
- Result glyphs auto-meld below py on execution (bottom port)
- Port-based model pulled forward from "future work" — spatial ports are concrete, not abstract

**Phase 3:** Composition extension + cross-axis sub-containers ✅
- Drag-to-extend: standalone glyph melds into existing composition (append to leaf / prepend to root)
- Cross-axis sub-containers: nested `meld-sub-container` flex divs for mixed-direction edges
- Meld system split into focused modules: `meld-detect.ts`, `meld-feedback.ts`, `meld-composition.ts` with barrel re-export
- All guards use `.closest('.melded-composition')` to traverse through sub-containers
- 3+ glyph chains in browser, composition persistence across page reload

## Consequences

### Positive
- Supports arbitrary DAG topologies for future features
- Single source of truth via proto (cross-language safety)
- Database schema matches graph semantics (edges table)
- Follows established patterns (ADR-006, ADR-007)
- No technical debt from temporary solutions
- **DAG-native thinking:** No derived fields, traverse edges directly
- Proto field reservation prevents future mistakes

### Negative
- Breaking change requires database recreation
- More complex than flat arrays for simple chains
- Edge traversal requires graph algorithms (topological sort for ordering)

### Neutral
- Proto generation adds build step (already exists)
- Go uses proto at boundaries, internal structs for DB (ADR-006 pattern)

## Alternatives Considered

**Keep flat arrays, add separate structure for complex cases:**
- Creates dual model complexity
- Still need migration path eventually
- Defers inevitable refactoring

**Port-based model (à la GoFlow):**
- Initially deferred as over-engineered
- Pulled forward in Phase 2: spatial ports (right/bottom/top) per glyph class
- Lightweight implementation — ports are directional rules in a registry, not abstract objects

**Graph library (Graphology, dominikbraun/graph):**
- Runtime overhead for storage/retrieval
- Database still needs edge table
- Can use for client-side traversal algorithms

## Future Work

- Cycle detection validation (DAG invariant enforcement)
- Topological sort for execution order
- Graph visualization/debugging tools
- Unmeld granularity: splitting a composition at a middle glyph

## References

- [GoFlow](https://github.com/trustmaster/goflow) - FBP approach for Go
- [Graphology](https://graphology.github.io/) - JS graph library
- Protocol Labs: [Designing a Dataflow Editor](https://research.protocol.ai/blog/2021/designing-a-dataflow-editor-with-typescript-and-react/)
- Issue #441: Multi-glyph melding UI
- `docs/development/multi-glyph-meld.md`: Implementation checklist
