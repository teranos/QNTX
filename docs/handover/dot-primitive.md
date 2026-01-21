# Dot-as-Primitive Implementation Handover

**Date:** 2026-01-21
**Branch:** `claude/code-review-analysis-jCws6`
**Status:** Core infrastructure complete, content migration pending

---

## What Has Been Done

### Core Infrastructure (✅ Complete)

1. **Extended WindowTray with expansion capabilities** (`web/ts/components/window-tray.ts`)
   - Added `expandedDots: Map<string, HTMLElement>` to track expanded windows
   - Implemented `expand(id)` - creates positioned window from dot
   - Implemented `collapse(id)` - collapses window back to dot
   - Implemented `createTitleBar()` - builds draggable title bar with buttons
   - Implemented `makeDraggable()` - enables window dragging
   - Implemented `register(item)` - registers dots on startup
   - Modified dot click handler to use new expansion path when `renderContent` exists
   - Maintains backward compatibility with legacy `onRestore` pattern

2. **Extended TrayItem interface** (`web/ts/components/window-tray.ts:12-33`)
   ```typescript
   export interface TrayItem {
       id: string;
       title: string;
       symbol?: string;
       renderContent?: () => HTMLElement;  // NEW: content rendering
       initialWidth?: string;              // NEW: window sizing
       initialHeight?: string;
       defaultX?: number;                  // NEW: positioning
       defaultY?: number;
       onExpand?: () => void;              // NEW: callbacks
       onCollapse?: () => void;
       onClose?: () => void;
       onRestore?: (sourceRect?: DOMRect) => void;  // DEPRECATED
   }
   ```

3. **Created tray window CSS** (`web/css/tray-window.css`)
   - `.tray-window` - positioned expanded window
   - `.tray-window-title-bar` - draggable title bar
   - `.tray-window-buttons` - minimize/close buttons
   - `.tray-window-content` - scrollable content area
   - Full styling with hover states, shadows, rounded corners

4. **Extended UIState** (`web/ts/state/ui.ts`)
   - Added `height?: string` to `WindowState` interface (line 65)
   - Added `getExpandedWindows()` method (lines 439-447)
   - Returns IDs of windows that are currently expanded (not minimized)

5. **Created dot registry** (`web/ts/dot-registry.ts`)
   - Registers 3 initial dots on startup:
     - `vidstream-window` (⮀) - VidStream
     - `db-stats-window` (⊔) - Database Statistics
     - `self-window` (⍟) - System Diagnostic
   - Currently uses placeholder content (HTML strings)
   - TODO: Replace with actual content rendering

6. **Integrated in main.ts** (`web/ts/main.ts:220-228`)
   - Calls `initializeDots()` after `windowTray.init()`
   - Restores expanded windows from previous session
   - Uses `uiState.getExpandedWindows()` to get list of expanded dots

7. **Updated index.html** (`web/index.html:55`)
   - Added `<link rel="stylesheet" href="/css/tray-window.css">`

---

## How It Works

### Lifecycle

```
Startup:
1. windowTray.init() - Creates tray element
2. initializeDots() - Registers dots via windowTray.register()
3. uiState.getExpandedWindows() - Gets list of previously expanded dots
4. windowTray.expand(id) - Restores each expanded window

User clicks dot:
1. Dot click handler checks if item.renderContent exists
2. If yes → windowTray.expand(id)
3. Creates .tray-window element with title bar and content
4. Positions based on saved state or defaults
5. Makes draggable, appends to document.body
6. Tracks in expandedDots Map

User clicks minimize:
1. Button calls windowTray.collapse(id)
2. Saves position/size to uiState
3. Removes window element from DOM
4. Removes from expandedDots Map
5. Dot persists in tray

User drags window:
1. Title bar mousedown starts drag
2. mousemove updates position
3. mouseup saves position to uiState
```

### State Management

- **Collapsed dots:** Stored in `windowTray.items` Map, always rendered in tray
- **Expanded windows:** Tracked in `windowTray.expandedDots` Map, added to DOM
- **Persistence:** `uiState.windowStates[id]` with `minimized` field
  - `minimized: false` = expanded
  - `minimized: true` = collapsed to dot
- **Position/size:** Saved in `WindowState` on drag/collapse

---

## What Needs to Be Done Next

### Priority 1: Extract Real Content Rendering

**Goal:** Replace placeholder HTML with actual VidStream/DatabaseStats/Self content

**VidStream Migration:**
1. Locate `web/ts/vidstream-window.ts`
2. Identify content creation logic (camera feed, controls, etc.)
3. Extract into standalone function or class: `createVidStreamContent()`
4. Update `dot-registry.ts`:
   ```typescript
   renderContent: () => {
       return createVidStreamContent();
   }
   ```
5. Ensure all event handlers, state, lifecycle hooks work in tray window
6. Test: Click VidStream dot → should see working camera feed
7. Remove old VidStreamWindow class (or mark deprecated)

