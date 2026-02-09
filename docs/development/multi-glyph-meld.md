# Multi-Glyph Chain Melding Implementation

**Issue:** [#411](https://github.com/teranos/QNTX/issues/411)
**Goal:** Support linear chains of 3+ glyphs: `[ax|py|prompt]`

## Current State

**Phase 1 Complete:** Storage + DAG migration (PRs #436, #443)
- ✅ Edge-based DAG structure (`composition_edges` table, proto types)
- ✅ Frontend uses proto `Composition` directly, no derived fields

**Phase 2 Complete:** Port-aware meldability + multi-directional melding
- ✅ MELDABILITY registry encodes spatial ports (right/bottom/top per glyph class)
- ✅ Multi-directional proximity detection and layout
- ✅ Result glyphs auto-meld below py on execution
- ✅ py → py chaining enabled

**Phase 3 Complete:** Composition extension + cross-axis sub-containers
- ✅ Drag-to-extend: standalone glyph melds into existing composition (append/prepend)
- ✅ 3+ glyph chains in browser (ax|py|prompt, etc.)
- ✅ Auto-meld result when py is already inside a composition
- ✅ Cross-axis sub-containers (result below py in horizontal composition)
- ✅ Meld system split into focused modules (detect/feedback/composition)

## Architecture Decision

**Edge-based DAG structure** (DAG-native, no derived fields)

Initial Phase 1 implemented `glyphIds: string[]` for linear chains. However, QNTX requires multi-directional melding:
- **Horizontal (right):** data flow chains (ax → py → prompt)
- **Vertical (top):** configuration injection (system-prompt ↓ prompt)
- **Vertical (bottom):** result attachment (chart ↑ ax for real-time monitoring)

Flat arrays cannot represent DAG structures. Phase 1b migrates to edge-based composition.

**Final structure (DAG-native):**

```typescript
interface CompositionEdge {
  from: string;           // source glyph ID
  to: string;             // target glyph ID
  direction: 'right' | 'top' | 'bottom';
  position: number;       // ordering for multiple edges same direction
}

interface CompositionState {
  id: string;
  edges: CompositionEdge[];
  x: number;
  y: number;
}
```

**Key principle:** No `glyph_ids` field. Traverse edges to find glyphs (DAG-native thinking).

**Rationale:**
- Supports arbitrary DAG topologies
- **DAG-native:** No derived `glyph_ids` field - traverse edges to find glyphs
- Phase 2 creates both `'right'` and `'bottom'` edges (multi-directional)
- Structure ready for `'top'` direction (reserved)
- Proto field 3 reserved (formerly `glyph_ids`) to prevent accidental reuse
- Follows patterns from flow-based programming (GoFlow, dataflow editors)

## Implementation Checklist

### Phase 1: State & Storage ✅ **COMPLETE**

**Decision:** Clean breaking change (no backward compatibility). No melded compositions exist in production yet.

#### Frontend State ✅
- ✅ Update `CompositionState` type in `web/ts/state/ui.ts`
  - ✅ Change `initiatorId: string` + `targetId: string` to `glyphIds: string[]`
  - ✅ Add new composition types: `'ax-py-prompt'`
  - ✅ Updated all composition helper functions (`isGlyphInComposition`, `findCompositionByGlyph`)
  - ✅ Updated unmeld return type to `{ glyphElements, glyphIds }`

#### Backend Storage ✅
- ✅ Create migration `020_multi_glyph_compositions.sql`
  - ✅ Create junction table `composition_glyphs(composition_id, glyph_id, position)`
  - ✅ Recreate `canvas_compositions` without `initiator_id`/`target_id` (breaking change)
  - ✅ Foreign key constraints: composition → glyphs cascade delete
  - ⚠️ No data migration (breaking change: existing compositions dropped)

- ✅ Update `glyph/storage/canvas_store.go`
  - ✅ `GetComposition()` returns `GlyphIDs []string` (queries junction table)
  - ✅ `UpsertComposition()` accepts glyph ID array (transaction-based with junction table)
  - ✅ `ListCompositions()` fixed nested query issue (two-pass approach)
  - ✅ Orphan validation: compositions with zero glyphs return error

- ✅ Update `glyph/handlers/canvas.go`
  - ✅ API payload accepts `glyph_ids: string[]`
  - ✅ Returns composition with full glyph array

#### Storage Tests ✅
- ✅ Update `glyph/storage/canvas_store_test.go`
  - ✅ All tests updated for `GlyphIDs` array format
  - ✅ Test cascade delete behavior (orphaning when all glyphs deleted)
  - ✅ Test composition upsert and retrieval with N glyphs
  - ✅ Fixed `ForeignKeyConstraints` test for new junction table behavior

#### Frontend Tests ✅
- ✅ Update `web/ts/state/compositions.test.ts` for `glyphIds` array
- ✅ Update `web/ts/components/glyph/meld-composition.test.ts` for new unmeld return format
- ✅ Add skipped TDD tests in `web/ts/state/compositions.test.ts`:
  - ✅ 3-glyph composition stores correctly
  - ✅ `isGlyphInComposition` works with 3-glyph chains
  - ✅ `findCompositionByGlyph` finds 3-glyph chains
  - ✅ Extending composition adds glyph to array
- ✅ Add skipped TDD tests in `web/ts/components/glyph/meld-composition.test.ts`:
  - ✅ Tim creates 3-glyph chain (ax|py|prompt) by dragging onto composition
  - ✅ Tim sees proximity feedback when dragging glyph toward composition
  - ✅ Tim extends ax|py composition by dragging prompt onto it
  - ✅ Tim extends 3-glyph chain into 4-glyph chain
- ✅ All active tests passing: 352 pass, 0 fail (8 skipped)

### Phase 1b Prerequisites: Proto Definitions ✅ **COMPLETE**

**Goal:** Define composition DAG structure in protobuf as single source of truth (ADR-006).

**Approach:** Follow ADR-007 pattern (TypeScript interfaces only), ADR-006 pattern (Go manual conversion at boundaries).

#### Create Proto Definition

- ✅ Create `glyph/proto/canvas.proto`:
  ```protobuf
  syntax = "proto3";
  package proto;

  option go_package = "github.com/teranos/QNTX/glyph/proto";

  // CompositionEdge represents a directed edge in the composition DAG
  message CompositionEdge {
    string from = 1;           // source glyph ID
    string to = 2;             // target glyph ID
    string direction = 3;      // 'right', 'top', 'bottom'
    int32 position = 4;        // ordering for multiple edges same direction
  }

  // Composition represents a DAG of melded glyphs
  message Composition {
    string id = 1;
    repeated CompositionEdge edges = 2;
    repeated string glyph_ids = 3;  // computed field: all unique glyph IDs
    double x = 4;                    // anchor X position in pixels
    double y = 5;                    // anchor Y position in pixels
  }
  ```

#### Update Proto Generation

- ✅ Update `proto.nix`:
  - ✅ Add `canvas.proto` to Go generation (generate-proto-go):
    ```nix
    ${pkgs.protobuf}/bin/protoc \
      --plugin=${pkgs.protoc-gen-go}/bin/protoc-gen-go \
      --go_out=. --go_opt=paths=source_relative \
      glyph/proto/canvas.proto
    ```
  - ✅ Add `canvas.proto` to TypeScript generation (generate-proto-typescript):
    ```nix
    ${pkgs.protobuf}/bin/protoc \
      --plugin=protoc-gen-ts_proto=web/node_modules/.bin/protoc-gen-ts_proto \
      --ts_proto_opt=esModuleInterop=true \
      --ts_proto_opt=outputEncodeMethods=false \
      --ts_proto_opt=outputJsonMethods=false \
      --ts_proto_opt=outputClientImpl=false \
      --ts_proto_opt=outputServices=false \
      --ts_proto_opt=onlyTypes=true \
      --ts_proto_opt=snakeToCamel=false \
      --ts_proto_out=web/ts/generated/proto \
      glyph/proto/canvas.proto
    ```

- ✅ Run `make proto` to generate code:
  - ✅ Verify Go types generated in `glyph/proto/canvas.pb.go`
  - ✅ Verify TypeScript interfaces generated in `web/ts/generated/proto/glyph/proto/canvas.ts`

- ✅ Commit proto definition and generated code

### Phase 1ba: Backend DAG Migration ✅ **COMPLETE**

**Goal:** Migrate database and Go storage layer from `composition_glyphs` junction table to edge-based DAG structure.

**Strategy:** Breaking change (drop existing compositions, like Phase 1)

#### Database Schema

- ✅ Create migration `021_dag_composition_edges.sql`
  - ✅ Create `composition_edges` table with foreign keys to compositions and glyphs
  - ✅ Drop `composition_glyphs` table (breaking change)
  - ✅ Recreate `canvas_compositions` without `type` column

#### Storage Layer

- ✅ Update `glyph/storage/canvas_store.go`
  - ✅ Import proto types: `import pb "github.com/teranos/QNTX/glyph/proto"`
  - ✅ Add internal `compositionEdge` struct for database operations
  - ✅ Update `CanvasComposition` struct with `Edges []*pb.CompositionEdge`
  - ✅ Add conversion helpers: `toProtoEdge()` and `fromProtoEdge()`
  - ✅ Update `UpsertComposition()` to write edges in transaction
  - ✅ Update `GetComposition()` to load edges via JOIN
  - ✅ Update `ListCompositions()` to load edges (two-pass approach)
  - ✅ `DeleteComposition()` cascade handled by foreign key constraints

#### Backend Tests

- ✅ Update `glyph/storage/canvas_store_test.go`
  - ✅ Replace all `GlyphIDs` with `Edges` in test data
  - ✅ All tests updated to use edge structure
  - ✅ Remove all references to `Type` field
  - ✅ Run `make test` - all Go tests passing (356 pass, 24 skip, 0 fail)

- ✅ Update `glyph/handlers/canvas_test.go`
  - ✅ All handler tests updated to use edges
  - ✅ Handler implementation requires no changes (generic JSON passthrough)

### Phase 1bb: API & Frontend State Migration ✅ **COMPLETE**

**Goal:** Update API handlers and TypeScript state management for edge-based compositions.

**Completion:** All tests passing (728 total: 352 Go + 376 TypeScript)

#### Backend API Handlers

- ✅ `glyph/handlers/canvas.go` requires no changes (generic JSON passthrough)
- ✅ Handler automatically serializes storage layer's edge-based `CanvasComposition` struct
- ⏸️ **Deferred:** DAG validation (cycle detection, invalid direction) - add when needed

#### Proto Definition (DAG-Native)

- ✅ **Removed `glyph_ids` field from proto** - edges ARE the composition
- ✅ Proto uses `reserved 3` to prevent field reuse
- ✅ `Composition` message contains only: `id`, `edges`, `x`, `y`

#### Frontend State

- ✅ Update `web/ts/state/ui.ts`
  - ✅ Import proto types: `import type { CompositionEdge, Composition } from '../generated/proto/glyph/proto/canvas'`
  - ✅ Use proto `Composition` directly: `export type CompositionState = Composition`
  - ✅ Remove `type` field

- ✅ Update `web/ts/state/compositions.ts` (DAG-native)
  - ✅ Add helper: `buildEdgesFromChain(glyphIds: string[], direction): CompositionEdge[]` (for tests/migrations)
  - ✅ Add helper: `extractGlyphIds(edges: CompositionEdge[]): string[]` (for logging)
  - ✅ Remove `getCompositionType()` and `getMultiGlyphCompositionType()`
  - ✅ Update `isGlyphInComposition()` - traverses edges (DAG-native)
  - ✅ Update `findCompositionByGlyph()` - traverses edges (DAG-native)

- ✅ Update `web/ts/api/canvas.ts`
  - ✅ Update `upsertComposition()` to send edges only
  - ✅ Update response parsing to expect edges only
  - ✅ Remove `glyph_ids` and `type` fields from API calls

- ✅ Update `web/ts/components/glyph/meld-system.ts` (DAG-native)
  - ✅ Create edges directly in `performMeld()` (not array-then-convert)
  - ✅ Binary meld creates one edge: `{ from: initiator, to: target, direction: 'right', position: 0 }`
  - ✅ Remove `glyphIds` from `unmeldComposition()` return value

#### Frontend Tests

- ✅ Update `web/ts/state/compositions.test.ts`
  - ✅ Replace all `glyphIds` with `edges` in test data (DAG-native)
  - ✅ Remove all `type` field references
  - ✅ Remove `getCompositionType()` tests (function removed)
  - ✅ Update tests to create edges directly (not via helper)
  - ✅ 2-glyph tests: `edges: [{ from: 'ax1', to: 'prompt1', direction: 'right', position: 0 }]`
  - ✅ 3-glyph tests: Two edges with position 0 and 1
  - ✅ Run `bun test` - all TypeScript tests passing (376 tests)

- ✅ Update `web/ts/components/glyph/meld-composition.test.ts`
  - ✅ Remove `glyphIds` expectation from `unmeldComposition()` return value

### Phase 1bc: Frontend UI Integration ✅ **COMPLETE**

**Goal:** Update meld system and UI to work with edge-based compositions.

**Completion:** All tests passing (728 total: 352 Go + 376 TypeScript)

#### Meld System

- ✅ Update `web/ts/components/glyph/meld-system.ts`
  - ✅ `performMeld()` already creates edges directly (done in Phase 1bb)
  - ✅ `unmeldComposition()` already doesn't return glyphIds (done in Phase 1bb)
  - ✅ `reconstructMeld()` signature simplified:
    - ✅ Removed `compositionType` parameter (no longer needed)
    - ✅ Takes glyphElements directly, DOM restoration doesn't need edges

- ✅ Update `web/ts/components/glyph/canvas-glyph.ts`
  - ✅ Import `extractGlyphIds` from compositions module
  - ✅ Use `extractGlyphIds(comp.edges)` to find glyph IDs from edges
  - ✅ Updated migration guard to check for `edges` instead of `glyphIds`
  - ✅ Removed `comp.type` from reconstructMeld call
  - ✅ Updated log statements to use edge/glyph counts instead of type

#### Integration Testing

- ✅ Run full test suite: `make test`
  - ✅ All Go tests pass (352 tests)
  - ✅ All TypeScript tests pass (376 tests)
  - ✅ No regressions

- ✅ Manual browser test: Create 2-glyph composition
  - ✅ Drag ax near prompt → meld
  - ✅ Verify composition saved with edges to backend
  - ✅ Refresh page → verify composition restored correctly
  - ✅ Unmeld → verify both glyphs restore independently
  - ✅ Note: Rectangle selection feature added to enable selecting glyphs within compositions

### Phase 2: Port-Aware Meldability ✅ **COMPLETE**

**Goal:** Spatial ports on glyphs defining valid directional connections, multi-directional proximity detection and layout.

#### Port-aware registry ✅
- ✅ `meldability.ts`: Restructured from flat `class → class[]` to `class → PortRule[]`
  - `EdgeDirection = 'right' | 'bottom' | 'top'`
  - `PortRule = { direction, targets[] }`
  - ax: right → prompt, py
  - py: right → prompt, py; bottom → result
  - prompt: bottom → result
  - note: bottom → prompt (note sits above prompt)
- ✅ `areClassesCompatible()` returns `EdgeDirection | null`
- ✅ `getLeafGlyphIds()` / `getRootGlyphIds()` for DAG sink/source detection
- ✅ `getMeldOptions()` for append (leaf) and prepend (root) with `incomingRole: 'from' | 'to'`
- ✅ `getGlyphClass()` regex-based, decoupled from registry

#### Multi-directional meld system ✅
- ✅ `meld-system.ts`: `checkDirectionalProximity()` handles right/bottom/top with alignment
- ✅ `findMeldTarget()` returns `{ target, distance, direction }`
- ✅ `performMeld()` accepts direction, switches flex layout (row vs column)
- ✅ `reconstructMeld()` accepts edges, determines layout from edge directions

#### Auto-meld result below py ✅
- ✅ `py-glyph.ts`: `createAndDisplayResultGlyph` calls `performMeld('bottom')`
- ✅ Composition made draggable immediately after auto-meld
- ✅ When py is already in a composition, result extends composition (Phase 3)

#### Call site updates ✅
- ✅ `glyph-interaction.ts`: passes direction to `performMeld`
- ✅ `canvas-glyph.ts`: passes edges to `reconstructMeld`
- ✅ `compositions.ts`: removed `isCompositionMeldable` placeholder

#### Tests ✅
- ✅ `meldability.test.ts` (new): port-aware registry, DAG helpers, getMeldOptions
- ✅ `meld-composition.test.ts`: directional melding, direction-aware reconstruction
- ✅ `meld-detect.test.ts`: bidirectional detection, composition target finding
- ✅ All 397 tests passing, 0 fail

#### Manual Testing ✅
- ✅ Horizontal meld (ax+prompt, ax+py) works as before
- ✅ Bottom meld: py execution auto-melds result below
- ✅ Composition draggable after auto-meld
- ✅ Refresh reconstructs compositions with correct layout direction

### Phase 3: Composition Extension ✅ **COMPLETE**

**Goal:** Meld a standalone glyph into an existing composition (edges-only: append to leaf / prepend to root). Also fix auto-meld when py is already inside a composition.

**Composition ID strategy:** Regenerate ID on extend (`melded-{from}-{to}` with the new edge's endpoints).

#### Module split ✅
- ✅ Split `meld-system.ts` into focused modules:
  - `meld-detect.ts` — proximity detection, target finding (`findMeldTarget`, `canInitiateMeld`, `canReceiveMeld`)
  - `meld-feedback.ts` — visual proximity cues (`applyMeldFeedback`, `clearMeldFeedback`)
  - `meld-composition.ts` — composition CRUD (`performMeld`, `extendComposition`, `reconstructMeld`, `unmeldComposition`)
  - `meld-system.ts` — barrel re-export (zero import churn for callers)
- ✅ Split tests into `meld-detect.test.ts` and `meld-composition.test.ts`

#### Drag-to-extend ✅
- ✅ `findMeldTarget()` recognizes glyphs inside compositions as meld targets
  - ✅ Relaxed guards: only skip elements in the SAME composition
  - ✅ Uses `.closest('.melded-composition')` for sub-container awareness
  - ✅ Both forward and reverse detection work with composition targets
- ✅ `extendComposition()` in `meld-composition.ts`
  - ✅ Same-axis: append/prepend to composition container
  - ✅ Cross-axis: creates `meld-sub-container` (nested flex) to preserve spatial layout
  - ✅ Reuses existing sub-container for repeat extensions (e.g., second py execution)
  - ✅ Regenerates composition ID, updates storage
- ✅ `glyph-interaction.ts`: detects composition targets via `.closest()`, calls `extendComposition`

#### Cross-axis sub-containers ✅
- ✅ DOM structure for mixed-direction compositions:
  ```
  .melded-composition (flex-direction: row)
    ├── ax-glyph
    └── .meld-sub-container (flex-direction: column)
        ├── py-glyph
        └── result-glyph
  ```
- ✅ `reconstructMeld()` rebuilds sub-containers from stored mixed-direction edges on page reload
- ✅ All guards use `.closest('.melded-composition')` instead of `parentElement?.classList.contains()`

#### Auto-meld-into-composition ✅
- ✅ `py-glyph.ts`: uses `.closest('.melded-composition')` to find parent composition
  - ✅ Calls `extendComposition` with direction `'bottom'` and role `'to'`
  - ✅ Works when py is direct child or inside sub-container

#### Tests ✅
- ✅ `meld-detect.test.ts`: reverse meld detection, composition target detection
- ✅ `meld-composition.test.ts`: meld/unmeld, directional layout, reconstruction, extension (same-axis + cross-axis + repeat execution), storage verification
- ✅ All 397 tests passing, 0 fail

#### Manual Testing ✅
- ✅ Drag prompt near ax|py composition → extends to ax|py|prompt
- ✅ Refresh → 3-glyph composition persists
- ✅ Unmeld → all 3 glyphs separate
- ✅ Run py in ax|py composition → result appears below py (cross-axis sub-container)
- ✅ Drag note above prompt inside composition → extends composition

### Phase 4: Backend Tests

**Backend tests deferred from Phase 3:**
- [ ] Unskip `glyph/handlers/canvas_test.go`:
  - [ ] 3-edge composition POST/GET roundtrip
  - [ ] Edge ordering preserved

### Phase 5: Integration & Polish

- [ ] py-py chaining manual test: `[py|py]` → `[py|py|prompt]`
- [ ] Update ADR-009 for Phase 3 completion

## Open Questions

1. **Unmeld granularity?** Should pulling a middle glyph split the composition in two, or always unmeld everything?
2. **Maximum composition size?** No hard limit yet — monitor UX as chains grow.

## Design Decisions

**py → py chaining:**
- **Decision:** Enabled in Phase 2 via port-aware MELDABILITY registry
- **Rationale:** Sequential Python pipelines are a common pattern (ETL, data transformation chains)
- **Implementation:** `MELDABILITY['canvas-py-glyph']` includes `{ direction: 'right', targets: ['canvas-py-glyph'] }`

**Edge-based composition IDs:**
- **Decision:** Regenerate ID on composition extension (`melded-{from}-{to}`)
- **Rationale:** Keeps naming consistent with creation pattern; old ID removed from storage, new ID added

**Cross-axis sub-containers:**
- **Decision:** Nested `meld-sub-container` divs for mixed-direction compositions
- **Rationale:** A single flex container can only flow in one direction. When a composition has both horizontal (right) and vertical (bottom) edges, the cross-axis glyphs need their own flex container.
- **Implementation:** `extendComposition()` detects cross-axis and wraps anchor + incoming in a sub-container. `reconstructMeld()` builds sub-containers from stored edges. All guards use `.closest('.melded-composition')` to traverse through sub-containers.

**Meld system module split:**
- **Decision:** Split monolith `meld-system.ts` into `meld-detect.ts`, `meld-feedback.ts`, `meld-composition.ts` with barrel re-export
- **Rationale:** File exceeded 700 lines with three distinct concerns. Barrel re-export preserves all existing import paths.

## Migration Strategy

**Actual approach (Phase 1):** Clean breaking change

Since no melded compositions exist in production yet, we opted for a simpler breaking change:
- Database migration recreates `canvas_compositions` table (drops old schema)
- No backward compatibility layer
- Production database deleted and recreated with new schema
- All tests updated to use new format

**Rationale:**
- Simpler implementation (no dual-format handling)
- No data loss risk (no production compositions exist)
- Cleaner codebase without compatibility shims

## Related Work

- ✅ PR #407 - Glyph melding foundation
- ✅ PR #428 - Glyph persistence to database
