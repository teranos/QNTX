# ADR-009: Edge-Based Composition DAG for Multi-Directional Melding

## Status
In Progress (Phase 1b)

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
  repeated string glyph_ids = 3;  // computed
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

## Implementation

**Phase 1ba:** Backend (DB + storage)
- Migration `021_dag_composition_edges.sql`
- Drop `composition_glyphs` junction table
- Create `composition_edges` table with `from`, `to`, `direction`, `position`
- Update Go storage layer to use proto edges

**Phase 1bb:** API + Frontend state
- API handlers accept/return proto edges
- TypeScript state uses proto `CompositionEdge` interface
- Remove `type` field from all layers

**Phase 1bc:** UI integration
- Meld system creates edges with `direction: 'right'`
- Reconstruction loads from edges
- Vertical melding support deferred (structure ready)

**Phase 2-5:** Horizontal chain UI
- Only create `direction: 'right'` edges
- Multi-glyph chains work via composition-to-glyph melding
- Vertical melding (top/bottom) implemented later

## Consequences

### Positive
- Supports arbitrary DAG topologies for future features
- Single source of truth via proto (cross-language safety)
- Database schema matches graph semantics (edges table)
- Follows established patterns (ADR-006, ADR-007)
- No technical debt from temporary solutions

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
- Over-engineered for current needs
- Glyphs don't have explicit ports (yet)
- Can add port abstraction layer later if needed

**Graph library (Graphology, dominikbraun/graph):**
- Runtime overhead for storage/retrieval
- Database still needs edge table
- Can use for client-side traversal algorithms

## Future Work

- Vertical melding UI (top/bottom directions)
- Cycle detection validation
- Topological sort for execution order
- Graph visualization/debugging tools
- Port-based connections (if semantic clarity needed)

## References

- [GoFlow](https://github.com/trustmaster/goflow) - FBP approach for Go
- [Graphology](https://graphology.github.io/) - JS graph library
- Protocol Labs: [Designing a Dataflow Editor](https://research.protocol.ai/blog/2021/designing-a-dataflow-editor-with-typescript-and-react/)
- Issue #441: Multi-glyph melding UI
- `docs/development/multi-glyph-meld.md`: Implementation checklist