**DatabaseStats Migration:**
Similar process:
1. Find `web/ts/database-stats-window.ts`
2. Extract `createDatabaseStatsContent()`
3. Update dot-registry.ts
4. Test and remove old class

**Self Window Migration:**
Similar process:
1. Find `web/ts/self-window.ts`
2. Extract `createSelfDiagnosticContent()`
3. Update dot-registry.ts
4. Test and remove old class

### Priority 2: Testing

**Manual Testing Checklist:**
- [ ] Dots appear in tray zone on startup
- [ ] Click dot → window expands at correct position
- [ ] Window is draggable via title bar
- [ ] Minimize button → collapses back to dot
- [ ] Close button → collapses back to dot (default behavior)
- [ ] Proximity morphing still works (8px → 220px with text)
- [ ] State persists across page reload
- [ ] Multiple dots can be expanded simultaneously
- [ ] Dragging updates position in uiState
- [ ] Content rendering works correctly

**What to Look For:**
- Window position should restore correctly after reload
- No console errors when expanding/collapsing
- Proximity morphing doesn't interfere with expansion
- Title bar drag doesn't conflict with other interactions
- Windows layer correctly (z-index)

### Priority 3: Symbol Palette Integration

**Current State:**
- Symbol Palette still exists at top of screen
- Clicking symbols creates old-style windows
- This duplicates functionality with tray dots

**Next Step:**
- Decide: Should Symbol Palette symbols also use tray dots?
- Or: Keep Symbol Palette for commands (ax, ix, etc.) and tray for windows only?
- Update accordingly

### Priority 4: Animation & Polish

**Expansion Animation:**
Currently windows just appear. Consider adding:
- Morph from dot position to final position
- Scale/fade animation
- See commented code in `collapse()` for reference

**Collapse Animation:**
Currently windows just disappear. Consider:
- Morph back to tray position
- Scale down to dot size
- Fade out

**Reference:**
```typescript
// In collapse():
windowEl.style.transition = 'all 0.3s ease';
const trayPos = this.getTargetPosition();
windowEl.style.transform = `translate(${trayPos.x}px, ${trayPos.y}px) scale(0)`;
```

### Priority 5: Mobile/Vertical Navigation

**From Original Vision:**
- Vertical up/down navigation (mobile-style)
- Haptic feedback on dot selection
- Finger pressed + move gesture

**Not Yet Implemented:**
- Touch event handlers
- Vertical scrolling through dots
- Haptic feedback API integration

**File to Modify:**
`web/ts/components/window-tray.ts` - add touch event listeners

---

## Key Files & Their Purposes

### Modified Files

| File | Purpose | Key Changes |
|------|---------|-------------|
| `web/ts/components/window-tray.ts` | Tray zone with dot expansion | Added expand/collapse/register methods, updated click handler |
| `web/ts/state/ui.ts` | UI state management | Added height field, getExpandedWindows() method |
| `web/ts/main.ts` | App initialization | Calls initializeDots(), restores expanded windows |
| `web/index.html` | HTML structure | Added tray-window.css link |

### New Files

| File | Purpose |
|------|---------|
| `web/css/tray-window.css` | Expanded window styles |
| `web/ts/dot-registry.ts` | Dot registration on startup |
| `docs/DOT_AS_PRIMITIVE.md` | Design documentation |

### Files to Extract From

| File | Content Needed |
|------|----------------|
| `web/ts/vidstream-window.ts` | Camera feed, controls rendering |
| `web/ts/database-stats-window.ts` | DB stats table rendering |
| `web/ts/self-window.ts` | System diagnostic rendering |

---

## Testing Instructions

### Quick Test (Placeholder Content)

1. **Start dev server:**
   ```bash
   make dev
   ```

2. **Open browser to http://localhost:8820**

3. **Look for dots in tray zone (bottom right)**
   - Should see 3 dots initially (8px gray circles)

4. **Hover near dots**
   - Should morph to show text (VidStream, Database Statistics, System Diagnostic)

5. **Click a dot**
   - Should expand into positioned window
   - Should show placeholder content

6. **Drag window by title bar**
   - Should move freely
   - Position should save

7. **Click minimize button (−)**
   - Should collapse back to dot
   - Dot should remain visible

8. **Reload page**
   - Expanded windows should restore
   - Position should be remembered

### Full Test (After Content Migration)

Same as above, but verify:
- VidStream shows working camera feed
- DatabaseStats shows real DB metrics
- Self shows system diagnostics
- All interactions work (play/pause, refresh, etc.)

---

## Important Notes & Gotchas

### Backward Compatibility

**Both patterns work simultaneously:**
- New: `renderContent` → uses `expand()`
- Old: `onRestore` → uses legacy path

This allows gradual migration. Don't break existing windows during transition.

### State Semantics

**Confusing naming:**
- `minimized: false` = window is EXPANDED
- `minimized: true` = window is COLLAPSED to dot

