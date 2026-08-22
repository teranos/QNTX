# The Universal Glyph Migration

Where QNTX takes the [glyph primitive](https://github.com/teranos/QNTX/blob/main/packages/glyphs/VISION.md):
everything interactive becomes a glyph, and the system that describes glyphs
becomes attested like everything else in QNTX.

## Attestable glyphs

Glyph state becomes attestation — position, size, manifestation as
provenance-tracked claims:

```
GLYPH-abc123 is expanded at {x:100, y:200, w:400, h:300} by USER-xyz at 2025-01-28T...
GLYPH-def456 is collapsed in GlyphRun by USER-xyz at 2025-01-28T...
```

This gives the interface the same properties attestations give data:
persistent UI state across sessions, shared workspaces, time-travel to how a
workspace looked at any moment, auditable interaction. Multi-device sync
follows from the same mechanism — the [mobile vision](./mobile.md) walks it
through a London tube journey.

Plugins attest new glyphs instead of registering them. The system is
extended by attestation, not by imports.

## Self-describing grammar

Segments, their symbols, and their glyph manifestations are hardcoded
constants today. They become attestations:

```
SEG "ax" is segment of grammar by system at bootstrap
SYM "⋈" is symbol of SEG "ax" by system at bootstrap
GLYPH "ax-glyph" is manifestation of SEG "ax" by system at bootstrap
```

A plugin attests a new segment, gives it a symbol, and defines its
manifestation — the system discovers it through the attestation graph. The
operators that create attestations are themselves attestations: the system
becomes fully self-describing.

## sym under glyph

Symbols are the visual expression of glyphs — through a sym, a glyph is
expressed. The `sym` package moves under `glyph/` (`glyph/sym`), so frontend
and backend share one fundamental primitive.

## The palette becomes the tray

The symbol palette's actions become glyphs in the GlyphRun tray, each with
its own manifestation type. One container, one interaction pattern, one
primitive — instead of windows, dots, symbols, and segments as separate
species.

## Related vision

- [Continuous Intelligence](./continuous-intelligence.md) — the paradigm glyphs manifest
- [Fractal Workspace](./fractal-workspace.md) — nested canvases and glyph manifestations
- [Time-Travel](./time-travel.md) — navigating glyph states across time
