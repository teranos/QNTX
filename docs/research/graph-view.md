# Graph View — Research

Exploring whether a WebGL graph visualization (regl) adds value for understanding attestation relationships. Force-directed layout with typed node/edge coloring. Open question: does this show structure that list views (ax/se glyphs) can't?

## Friction

- The graph system requires explicitly attested types and relationships to produce meaningful structure. This manual overhead undermines the value proposition — if you have to tell the system what everything is before it can show you anything, the visualization isn't revealing much.
- As a standalone spawnable glyph it feels disconnected. Would need to be embedded into ax/se results to be contextually useful.

## Technical limitations

- **No labels** — colored dots without identity. regl has no text; needs HTML overlay or MSDF.
- **Polling, not live** — 2s poll. WebSocket graph broadcast lacks `type` field so routeMessage() drops it.
- **Dense center** — disconnected components pile up due to gravity. Needs quadtree repulsion for 1000+ nodes.
