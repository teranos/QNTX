# Focus

Double-click a glyph to focus it. The glyph's DOM element transforms while the canvas zooms and pans to center it. Viewport width determines the split — how many glyphs are visible at once in equal columns: 2 at 50%, 3 at 33%, 4 at 25%, 5 at 20%. Portrait mobile: one glyph fills the screen. Landscape mobile: 50/50 split, same as narrow desktop. On mobile portrait, the single column stacks vertically — a result glyph with its follow-ups forms a conversation chain you scroll through. The split is a window into the meld — if a meld has more members than the split shows, scroll to bring adjacent glyphs into view. Escape unfocuses: the glyph transforms back down, zoom returns to its previous level, pan stays where you are.

Scroll direction maps to meld direction — left/right for horizontal melds, up/down for vertical. The layout reshapes continuously with scroll.

## Implementation

### Part 1: Foundation

Glyph transform + canvas zoom/pan. Double-click transforms the glyph element, canvas zooms and pans to center it. Escape reverses both.

### Part 2: Meld split

Viewport width determines equal-column splits. Focused glyph's meld neighbors fill adjacent columns.

### Part 3: Meld navigation

Scroll through meld relationships. The layout reshapes continuously with scroll movement.
