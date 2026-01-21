# Dot-as-Primitive Architecture

## Vision

The dot is the fundamental primitive of QNTX UI. Every feature, window, and panel is represented as a dot that can expand. This unifies the Symbol Palette and WindowTray into a single, consistent system inspired by macOS dock.

**Key principle:** The dot IS the thing. Windows are temporary expanded views.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        Dock (Bottom of screen)               │
│  [⋈] [≡] [꩜] [⊔] [⚇] [Go] [py] [⚙] [⛶] [⮀] [⨳] ...        │
└─────────────────────────────────────────────────────────────┘
   │    │    │    │    │
   └────┴────┴────┴────┴──> Each is a Dot primitive

When clicked, dot expands:
- Windows (VidStream, Database, Self) → Full draggable window
- Panels (Config, Prose, Pulse) → Slide-in panel (TODO: migrate)
- Commands (ax, ix) → Focus query input or show explorer

When closed/minimized:
- Window collapses back to dot
- Dot remains visible in dock (always accessible)
```

## Core Components

### 1. Dot (dot.ts)
The primary primitive. Represents a feature/window/panel identity.

**State:**
- Collapsed: 8px circle in dock
- Expanded: Full window or panel visible

**Lifecycle:**
```typescript
const dot = new Dot({
    id: 'vidstream-window',
    symbol: '⮀',
    title: 'VidStream',
    windowConfig: { width: '700px' },
    onExpand: () => console.log('Expanded'),
    onCollapse: () => console.log('Collapsed'),
});

dock.register(dot);  // Adds to dock
dot.expand();        // Shows window
dot.collapse();      // Hides window (dot remains in dock)
dot.destroy();       // Removes from dock permanently
```

### 2. Dock (dock.ts)
Unified container that manages all dots.

**Features:**
- Proximity morphing (8px → 220px) with text
- Direction-aware easing (horizontal vs vertical)
- Baseline boost (when any dot is close, all expand slightly)
- 60fps RAF updates

**Configuration (preserved from WindowTray):**
```typescript
PROXIMITY_THRESHOLD_HORIZONTAL = 30px  // Mouse distance for expansion
PROXIMITY_THRESHOLD_VERTICAL = 110px   // Vertical has larger range
BASELINE_BOOST_TRIGGER = 0.80          // When to boost all dots
TEXT_FADE_THRESHOLD = 0.5              // When to show text
```

### 3. Dot Registry (dot-registry.ts)
Central registry of standard QNTX dots.

Initializes all available features as dots on startup.

## Proximity Morphing

Preserved from WindowTray's carefully tuned behavior:

### Visual Transformation
```
Idle (far away):
[•] 8px gray circle

Proximity ~30-50%:
[•••] Expands to ~50px, darkens

Proximity ~80%:
[••••••••••] Expands to ~180px, shows text

Fully hovered:
[  VidStream  ] 220px bar with title, dark background
```

### Easing Curves

**Horizontal approach** (gradual → dramatic):
- 0-80% proximity: Slow growth (40% of expansion)
- 80-90%: Dramatic finish (60% of expansion)
- Creates "dramatic reveal" effect

**Vertical approach** (fast bloom → refinement):
- 0-55% proximity: Fast growth (80% of expansion)
- 55-100%: Slow refinement (20% of expansion)
- Creates "bloom from bottom" effect

**Baseline Boost:**
When any dot reaches 80% proximity, ALL dots boost by 30%. This creates a cohesive "the dock is hot" feeling.

## Migration Guide

### Current System (Legacy)
```typescript
// Symbol Palette (symbol-palette.ts)
case 'vidstream':
    showVidStreamWindow();
    break;

