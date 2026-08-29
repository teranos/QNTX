# AXIOMAS

## Element Axioma

A glyph is exactly one DOM element for its entire lifetime. Glyphs are reparented, never cloned. Everything about it survives every transition between manifestations.

## Morph Axioma

A morph is a state transition of a glyph between manifestations, ending one of two ways: the glyph takes the new state, or the attempt is abandoned and it keeps the one it had.

A glyph is in one transition at any time.

## One-Per-Side Axioma

Each side of a glyph accepts at most one meld connection.

## Containment Axioma

A glyph never hides its own content. What it holds is either inside its box or
reachable by scrolling, and clipping is neither — the data is still there and
there is no way to get to it.

A glyph that cannot fit what it holds says so by scrolling. It does not shrink
the content, truncate it, or let it run under its own edge.
