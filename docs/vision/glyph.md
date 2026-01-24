# The True Glyph-Primitive Vision

## Core Concept: The Glyph IS the Window

A single entity that exists in three visual states, morphing between them through smooth animations based on user interaction.

## The Glyph State Continuum

### Core States (Implemented)

#### 1. Collapsed State (8px glyph) ✓

- A tiny gray square sitting quietly in the GlyphRun (middle right of screen by default) ✓
- Minimal visual footprint - just a subtle indicator that something exists ✓
- No text, no decoration - pure simplicity ✓
- Multiple glyphs stack vertically in the GlyphRun ✓

#### 2. Proximity Expanded State (8px → 220px) ✓

- As the mouse approaches, the glyph smoothly morphs larger ✓
- The transformation is proximity-based: closer = larger ✓
- Text fades in showing what the glyph represents (e.g., "VidStream", "Database Statistics") ✓
- Background color transitions from gray to darker as it expands ✓
- The existing window-tray.ts already implements this perfectly (lines 164-285) ✓
- Carefully tuned easing curves, thresholds, and baseline boost logic ✓

#### 3. Full Window State ✓

- **THE KEY MOMENT:** When clicked, the expanded glyph doesn't create a window - it BECOMES the window ✓
- The glyph element itself morphs through animation:
  - Grows from its current size (likely 220px wide after proximity expansion) ✓
  - Moves from tray position to window position ✓
  - Transforms shape from proximate rectangle pill (expanded glyph) to rectangular window ✓
  - Window chrome (title bar, controls) fades in during transformation ✓
  - Content appears as the window reaches full size ✓
- The glyph disappears from the tray because it IS now the window ✓
- This is a smooth, continuous animation where users can see the glyph becoming the window ✓

### The Universal Manifestation Principle

**THE ULTIMATE VISION:** A glyph can manifest into ANY interactive form the user desires. The glyph is not limited to predefined states but can morph into whatever shape, model, or interface is needed:

#### Potential Manifestations

- **Modal dialogs** - Glyph morphs to center screen with backdrop
- **Tooltips** - Glyph becomes floating contextual information
- **Menus** - Glyph expands into dropdown or radial menu
- **Notifications** - Glyph slides in as toast or banner
- **Overlays** - Glyph becomes translucent HUD element
- **Widgets** - Glyph becomes persistent desktop widget
- **Canvas** - Glyph becomes drawable/editable surface
- **Graph nodes** - Glyph becomes part of node-based interface
- **3D objects** - Glyph morphs into spatial/volumetric form
- **Terminal** - Glyph becomes command interface
- **Split views** - Glyph divides into multiple synchronized panes
- **Timelines** - Glyph stretches into temporal visualization
- **Cards** - Glyph becomes stackable/swipeable card interface
- **Floating palette** - Glyph becomes tool palette following cursor

#### The Morphing Grammar

Each manifestation is just another state in the glyph's continuous transformation space. The same DOM element, the same identity, infinite forms:

```
Glyph → [User Intent] → Manifestation
                     ↓
              [Interaction]
                     ↓
            Another Form or Back
```

#### Why This Matters

1. **Infinite Flexibility**: No artificial constraints on what UI can be
2. **Consistent Identity**: The glyph maintains its identity across all forms
3. **Learnable Patterns**: Users learn the morphing grammar, not specific UIs
4. **Future-Proof**: New interface paradigms are just new manifestation types
5. **Conceptual Purity**: ONE primitive, infinite expressions

## Glyph State Persistence

Glyphs are stateful entities that remember their window configuration:
- **Window position**: Where the window was last positioned on screen ✓
- **Window size**: The dimensions the user set by resizing
- **Content state**: Form inputs, scroll position, expanded/collapsed sections
- **View state**: Active tabs, selected items, filter settings

When a window collapses back to a glyph and later re-expands, it restores exactly as the user left it. This reinforces the mental model that the glyph IS the window - it's just temporarily minimized, not destroyed and recreated.

## GlyphRun Positioning

The GlyphRun position is configurable - it can dock to either the left or right side of the screen. By default it appears on the right side, vertically centered. Users can configure it to appear on the left side if that better fits their workflow. The vertical stacking of glyphs remains consistent regardless of which side is chosen.

## The Critical Animation Sequence

### Expand (Glyph → Window)

1. User clicks the proximity-expanded glyph
2. Capture glyph's current position and size
3. Begin transformation animation:
   - Glyph starts growing from its current dimensions
   - Simultaneously moves from tray position toward target window position
   - Border radius animates from rounded to squared
   - Background transitions to window background
   - Title bar and controls fade in
   - Content fades in as window reaches final size
4. Animation duration: ~600ms for smooth but snappy feel
5. Glyph is removed from tray (it has become the window)

### Collapse (Window → Glyph)

1. User clicks minimize button on window
2. Window begins shrinking animation:
   - Window shrinks toward tray position
   - Title bar and controls fade out
   - Content fades out
   - Shape transforms from rectangle back to rounded glyph
   - Color transitions back to gray
3. Window element morphs all the way down to 8px glyph
4. Glyph reappears in tray at the end (the window has become the glyph)

## Why This Vision Matters

### Visual Continuity

