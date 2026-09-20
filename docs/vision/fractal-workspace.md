# Fractal Workspace - Vision

**Status:** Active - Nested canvas elements implemented in [#461](https://github.com/teranos/QNTX/pull/461), ongoing refinement

**Implementation Path:** Workspaces are composed of [elements](https://github.com/teranos/elements/blob/main/VISION.md) with [persistence and sync](../plans/element-persistence-visual-sync.md). Nested canvas elements (⧉) create fractal workspaces - canvases within canvases. Users navigate by entering nested canvases.

## Concepts

Transform workspace navigation from **flat graph** to **fractal canvases** - a paradigm shift where the main canvas contains nested canvas elements that users enter to access deeper workspaces. Elements are **persistent information surfaces** displaying contextual data without interaction, embodying the [continuous intelligence](./continuous-intelligence.md) paradigm.

### 1. Navigation Modes

**Zoom-based element manifestations:**
- Elements progressively reveal detail as you zoom in
- Each element type defines its own manifestations (e.g., chart-element: mini → zoomed → fullscreen)
- Figma-like zoom/pan for spatial navigation
- Especially important on mobile with pinch-to-zoom

**Meld composition navigation (mobile):**
- When in fullscreen element manifestation, drag down from top to navigate to connected element above
- Navigate through melded compositions fluidly
- Access related elements without exiting to canvas view

**Hierarchical canvas navigation:**
- Tap/click nested canvas element to enter that workspace
- Elements and compositions exist within canvases
- Nested canvases create fractal workspace hierarchy
- Exit to return to parent canvas

**Philosophy:** Multiple complementary navigation patterns. Zoom reveals detail. Meld navigation follows connections. Canvas nesting organizes workspaces.

### 2. Compositional Computing

Inspired by Smalltalk/Pharo's pane model - elements are **compositional surfaces** that can be:
- Arranged on canvases (grid layout)
- Organized hierarchically (nested canvas elements)
- Connected through time ([time-travel](./time-travel.md))
- Melded into compositions ([melding](https://github.com/teranos/elements/blob/main/VISION.md#melding))

### 3. Rich Data Display

Elements show contextual data on their surface. Each element type determines what information to display based on the entity it represents. Plugins that attest new element types define how those elements render their data.

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
- Element surface = **first-class information display**

## Implementation

Elements are the universal primitive ([VISION.md](https://github.com/teranos/elements/blob/main/VISION.md)):

- Elements manifest differently at different zoom levels
- Elements are attestable (plugins attest new element types)
- Element state is attested (positions, sizes, manifestations persist)
- Nested canvas elements create fractal workspaces
- Meld compositions connect elements

### Implementation Considerations

- Responsive to different screen sizes
- Smooth transitions between zoom levels (element morphing)
- Element size adapts to content

## Mobile-First Considerations

See the [App's vision](https://github.com/teranos/QNTX-App/blob/main/VISION.md) for detailed mobile UX vision and implementation status.

**Deep Exploratory Analysis** (30+ min sessions on mobile):
- **Pinch-to-zoom:** Elements reveal progressive detail as you zoom in
- **Element manifestations:** Mini → zoomed → fullscreen based on zoom level
- **Meld navigation:** Drag down from top when in fullscreen element to navigate to connected element above
- **Canvas navigation:** Tap nested canvas element to enter, back gesture to exit
- **Landscape enhancement:** Wider elements show more detail when horizontal

**Gesture Mapping:**
- Pinch in/out → Zoom reveals element manifestations
- Tap nested canvas → Enter that workspace
- Drag from top (fullscreen) → Navigate meld composition
- Back gesture → Exit nested canvas

**Key Design Principle:** Mobile is a primary exploratory interface. Desktop adds power-user features.
