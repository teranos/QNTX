# Plugin Custom UI

How plugins extend the QNTX frontend with custom glyph types, rendering, and real-time updates.

## Problem

Plugins are process-isolated gRPC services. They can register HTTP endpoints (`/api/{plugin}/*`) and WebSocket handlers, but the frontend has no mechanism for plugins to:

- Register custom glyph types in the glyph registry
- Deliver frontend code (JS/CSS) to the browser
- Route WebSocket messages to plugin-specific handlers
- Inject rendering logic for custom glyph content

All glyph types are hardcoded in `glyph-registry.ts` at build time. A plugin like `qntx-code` that wants a "Go Editor" glyph or a biotech plugin that wants a "Protein Viewer" glyph has no extension point.

## Current Implementation

AT Protocol plugin (`qntx-atproto`) provides a feed glyph using server-rendered HTML fragments. The plugin serves HTML via HTTP endpoint, frontend fetches and mounts it into the glyph content area.

Plugins implement the [`UIPlugin`](https://github.com/teranos/QNTX/blob/main/plugin/interface.go) interface which returns `GlyphDef` structs defining custom glyph types.

## Example

AT Protocol plugin ([`qntx-atproto`](https://github.com/teranos/QNTX/tree/main/qntx-atproto)) provides a feed glyph (🦋) showing Bluesky posts.

## Status

Python plugin has not been migrated to custom UI yet.

## Links

- [ADR-001: Domain Plugin Architecture](../adr/ADR-001-domain-plugin-architecture.md) - plugin isolation model
- [Glyph Vision](../vision/glyphs.md) - future direction (attested glyphs, grammar)
- [External Plugin Guide](../development/external-plugin-guide.md) - building plugins