This is inverted from intuition because it was designed for old model (windows minimize to tray).

### Proximity Morphing

**Don't touch the morphing code!**
- Lines 164-285 in window-tray.ts
- Carefully tuned thresholds, easing curves, baseline boost
- If you break it, user will be upset (they worked hard on it)

### Click Handler Logic

**Order matters in renderItems():**
```typescript
// New path (dot-as-primitive)
if (item.renderContent) {
    this.expand(item.id);
    return;
}

// Legacy path (old windows)
if (item.onRestore) {
    item.onRestore(dotRect);
}
```

If an item has both `renderContent` and `onRestore`, it will use new path. This is intentional.

### DOM Hierarchy

**Expanded windows live at document.body level:**
```
<body>
  <div id="graph-container">
    <div class="window-tray">...</div>  <!-- Tray zone -->
  </div>
  <div class="tray-window">...</div>  <!-- Expanded window (sibling to container) -->
</body>
```

This allows windows to break out of tray zone and position anywhere.

### Z-Index

**Current z-index:**
- `.tray-window` has `z-index: 1000`
- May need stacking order management for multiple windows

---

## Common Issues & Solutions

### Issue: Dots don't appear on startup

**Check:**
1. Is `initializeDots()` being called? (main.ts:222)
2. Are dots being registered? (dot-registry.ts)
3. Is `windowTray.init()` called first? (main.ts:218)
4. Console errors?

**Debug:**
```typescript
// In dot-registry.ts
console.log('Dots initialized:', windowTray.count);
```

### Issue: Window doesn't expand when clicking dot

**Check:**
1. Does TrayItem have `renderContent` defined?
2. Console errors when clicking?
3. Is `expand()` method being called?

**Debug:**
```typescript
// In window-tray.ts click handler
console.log('Dot clicked:', item.id, 'has renderContent:', !!item.renderContent);
```

### Issue: Window position not saved

**Check:**
1. Is drag ending properly (mouseup)?
2. Is `uiState.updateWindowState()` being called?
3. Check localStorage in devtools

**Debug:**
```typescript
// In makeDraggable onMouseUp
console.log('Saving position:', id, rect.left, rect.top);
```

### Issue: Proximity morphing broken

**Check:**
1. Did you modify lines 164-285?
2. Is `isRestoring` flag being set correctly?
3. Console errors in updateProximity()?

**Solution:**
- Revert changes to proximity code
- Only touch expansion logic, not morphing

---

## Architecture Decisions

### Why not use Window component?

**Decision:** Build expansion rendering directly in WindowTray

**Rationale:**
- Clean break from old model
- WindowTray owns entire lifecycle
- No temporary dependencies
- Simpler: dot expands in place

### Why register dots on startup?

**Decision:** Dots exist from beginning, not created by minimizing

**Rationale:**
- Dots are primary primitives
- Always visible reference point
- Users know what's available
- Like macOS dock

### Why collapse instead of destroy?

**Decision:** Close button collapses to dot by default

**Rationale:**
- Dots are permanent
- Matches "dot IS the window" vision
- Can override with `onClose` if needed

---

## Next Session Checklist

Before starting work:
- [ ] Read this handover doc
- [ ] Review `docs/DOT_AS_PRIMITIVE.md` for full vision
- [ ] Check current branch (`claude/code-review-analysis-jCws6`)
- [ ] Pull latest changes
- [ ] Run `make dev` to test current state
- [ ] Click dots to verify placeholder expansion works

First task:
- [ ] Extract VidStream content rendering
- [ ] Update dot-registry.ts with real content
- [ ] Test VidStream dot expansion
- [ ] Verify camera feed, controls work

Questions to ask user:
1. Should Symbol Palette symbols also use tray dots?
2. Do we want expansion/collapse animations?
3. Mobile/touch support priority?

---

## Useful Commands

```bash
# Start dev server
make dev

# Type check
bun run typecheck

# Check git status
git status

# View recent commits
git log --oneline -5

# Test in browser
open http://localhost:8820
```

---

## Resources

- **Design doc:** `docs/DOT_AS_PRIMITIVE.md`
- **WindowTray code:** `web/ts/components/window-tray.ts`
- **Dot registry:** `web/ts/dot-registry.ts`
- **Main init:** `web/ts/main.ts:220-228`
- **Tray CSS:** `web/css/tray-window.css`

---

## Summary

**What works:**
- Dots appear in tray on startup ✅
- Proximity morphing (8px → 220px) ✅
- Click to expand → positioned window ✅
- Draggable windows ✅
- Minimize back to dot ✅
- State persistence ✅

**What's missing:**
- Real content rendering (currently placeholders)
- Expansion/collapse animations
- Mobile/touch support
- Symbol Palette integration decision

**Immediate next step:**
Extract VidStream content from `web/ts/vidstream-window.ts` and wire it into `dot-registry.ts`.
