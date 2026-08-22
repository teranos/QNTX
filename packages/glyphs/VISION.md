# Glyphs

The glyph is the universal UI primitive: exactly one DOM element for its
entire lifetime, morphing between manifestations while its identity never
changes. The axioms are in [AXIOMAS.md](AXIOMAS.md); this is why they exist.

## The glyph IS the window

A window is not launched from a glyph, and a glyph is not an icon for a
window. The element resting as a dot in the tray is the element that becomes
the window: it grows out of its place, takes chrome, and is the window until
it shrinks back. Nothing is created, nothing is destroyed, nothing teleports.
The user watches one thing change state, and the visual continuity makes the
relationship unmistakable.

## The state continuum

At rest a glyph is a dot — presence without demand. As the pointer
approaches it grows, and its symbol and title fade in: attention is answered
before commitment. Interaction commits it to a manifestation — a window with
chrome, a fullscreen panel, a canvas, an element placed on a workspace.
Leaving a manifestation is the same road driven backwards, down to the dot
it never stopped being.

A morph between states is a transaction. It ends one of two ways: the glyph
takes the new state, or the attempt is abandoned and the glyph keeps the one
it had. Nothing about a morph outlives the morph.

## Universal manifestation

A manifestation is a state, not a component. The same element, the same
identity, another form — and any form an interface needs tomorrow is a new
manifestation type, not a new primitive.

```
glyph → [intent] → manifestation → [interaction] → another form, or back
```

Users learn the morphing grammar once, not each UI.

## Visual identity

What a glyph wears is data on the glyph, never a property of a
manifestation: its symbol, its color, its border. Every manifestation reads
them and every manifestation shows them — the dot a note minimizes into
wears the note's border. Everything about a glyph survives every transition.

## Memory

A glyph remembers: where its window last stood, what its content held, where
it sits on a canvas. Expanding a glyph reveals what was always there;
nothing is reconstructed. The user knows where a thing lives because it
never left.

## The tray

The tray is where glyphs rest. It holds every minimized glyph as a dot and
answers proximity — pointer and thumb alike. The tray is the continuum made
visible: a screen of windows and a tray of dots are one population in
different states.

## Melding

Glyphs compose by touch. Dragged close enough, they fuse into spatial
compositions with typed, directed edges — data flows along the geometry the
user built by hand. Each side of a glyph accepts one connection; the
composition is a graph the user can see because it is the layout itself.

## The host

A host expresses itself through glyphs — its symbols, its panels, its
grammar. What a host builds on top of the primitive (QNTX: attested glyph
state, a self-describing grammar) is the host's vision. Where the primitive
ends and a host begins is the Boundary section of the [README](README.md).