function showVidStreamWindow() {
    if (!vidstreamWindowInstance) {
        vidstreamWindowInstance = new VidStreamWindow();
    }
    vidstreamWindowInstance.toggle();
}
```

### New System (Dot-based)
```typescript
// Dot Registry (dot-registry.ts)
const vidstreamDot = new Dot({
    id: 'vidstream-window',
    symbol: '⮀',
    title: 'VidStream - Real-time Video Inference',
    tooltip: '⮀ VidStream - video inference',
    windowConfig: {
        width: '700px',
        // Window content is still created by VidStreamWindow
    },
    onExpand: () => {
        log(SEG.VID, 'VidStream expanded');
        // Initialize VidStream content
    },
});
dock.register(vidstreamDot);
```

## Migration Steps for a Feature

1. **Register dot in dot-registry.ts**
   ```typescript
   const myFeatureDot = new Dot({
       id: 'my-feature',
       symbol: '◆',
       title: 'My Feature',
       windowConfig: { width: '600px' },
       onExpand: () => initMyFeature(),
   });
   dock.register(myFeatureDot);
   ```

2. **Remove from symbol-palette.ts**
   ```typescript
   // DELETE:
   case 'my-feature':
       showMyFeature();
       break;
   ```

3. **Update feature implementation**
   - If using Window class: pass Dot's window instance
   - If using BasePanel: will need panel migration (TODO)

4. **Test**
   - Click dot → feature expands
   - Close feature → collapses back to dot
   - Dot remains clickable in dock

## Coexistence During Migration

**Current state:** Both systems coexist:
- Old: Symbol Palette (top bar) + WindowTray (bottom)
- New: Dock (bottom)

**Strategy:**
1. Keep both systems running
2. Migrate features one by one from Palette → Dock
3. Once all migrated, remove Palette and WindowTray
4. Dock becomes the sole UI primitive system

## State Management

Dot state is persisted via `uiState`:

```typescript
// WindowState interface (ui.ts)
export interface WindowState {
    x: number;
    y: number;
    width: string;
    minimized: boolean;  // false = expanded, true = collapsed to dot
}

// Dots save state on expand/collapse
dot.expand()  → uiState.updateWindowState(id, { minimized: false })
dot.collapse() → uiState.updateWindowState(id, { minimized: true })
```

## File Structure

```
web/ts/
├── components/
│   ├── dot.ts              # Dot primitive class
│   ├── dock.ts             # Dock container with proximity morphing
│   ├── window.ts           # Window component (owned by Dot)
│   └── window-tray.ts      # Legacy tray (will be removed)
├── dot-registry.ts         # Standard QNTX dots
├── symbol-palette.ts       # Legacy palette (will be removed)
└── DOT-ARCHITECTURE.md     # This file

web/css/
├── dock.css                # Dock and dot styles
├── window.css              # Window styles
├── window-tray.css         # Legacy tray styles (will be removed)
└── symbol-palette.css      # Legacy palette styles (will be removed)
```

## Design Decisions

### Why dots instead of windows?

**Problem with window-first:**
- Windows can minimize → user loses reference
- Tray is separate from launcher
- Two systems for same conceptual space

**Dot-first solution:**
- Dot is permanent reference point
- Single unified dock
- Like macOS: icon IS the app, window is ephemeral

### Why preserve WindowTray morphing?

The proximity morphing was carefully tuned with:
- Specific distance thresholds
- Direction-aware easing curves
- Baseline boost for cohesion
- Text fade thresholds

This interaction design took significant effort and user feedback. The Dock preserves it exactly.

### Why gradual migration?

- Allows testing each feature independently
- Reduces risk of breaking existing workflows
- Enables comparison of old vs new patterns
- Can revert individual features if needed

## Next Steps

1. ✅ Create Dot, Dock, and CSS infrastructure
2. ✅ Wire up Dock in main.ts (coexists with old system)
3. ⏳ Migrate first feature (VidStream) to Dot system
4. Test proximity morphing and state persistence
5. Migrate remaining features one by one
6. Remove Symbol Palette and WindowTray
7. Update all references in codebase

## Testing Checklist

For each migrated feature:
- [ ] Dot appears in dock on startup
- [ ] Clicking dot expands feature
- [ ] Proximity morphing works (8px → 220px)
- [ ] Text appears at >50% proximity
- [ ] Baseline boost activates at 80%
- [ ] Closing feature collapses to dot
- [ ] Dot remains clickable after collapse
- [ ] State persists across page reload
- [ ] Window position/size persists

## Known Issues

1. **Panel migration not yet designed**
   - BasePanel (slide-in panels) not yet integrated
   - Currently use `onClick` with legacy toggle functions
   - Need to design panel expansion pattern

2. **Symbol palette still present**
   - Top bar palette still exists
   - Creates visual duplication
   - Will be removed after full migration

3. **Window close semantics**
   - Currently close button calls `dot.collapse()`
   - Should some features destroy on close?
   - Need per-feature configuration

## FAQ

**Q: Why is the old WindowTray still there?**
A: Both systems coexist during migration. Once all features use Dots, WindowTray will be removed.

**Q: Do I need to update existing windows?**
A: No. Window class still works. Dots just own Window instances now.

**Q: What about panels (Config, Prose, etc.)?**
A: Panel migration design is TODO. Currently they use legacy toggle functions via onClick.

**Q: Can I have dots that don't expand to windows?**
A: Yes! Use `onClick` instead of `windowConfig` for custom behavior (e.g., focus input).

**Q: How do I disable a dot?**
A: Add `.degraded` class (see VidStream desktop mode example in dot-registry.ts).
