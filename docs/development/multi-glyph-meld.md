# Multi-Glyph Chain Melding Implementation

**Issue:** [#411](https://github.com/teranos/QNTX/issues/411)
**Goal:** Support linear chains of 3+ glyphs: `[ax|py|prompt]`

## Current State

**Phase 1 Complete:**
- ✅ Storage layer supports N-glyph compositions (via junction table)
- ✅ Four composition types: `ax-prompt`, `ax-py`, `py-prompt`, `ax-py-prompt`
- ✅ Backend API accepts/returns `glyphIds: string[]`
- ✅ Frontend state updated for array format
- ✅ All tests passing (Go + TypeScript)

**Still TODO (Phase 2-5):**
- ❌ UI doesn't support melding composition + glyph yet (still binary only)
- ❌ Meldability logic doesn't recognize compositions as targets
- ❌ No 3+ glyph chain creation in browser yet

## Architecture Decision

**Flat list structure** (not recursive nesting)
- Composition contains N glyphs as direct children
- Data flows left-to-right through linear chain
- Matches vision doc: "linear chains A → B → C"

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

### Phase 2: Meldability Logic

- [ ] Update `web/ts/components/glyph/meldability.ts`
  - [ ] Add `'melded-composition'` to `MELDABILITY` registry
  - [ ] Define what glyphs can meld with compositions
  - [ ] Add `getCompositionGlyphIds(composition: HTMLElement): string[]` helper

- [ ] Update `web/ts/state/compositions.ts`
  - [ ] Add `getCompositionType()` support for 3-glyph chains
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

### Phase 5: Integration & Polish

- [ ] Test manually in browser
  - [ ] Create `[ax|py]`, verify it works
  - [ ] Drag `prompt` to meld → verify `[ax|py|prompt]` forms
  - [ ] Refresh page → verify chain persists
  - [ ] Unmeld → verify all 3 glyphs separate correctly

- [ ] Update documentation
  - [ ] Add example to `docs/vision/glyph-melding.md`
  - [ ] Note limitations (linear only, no branching)

## Open Questions

1. **Maximum chain length?** Propose: limit to 4-5 glyphs for UX clarity
2. **Chain validation?** Should we enforce valid orderings (e.g., prevent `[prompt|ax]`)?
3. **Data flow semantics?** How does data pass through 3-glyph chains?
4. **Unmeld granularity?** Should pulling middle glyph split chain in two?

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
