# Multi-Glyph Chain Melding Implementation

**Issue:** [#411](https://github.com/teranos/QNTX/issues/411)
**Goal:** Support linear chains of 3+ glyphs: `[ax|py|prompt]`

## Current State

**Phase 1 Complete:**
- ✅ Storage layer supports N-glyph compositions (via junction table)
- ✅ Backend API accepts/returns `glyphIds: string[]`
- ✅ Frontend state updated for array format
- ✅ All tests passing (Go + TypeScript)

**Phase 1b Required (DAG Migration):**
- ❌ Current `glyphIds: string[]` structure cannot represent DAG topologies
- ❌ Need edge-based structure for multi-directional melding (horizontal, top, bottom)
- ❌ Breaking change required: migrate to `composition_edges` table
- ❌ Remove `type` field (edges are the type information)

**Still TODO (Phase 2-5):**
- ❌ UI doesn't support melding composition + glyph yet (still binary only)
- ❌ Meldability logic doesn't recognize compositions as targets
- ❌ No 3+ glyph chain creation in browser yet
- ❌ py → py chaining not yet enabled (planned extension for sequential pipelines)
- ❌ Vertical melding (top/bottom directions) not yet supported

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
- Phase 2-3 only create `direction: 'right'` edges (horizontal chains)
- Structure ready for vertical melding (future work)
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
- ✅ Update `web/ts/components/glyph/meld-system.test.ts` for new unmeld return format
- ✅ Add skipped TDD tests in `web/ts/state/compositions.test.ts`:
  - ✅ 3-glyph composition stores correctly
  - ✅ `isGlyphInComposition` works with 3-glyph chains
  - ✅ `findCompositionByGlyph` finds 3-glyph chains
  - ✅ Extending composition adds glyph to array
- ✅ Add skipped TDD tests in `web/ts/components/glyph/meld-system.test.ts`:
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

- ✅ Update `web/ts/components/glyph/meld-system.test.ts`
  - ✅ Remove `glyphIds` expectation from `unmeldComposition()` return value

### Phase 1bc: Frontend UI Integration ⏸️ **NEXT**

**Goal:** Update meld system and UI to work with edge-based compositions.

**Status:** Phase 1bb complete, ready to start 1bc

#### Meld System

- [ ] Update `web/ts/components/glyph/meld-system.ts`
  - [ ] Update `performMeld()`:
    - [ ] Create composition with `edges` instead of `glyphIds`
    - [ ] Build edge: `{ from: initiatorGlyph.id, to: targetGlyph.id, direction: 'right' }`
    - [ ] Remove `type` computation (no longer stored)
  - [ ] Update `reconstructMeld()`:
    - [ ] Accept `edges` parameter instead of deriving from glyphIds
    - [ ] Extract glyphIds from edges for DOM lookup
  - [ ] Update `unmeldComposition()`:
    - [ ] Return edges in result (for caller to know structure)
    - [ ] Remove type-related logic

- [ ] Update `web/ts/components/glyph/glyph-interaction.ts`
  - [ ] Ensure composition restoration uses edges
  - [ ] Update any references to `glyphIds` to use edge extraction

#### Meld System Tests

- [ ] Update `web/ts/components/glyph/meld-system.test.ts`
  - [ ] Update all test expectations to check `edges` instead of `glyphIds`
  - [ ] Remove `type` assertions
  - [ ] Verify edges have correct `direction: 'right'`
  - [ ] Run `bun test` - expect all tests to pass

#### Integration Testing

- [ ] Manual browser test: Create 2-glyph composition
  - [ ] Drag ax near prompt → meld
  - [ ] Verify composition saved with edges to backend
  - [ ] Refresh page → verify composition restored correctly
  - [ ] Unmeld → verify both glyphs restore independently

- [ ] Manual browser test: Create 3-glyph chain (once Phase 2 complete)
  - [ ] Create ax|py composition
  - [ ] Drag prompt onto it → verify extends to ax|py|prompt
  - [ ] Refresh → verify 3-glyph chain persists
  - [ ] Unmeld → verify all 3 glyphs separate

- [ ] Run full test suite: `make test`
  - [ ] All Go tests pass
  - [ ] All TypeScript tests pass
  - [ ] No regressions

### Phase 2: Meldability Logic

- [ ] Update `web/ts/components/glyph/meldability.ts`
  - [ ] Add `'melded-composition'` to `MELDABILITY` registry
  - [ ] Define what glyphs can meld with compositions
  - [ ] Add `getCompositionGlyphIds(composition: HTMLElement): string[]` helper
  - [ ] **Extension:** Enable py → py chaining
    - [ ] Add `'canvas-py-glyph'` to py's compatible targets: `['canvas-prompt-glyph', 'canvas-py-glyph']`
    - [ ] Enables sequential Python pipelines: `py|py`, `py|py|prompt`, `ax|py|py`
    - [ ] Semantic: output of first py script feeds into second py script

- [ ] Update `web/ts/state/compositions.ts`
  - [ ] Add `getCompositionType()` support for 3-glyph chains
  - [ ] Add support for new composition types: `'py-py'`, `'py-py-prompt'`, `'ax-py-py'`
  - [ ] Add helper: `isCompositionMeldable(comp: CompositionState, glyphType: string): boolean`
  - [ ] Update `addComposition()` to handle N glyphs

### Phase 3: DOM Manipulation

- [ ] Update `web/ts/components/glyph/meld-system.ts`
  - [ ] Modify `findMeldTarget()` to recognize compositions as valid targets
  - [ ] Update `performMeld()`:
    - [ ] Detect if target is composition (`isMeldedComposition(target)`)
    - [ ] If composition: append glyph to existing container (don't create new wrapper)
    - [ ] If glyph: create new composition (existing behavior)
    - [ ] Update `data-glyph-ids` attribute with full array
  - [ ] Update `unmeldComposition()`:
    - [ ] Restore N glyphs (not just 2)
    - [ ] Space glyphs horizontally based on count
    - [ ] Return array of restored elements
  - [ ] Update `reconstructMeld()` for N-glyph restoration

- [ ] Update `web/ts/components/glyph/glyph-interaction.ts`
  - [ ] Ensure composition targets work in `handleMouseUp` (line ~160)
  - [ ] Update meld feedback to work with composition targets

#### CSS Styling
- [ ] Update `web/css/glyph/meld.css`
  - [ ] Add rule: `.melded-composition > *:not(:last-child)` for separator borders
  - [ ] Support variable gap count based on child count
  - [ ] Ensure spacing works for 3+ glyphs

### Phase 4: Tests

**TDD tests already written (skipped until implementation):**

**Frontend tests:**
- [ ] Unskip `web/ts/components/glyph/meld-system.test.ts`:
  - [ ] "Tim creates 3-glyph chain (ax|py|prompt) by dragging onto composition"
  - [ ] "Tim sees proximity feedback when dragging glyph toward composition"
  - [ ] "Tim extends ax|py composition by dragging prompt onto it"
  - [ ] "Tim extends 3-glyph chain into 4-glyph chain"
- [ ] Unskip `web/ts/state/compositions.test.ts`:
  - [ ] "3-glyph composition stores correctly"
  - [ ] "isGlyphInComposition works with 3-glyph chains"
  - [ ] "findCompositionByGlyph finds 3-glyph chains"
  - [ ] "extending composition adds glyph to array"

**Backend tests:**
- [ ] Unskip `glyph/handlers/canvas_test.go`:
  - [ ] `TestCanvasHandler_HandleCompositions_POST_ThreeGlyphs` (POST with 3 glyph IDs)
  - [ ] `TestCanvasHandler_HandleCompositions_GET_PreservesGlyphOrder` (verifies left-to-right order)
  - [ ] `TestCanvasHandler_HandleCompositions_POST_FourGlyphChain` (4-glyph chain extensibility)

**Additional tests needed:**
- [ ] Add visual layout tests
  - [ ] Test: glyphs horizontally aligned in 3-glyph chain
  - [ ] Test: no overlap between adjacent glyphs
  - [ ] Test: proper spacing maintained
- [ ] Add py-py chaining tests
  - [ ] Test: `py + py → [py|py]` creates 2-glyph py chain
  - [ ] Test: `[py|py] + prompt → [py|py|prompt]` extends py chain
  - [ ] Test: `ax + py → [ax|py]` then `[ax|py] + py → [ax|py|py]`
  - [ ] Test: meldability registry allows py → py melding

### Phase 5: Integration & Polish

- [ ] Test manually in browser
  - [ ] Create `[ax|py]`, verify it works
  - [ ] Drag `prompt` to meld → verify `[ax|py|prompt]` forms
  - [ ] Refresh page → verify chain persists
  - [ ] Unmeld → verify all 3 glyphs separate correctly
  - [ ] **py-py chaining:**
    - [ ] Create `[py|py]` by dragging py onto py
    - [ ] Extend to `[py|py|prompt]` by dragging prompt
    - [ ] Verify `[ax|py|py]` works (ax followed by 2 py scripts)
    - [ ] Test data flows correctly through sequential py scripts

- [ ] Update documentation
  - [ ] Add example to `docs/vision/glyph-melding.md`
  - [ ] Document py-py chain semantics (sequential pipeline)
  - [ ] Note limitations (linear only, no branching)

## Open Questions

1. **Maximum chain length?** Propose: limit to 4-5 glyphs for UX clarity
2. **Chain validation?** Should we enforce valid orderings (e.g., prevent `[prompt|ax]`)?
3. **Data flow semantics?** How does data pass through 3-glyph chains?
4. **Unmeld granularity?** Should pulling middle glyph split chain in two?

## Design Decisions

**py → py chaining (Phase 2 extension):**
- **Decision:** Enable py glyphs to chain with other py glyphs
- **Rationale:** Sequential Python pipelines are a common pattern (ETL, data transformation chains)
- **Semantic:** Output of first py script feeds as input to second py script
- **New composition types:** `py-py`, `py-py-prompt`, `ax-py-py`, `py-py-py`, etc.
- **Implementation:** Update `MELDABILITY` to include `'canvas-py-glyph': ['canvas-prompt-glyph', 'canvas-py-glyph']`

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