Users SEE the transformation. There's no teleportation, no sudden appearance/disappearance. The glyph literally grows into the window and shrinks back. This visual continuity makes the relationship crystal clear.

### Conceptual Simplicity

- No more "windows that minimize to glyphs"
- No more "glyphs that launch windows"
- Just: "glyphs that ARE windows in different states"
- One entity, three visual presentations

### Spatial Memory

Users know where their windows "live" when minimized - they can see them as glyphs. When they expand a glyph, they're not launching something new, they're revealing what was always there.

## Implementation Approach

### Use Existing Infrastructure

- **Keep window-tray.ts proximity morphing** - it's already perfect for states 1 & 2
- **Extend the click handler** - instead of `item.onRestore()`, morph the glyph into window
- **Add to TrayItem interface** - add `renderContent?: () => HTMLElement` for window content

### What Changes in window-tray.ts

1. **Extend TrayItem interface:**

```typescript
export interface TrayItem {
    id: string;
    title: string;
    onRestore?: (sourceRect?: DOMRect) => void;  // Keep for backward compat
    renderContent?: () => HTMLElement;            // NEW: For glyph-primitive
    initialWidth?: string;                        // NEW: Window dimensions
    initialHeight?: string;
    defaultX?: number;                            // NEW: Window position
    defaultY?: number;
}
```

2. **Modify click handler (around line 371):**

- Check if item has `renderContent` (new glyph-primitive path)
- If yes: morph glyph into window
- If no: use existing `onRestore` (backward compatible)

3. **Add morphing methods:**

- `morphToWindow(glyph, item)` - handles expansion animation
- `morphToGlyph(window, item)` - handles collapse animation

### CSS for Smooth Animations

```css
.glyph-morphing-to-window {
    transition: all 600ms cubic-bezier(0.4, 0, 0.2, 1);
    transition-property: transform, width, height, border-radius, background-color, opacity;
}

.window-morphing-to-glyph {
    transition: all 600ms cubic-bezier(0.4, 0, 0.2, 1);
    transition-property: transform, width, height, border-radius, background-color, opacity;
}
```

## What We're NOT Doing

### NOT Creating Separate Systems

- Not making a new type of glyph
- Not creating a parallel window system
- Not duplicating the proximity morphing logic

### NOT Breaking Existing Functionality

- Existing minimized windows still work with `onRestore`
- Window component remains for apps that need it
- Backward compatibility maintained

### NOT Overengineering

- Simple CSS transitions for animations
- Reuse existing DOM elements where possible
- Minimal new code - mostly extending what exists

## Success Criteria

1. **Visual**: The transformation is smooth and obvious - users can clearly see the glyph becoming the window
2. **Conceptual**: Users understand glyphs and windows are the same entity in different states
3. **Technical**: Minimal code changes, maximum reuse of existing infrastructure
4. **Performance**: Animations are smooth at 60fps
5. **Compatibility**: Existing window minimize/restore still works

## Test Scenarios

1. **Basic Morph**: Click glyph → morphs to window → click minimize → morphs back to glyph
2. **Proximity During Morph**: Mouse near tray during collapse animation shouldn't interfere
3. **Multiple Glyphs**: Multiple glyphs can be expanded simultaneously
4. **State Persistence**: Window positions saved, glyphs reappear in tray after refresh
5. **Dragging**: Windows are draggable, position updates saved

## The End Goal

When complete, users will experience a seamless transformation where glyphs in the tray literally grow into windows and shrink back. The animation makes the relationship unmistakable - the glyph IS the window, just in different visual states based on user needs.

## Future: The Universal Glyph Migration

### Replacing Seg and Sym

The backend will eventually migrate from `seg` (Segment) and `sym` (Symbol) to use `glyph` throughout:

- The `sym` package will become the `glyph` package
- All references to segments and symbols will become glyphs
- Frontend and backend will share the same fundamental primitive: **Glyph**
- Complete coherence between frontend visualization and backend data model

### GlyphRun Replacing Symbol Palette

The current Symbol Palette will be absorbed into the GlyphRun:

- Symbols from the palette become glyphs in the run
- The same proximity morphing applies to all glyphs
- Clicking a symbol glyph could:
  - Morph into a window showing symbol details
  - Execute a command (like current palette)
  - Transform into a different visualization mode
- The GlyphRun becomes the universal container for all interactive visual elements

### Why This Unification Matters

1. **Conceptual Clarity**: One primitive (Glyph) instead of windows, dots, symbols, segments
2. **Visual Consistency**: Everything uses the same morph animations and proximity behaviors
3. **Backend-Frontend Alignment**: The same term and concept throughout the entire stack
4. **User Mental Model**: Users learn ONE interaction pattern that applies everywhere
5. **Future Extensibility**: New features become new glyph types with consistent behavior

### The Vision: Glyph as Universal Primitive

Glyphs are the atoms of the QNTX interface. Every visual element that can transform, morph, or contain information is a glyph. They can be:

- **Windows** (when expanded to full state)
- **Symbols** (when representing semantic concepts)
- **Commands** (when executing actions)
- **Visualizations** (when showing data)
- **Containers** (when holding other glyphs)

All sharing the same fundamental behavior: proximity morphing, smooth transformations, and the ability to exist in multiple visual states.

