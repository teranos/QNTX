# Multi-Glyph Chain Melding Implementation

**Issue:** [#411](https://github.com/teranos/QNTX/issues/411)
**Goal:** Support linear chains of 3+ glyphs: `[ax|py|prompt]`

## Current State

- ✅ Binary melding works (2 glyphs only)
- ✅ Three composition types: `ax-prompt`, `ax-py`, `py-prompt`
- ✅ Persistence to database via `canvas_compositions` table
- ❌ Cannot meld composition + glyph
- ❌ No support for 3+ glyph chains

## Architecture Decision

**Flat list structure** (not recursive nesting)
- Composition contains N glyphs as direct children
- Data flows left-to-right through linear chain
- Matches vision doc: "linear chains A → B → C"

## Implementation Checklist

### Phase 1: State & Storage

#### Frontend State
- [ ] Update `CompositionState` type in `web/ts/state/ui.ts`
  - [ ] Change `initiatorId: string` + `targetId: string` to `glyphIds: string[]`
  - [ ] Add new composition types: `'ax-py-prompt'`, etc.
  - [ ] Keep backward compatibility for existing 2-glyph compositions

#### Backend Storage
- [ ] Create migration `020_multi_glyph_compositions.sql`
  - [ ] Add `glyph_order INTEGER` column to `canvas_compositions`
  - [ ] Create junction table `composition_glyphs(composition_id, glyph_id, position)`
  - [ ] Migrate existing data: convert `initiator_id`/`target_id` to junction entries

- [ ] Update `glyph/storage/canvas_store.go`
  - [ ] Change `GetComposition()` to return `[]string` of glyph IDs
  - [ ] Change `UpsertComposition()` to accept glyph ID array
  - [ ] Add `AddGlyphToComposition(compId, glyphId, position)` method

- [ ] Update `glyph/handlers/canvas.go`
  - [ ] Update API payload to accept `glyph_ids: string[]`
  - [ ] Return composition with full glyph array

#### Storage Tests
- [ ] Update `glyph/storage/canvas_store_test.go`
  - [ ] Test 3-glyph composition creation
  - [ ] Test adding glyph to existing composition
  - [ ] Test ordering of glyphs in chain

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

- [ ] Update `web/ts/components/glyph/meld-system.test.ts`
  - [ ] Test: `ax + py → [ax|py]` (existing)
  - [ ] Test: `[ax|py] + prompt → [ax|py|prompt]` (NEW)
  - [ ] Test: `unmeld [ax|py|prompt] → ax, py, prompt` (NEW)
  - [ ] Test: `composition appears in meldability registry` (NEW)
  - [ ] Test: `findMeldTarget() detects compositions` (NEW)
  - [ ] Test: `restore 3-glyph composition from storage` (NEW)

- [ ] Update handler tests in `glyph/handlers/canvas_test.go`
  - [ ] Test POST with 3 glyph IDs
  - [ ] Test GET returns correct glyph order

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

**Backward compatibility:**
- Existing 2-glyph compositions must keep working
- Database migration converts `initiator_id`/`target_id` to glyph array
- Frontend reads both old and new formats during transition

**Rollout:**
1. Add new schema alongside old (no breaking changes)
2. Migrate existing data
3. Update frontend to write new format
4. Deprecate old columns (future cleanup)

## Related Work

- ✅ PR #407 - Glyph melding foundation
- ✅ PR #428 - Glyph persistence to database
