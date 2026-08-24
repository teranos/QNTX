# The Glyph Migration

The glyph runtime lives in [packages/glyphs](https://github.com/teranos/QNTX/blob/main/packages/glyphs/VISION.md). Two migration steps are already law in [CLAUDE.md](https://github.com/teranos/QNTX/blob/main/CLAUDE.md): the `sym` package becomes `glyph/sym`, and the symbol palette's actions become glyphs in the GlyphRun tray. This doc holds the step after those, which has not started.

## Glyph state becomes attestation

A glyph's state — where it sits, what size it holds, which manifestation it wears — is a claim about the workspace, and QNTX already has a way to hold claims: attestations, with provenance.

```
GLYPH-abc123 is expanded at {x:100, y:200, w:400, h:300} by USER-xyz at 2025-01-28T...
```

Everything attestations give data would then apply to the interface itself. A workspace survives sessions because its state is attested. It can be shared, because attestations can be read by more than one device. It can be replayed to how it looked at any moment ([time-travel](./time-travel.md)), and it syncs across devices by the same mechanism ([mobile](./mobile.md) walks this through a tube journey).

The same move applies one level down. Segments, their symbols, and their manifestations are hardcoded constants today. As attestations, a plugin could attest a new segment, give it a symbol, and define its manifestation — the system would discover it through the attestation graph instead of through imports ([plugin custom UI](../architecture/plugin-custom-ui.md)).
