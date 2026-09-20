# Plugin Custom UI

How plugins extend the QNTX frontend with custom element types, rendering, and real-time updates.

## Problem

Plugins are process-isolated gRPC services. They can register HTTP endpoints (`/api/{plugin}/*`) and WebSocket handlers, but the frontend has no mechanism for plugins to:

- Register custom element types in the element registry
- Deliver frontend code (JS/CSS) to the browser
- Route WebSocket messages to plugin-specific handlers
- Inject rendering logic for custom element content

All element types are hardcoded in `element-registry.ts` at build time. A plugin like `qntx-code` that wants a "Go Editor" element or a biotech plugin that wants a "Protein Viewer" element has no extension point.

## Current Implementation

AT Protocol plugin (`qntx-atproto`) provides a feed element using server-rendered HTML fragments. The plugin serves HTML via HTTP endpoint, frontend fetches and mounts it into the element content area.

Plugins implement the [`UIPlugin`](https://github.com/teranos/QNTX/blob/main/plugin/interface.go) interface which returns `ElementDef` structs defining custom element types.

## Example

AT Protocol plugin ([`qntx-atproto`](https://github.com/teranos/QNTX/tree/main/qntx-atproto)) provides a feed element (🦋) showing Bluesky posts.

## TODO

- Panel manifestation: plugins should be able to open persistent panels (sidebar, bottom dock)

## Status

Python plugin has not been migrated to custom UI yet.

## Links

- [ADR-001: Domain Plugin Architecture](../adr/ADR-001-domain-plugin-architecture.md) - plugin isolation model
- [Element Migration](../vision/element-migration.md) - future direction (attested elements, grammar)
- [ADR-001: Domain Plugin Architecture](../adr/ADR-001-domain-plugin-architecture.md) - building plugins
