# Focus

Focus is the primary mobile interface for glyphs. On narrow screens, the canvas is the composition — you navigate it by scrolling and pivoting, not by zooming and panning a map.

Double-click a glyph to focus it. The canvas zooms to 100% and pans to center the glyph. The focused glyph's vertical chain stacks top-to-bottom in the center column — a thread you scroll through. Sibling columns show horizontal neighbors, each with their own thread. Viewport width determines how many columns are visible. The split is a window into the composition — if there are more members than the split shows, scroll to bring them into view. Escape unfocuses: glyphs transform back, zoom restores to its previous level, pan stays where you are.

A composition is a DAG with vertical edges (bottom) and horizontal edges (right).

Scroll direction maps to meld direction — left/right for horizontal melds, up/down for vertical. The layout reshapes continuously with scroll.

## Implementation

### Part 1: Thread layout

Focus a glyph — its vertical chain (bottom edges, root to leaf) stacks in the center column. The chain is walked from the composition's edges. Each glyph sized to its content. Prompt at top, results below. Scroll navigates the thread.

### Part 2: Sibling columns

Horizontal neighbors appear in flanking columns, each with their own vertical thread. Center column ~20% wider.

### Part 3: Pivoting

Focusing any glyph — including a sibling — pivots the view. That glyph's vertical chain becomes the center thread.

### Part 4: Scroll

Vertical scroll navigates the thread. Horizontal scroll pivots to the neighboring glyph — the entire view rebuilds around the new focus. Click a sibling to pivot into it.

### Part 5: Unfocus

Escape unfocuses. Glyphs transform back, zoom restores. More unfocus triggers to be determined.

### Part 6: Persistence

Focus state survives sessions. When you return, you're where you left off.
