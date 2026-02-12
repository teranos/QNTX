# Fractal Workspace - Vision

**Status:** Active - Nested canvas glyphs implemented in [#461](https://github.com/teranos/QNTX/pull/461), ongoing refinement

**Implementation Path:** Workspaces are composed of [glyphs](./glyphs.md) with [persistence and sync](../plans/glyph-persistence-visual-sync.md). Nested canvas glyphs (⧉) create fractal workspaces - canvases within canvases. Users navigate by entering nested canvases.

## Concepts

Transform workspace navigation from **flat graph** to **fractal canvases** - a paradigm shift where the main canvas contains nested canvas glyphs that users enter to access deeper workspaces. Glyphs are **persistent information surfaces** displaying contextual data without interaction, embodying the [continuous intelligence](./continuous-intelligence.md) paradigm.

### 1. Navigation Modes

**Zoom-based glyph manifestations:**
- Glyphs progressively reveal detail as you zoom in
- Each glyph type defines its own manifestations (e.g., chart-glyph: mini → zoomed → fullscreen)
- Figma-like zoom/pan for spatial navigation
- Especially important on mobile with pinch-to-zoom

**Meld composition navigation (mobile):**
- When in fullscreen glyph manifestation, drag down from top to navigate to connected glyph above
- Navigate through melded compositions fluidly
- Access related glyphs without exiting to canvas view

**Hierarchical canvas navigation:**
- Tap/click nested canvas glyph to enter that workspace
- Glyphs and compositions exist within canvases
- Nested canvases create fractal workspace hierarchy
- Exit to return to parent canvas

**Philosophy:** Multiple complementary navigation patterns. Zoom reveals detail. Meld navigation follows connections. Canvas nesting organizes workspaces.

### 2. Compositional Computing

Inspired by Smalltalk/Pharo's pane model - glyphs are **compositional surfaces** that can be:
- Arranged on canvases (grid layout)
- Organized hierarchically (nested canvas glyphs)
- Connected through time ([time-travel](./time-travel.md))
- Melded into compositions ([glyph-melding](./glyph-melding.md))

### 3. Rich Data Display

Glyphs show contextual data on their surface. Each glyph type determines what information to display based on the entity it represents. Plugins that attest new glyph types define how those glyphs render their data.

## Design Goals

### Visual Hierarchy
- **Type → Label → Fields → Detail → Context**
- Each zoom level serves distinct use case
- Clear visual differentiation between levels

### Layout Flexibility
- **Layout Modes:**
  - Grid: Alphabetical/chronological arrangement
  - Hierarchy: Parent-child tree structure
  - Timeline: Temporal progression
  - Pipeline: Workflow stages
  - Graph: Force-directed relationships (current default)

### Always-On Data
- Most important fields visible without interaction
- Hover/click for supplementary actions, not primary data
- Glyph surface = **first-class information display**

## Implementation

Glyphs are the universal primitive ([glyphs.md](./glyphs.md)):

- Glyphs manifest differently at different zoom levels
- Glyphs are attestable (plugins attest new glyph types)
- Glyph state is attested (positions, sizes, manifestations persist)
- Nested canvas glyphs create fractal workspaces
- Meld compositions connect glyphs

### Implementation Considerations

- Responsive to different screen sizes
- Smooth transitions between zoom levels (glyph morphing)
- Glyph size adapts to content

## Mobile-First Considerations

See [mobile.md](./mobile.md) for detailed mobile UX vision and implementation status.

**Deep Exploratory Analysis** (30+ min sessions on mobile):
- **Pinch-to-zoom:** Glyphs reveal progressive detail as you zoom in
- **Glyph manifestations:** Mini → zoomed → fullscreen based on zoom level
- **Meld navigation:** Drag down from top when in fullscreen glyph to navigate to connected glyph above
- **Canvas navigation:** Tap nested canvas glyph to enter, back gesture to exit
- **Landscape enhancement:** Wider glyphs show more detail when horizontal

**Gesture Mapping:**
- Pinch in/out → Zoom reveals glyph manifestations
- Tap nested canvas → Enter that workspace
- Drag from top (fullscreen) → Navigate meld composition
- Back gesture → Exit nested canvas

**Key Design Principle:** Mobile is a primary exploratory interface. Desktop adds power-user features.
