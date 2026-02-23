# Plugin Custom UI

How plugins extend the QNTX frontend with custom glyph types, rendering, and real-time updates.

## Problem

Plugins are process-isolated gRPC services. They can register HTTP endpoints (`/api/{plugin}/*`) and WebSocket handlers, but the frontend has no mechanism for plugins to:

- Register custom glyph types in the glyph registry
- Deliver frontend code (JS/CSS) to the browser
- Route WebSocket messages to plugin-specific handlers
- Inject rendering logic for custom glyph content

All glyph types are hardcoded in `glyph-registry.ts` at build time. A plugin like `qntx-code` that wants a "Go Editor" glyph or a biotech plugin that wants a "Protein Viewer" glyph has no extension point.

## Current State

### What Plugins Can Do

- **HTTP API**: Register handlers at `/api/{plugin}/*` via `RegisterHTTP(mux)` (routing.go:29-43)
- **WebSocket**: Register handlers via `RegisterWebSocket()` (routing.go:48-71)
- **Config UI**: Expose `ConfigSchema()` for auto-generated config forms (plugin/interface.go:116-139)
- **Health**: Report status via `Health()`, surfaced in plugin panel

### What's Missing

| Capability | Status |
|---|---|
| Custom glyph types | No registration mechanism |
| Plugin frontend code delivery | No asset serving |
| Plugin WebSocket message routing | Hardcoded in websocket-handlers/ |
| Dynamic glyph content rendering | All renderers compiled into bundle |
| Symbol palette extension | Hardcoded in sym package |

## Design

### Principle: HTML Over JS

Plugins render their glyph content server-side as HTML fragments. The frontend fetches and mounts the HTML into the glyph's content area. No plugin JavaScript runs in the browser.

Why:
- **Security**: No arbitrary JS execution from plugin processes
- **Simplicity**: Plugins use their own language's templating (Go `html/template`, Python Jinja, etc.)
- **Isolation**: Plugin rendering bugs can't crash the frontend
- **Manifestation axiom**: The glyph DOM element is still created by `createGlyphElement` — only the content inside it comes from the plugin

This is the same pattern as htmx: server-rendered HTML fragments, swapped into the DOM.

### Extension Point: `RegisterGlyphs`

Add a new optional interface to the plugin contract:

```go
// UIPlugin is an optional interface for plugins that provide custom glyph types.
type UIPlugin interface {
    DomainPlugin

    // RegisterGlyphs returns glyph type definitions this plugin provides.
    // Each definition includes: symbol, title, label, and the HTTP path
    // that renders the glyph's HTML content.
    RegisterGlyphs() []GlyphDef
}

type GlyphDef struct {
    // Symbol is the glyph identifier (e.g., "⚗" for a chemistry plugin).
    // Must not collide with built-in symbols from sym package.
    Symbol string

    // Title is the human-readable name shown in the title bar.
    Title string

    // Label is a short identifier for logs and the spawn menu.
    Label string

    // ContentPath is the HTTP path (relative to /api/{plugin}/) that
    // returns the HTML fragment for this glyph's content area.
    // The frontend GETs this path with ?glyph_id={id}&content={encoded}
    // and mounts the response HTML into the glyph element.
    ContentPath string

    // CSS is an optional HTTP path to a stylesheet for this glyph type.
    // Loaded once when the first glyph of this type is created.
    CSSPath string

    // DefaultWidth and DefaultHeight in pixels. 0 = use system default.
    DefaultWidth  int
    DefaultHeight int
}
```

### gRPC Extension

Add to `domain.proto`:

```protobuf
message GlyphDefResponse {
    repeated GlyphDef glyphs = 1;
}

message GlyphDef {
    string symbol = 1;
    string title = 2;
    string label = 3;
    string content_path = 4;
    string css_path = 5;
    int32 default_width = 6;
    int32 default_height = 7;
}

service DomainPluginService {
    // ... existing RPCs ...
    rpc RegisterGlyphs(Empty) returns (GlyphDefResponse);
}
```

### Frontend Discovery

On plugin load, QNTX core calls `RegisterGlyphs()` and caches the results. The frontend fetches all plugin glyph definitions via a new endpoint:

```
GET /api/plugins/glyphs
```

Returns:

```json
[
    {
        "plugin": "biotech",
        "symbol": "⚗",
        "title": "Protein Viewer",
        "label": "Protein",
        "content_url": "/api/biotech/glyphs/protein/content",
        "css_url": "/api/biotech/glyphs/protein/style.css",
        "default_width": 600,
        "default_height": 400
    }
]
```

### Frontend Integration

#### 1. Plugin Glyph Registry

At startup, fetch plugin glyph definitions and register them alongside built-in types:

```typescript
// plugin-glyphs.ts

interface PluginGlyphDef {
    plugin: string;
    symbol: string;
    title: string;
    label: string;
    content_url: string;
    css_url?: string;
    default_width?: number;
    default_height?: number;
}

/** Fetch plugin glyph defs and register them in the glyph registry. */
export async function loadPluginGlyphs(): Promise<void> {
    const resp = await apiFetch('/api/plugins/glyphs');
    if (!resp.ok) return; // No plugins or no glyphs — fine

    const defs: PluginGlyphDef[] = await resp.json();
    for (const def of defs) {
        registerPluginGlyphType(def);
    }
}
```

#### 2. Plugin Content Renderer

A single generic renderer handles all plugin glyphs by fetching HTML from the plugin's content endpoint:

```typescript
function createPluginGlyphRenderer(def: PluginGlyphDef) {
    return async (glyph: Glyph): Promise<HTMLElement> => {
        const container = document.createElement('div');
        container.className = `plugin-glyph-content plugin-${def.plugin}`;

        const params = new URLSearchParams({
            glyph_id: glyph.id,
            content: glyph.content || '',
        });

        const resp = await fetch(`${def.content_url}?${params}`);
        if (!resp.ok) {
            container.textContent = `Failed to load ${def.title} content`;
            return container;
        }

        container.innerHTML = resp.text();
        return container;
    };
}
```

#### 3. CSS Loading

Plugin CSS is loaded once per glyph type, on first instantiation:

```typescript
const loadedCSS = new Set<string>();

function loadPluginCSS(url: string): void {
    if (loadedCSS.has(url)) return;
    loadedCSS.add(url);

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = url;
    document.head.appendChild(link);
}
```

#### 4. Glyph Registry Extension

Extend `glyph-registry.ts` to accept dynamic registrations:

```typescript
/** Register a plugin-provided glyph type at runtime. */
export function registerGlyphType(entry: GlyphTypeEntry): void {
    if (_bySymbol.has(entry.symbol)) {
        console.warn(`Glyph type for symbol ${entry.symbol} already registered, skipping`);
        return;
    }
    GLYPH_TYPES.push(entry);
    _bySymbol.set(entry.symbol, entry);
    _byClassName.set(entry.className, entry);
}
```

### Real-Time Updates via WebSocket

Plugins that need live updates in their glyphs use the existing watcher system. The flow:

1. Plugin glyph creates a watcher on mount (POST `/api/watchers`)
2. Backend routes attestation matches via existing `watcher_match` WebSocket message
3. Frontend routes the match to the glyph by `watcher_id` prefix
4. Glyph re-fetches its HTML content from the plugin

For plugin-specific routing, extend the `watcher_match` handler to recognize plugin glyph prefixes:

```typescript
// In websocket-handlers, after existing ax-glyph/se-glyph routing:
if (data.watcher_id?.startsWith('plugin-glyph-')) {
    const glyphId = data.watcher_id.substring('plugin-glyph-'.length);
    refreshPluginGlyphContent(glyphId);
}
```

Where `refreshPluginGlyphContent` re-fetches the HTML from the plugin's content endpoint with updated state.

### Interactive Content: Plugin WebSocket

For glyphs that need bidirectional real-time communication (e.g., a terminal, a language server editor), the plugin registers a WebSocket handler and the glyph connects directly:

```
Plugin registers: /gopls WebSocket handler
Glyph connects:   ws://host/gopls
```

This already works today (qntx-code uses it for gopls). The custom UI mechanism just needs to let the glyph know which WebSocket path to connect to. Add an optional field to `GlyphDef`:

```go
type GlyphDef struct {
    // ... existing fields ...

    // WSPath is an optional WebSocket path for bidirectional communication.
    // The frontend connects to this WebSocket when the glyph is opened.
    WSPath string
}
```

## Plugin Implementation Example

A biotech plugin providing a protein viewer glyph:

```go
func (p *BiotechPlugin) RegisterGlyphs() []plugin.GlyphDef {
    return []plugin.GlyphDef{
        {
            Symbol:      "⚗",
            Title:       "Protein Viewer",
            Label:       "Protein",
            ContentPath: "/glyphs/protein/content",
            CSSPath:     "/glyphs/protein/style.css",
            DefaultWidth: 600,
            DefaultHeight: 400,
        },
    }
}

func (p *BiotechPlugin) RegisterHTTP(mux *http.ServeMux) error {
    // Glyph content endpoint — returns HTML fragment
    mux.HandleFunc("/glyphs/protein/content", p.handleProteinContent)

    // Glyph CSS endpoint
    mux.HandleFunc("/glyphs/protein/style.css", p.handleProteinCSS)

    // Existing API endpoints
    mux.HandleFunc("/sequences", p.handleSequences)
    return nil
}

func (p *BiotechPlugin) handleProteinContent(w http.ResponseWriter, r *http.Request) {
    glyphID := r.URL.Query().Get("glyph_id")
    content := r.URL.Query().Get("content")

    // Parse content (e.g., PDB ID stored in glyph.content)
    pdbID := content
    if pdbID == "" {
        pdbID = "1crn" // default
    }

    // Render HTML fragment
    w.Header().Set("Content-Type", "text/html")
    fmt.Fprintf(w, `
        <div class="protein-viewer" data-pdb="%s">
            <div class="protein-3d-canvas" id="pv-%s"></div>
            <div class="protein-info">
                <span class="protein-id">%s</span>
            </div>
        </div>
    `, pdbID, glyphID, pdbID)
}
```

## Spawn Menu Integration

The canvas spawn menu (right-click → "Add glyph") currently shows built-in types. Plugin glyphs appear in the same menu, grouped under the plugin name:

```
┌─────────────────┐
│ ⋈ AX Query      │
│ ⊨ Semantic      │
│ py Python       │
│ ─────────────── │
│ biotech         │
│   ⚗ Protein     │
│   🧬 Sequence   │
│ finance         │
│   📊 Portfolio  │
└─────────────────┘
```

The frontend builds this from the merged built-in + plugin glyph registries.

## Constraints

**No plugin JS in the browser.** All interactivity beyond what HTML/CSS provides must go through:
- Re-fetching HTML content (polling or watcher-triggered)
- WebSocket connections to plugin endpoints
- Form submissions to plugin HTTP endpoints

If a plugin needs complex client-side interactivity (3D rendering, code editors), it has two paths:
1. Use a WASM module served as a static asset via `CSSPath`/additional asset paths
2. Propose a new built-in glyph type that wraps the capability (e.g., CodeMirror is built-in, not plugin-specific)

**Manifestation axiom holds.** The glyph DOM element is created by `createGlyphElement` in `run.ts`. Plugin content is mounted inside the glyph's content area, not as a replacement for the glyph element itself.

**No symbol collisions.** Plugin symbols must not collide with built-in symbols from the `sym` package. The registry rejects duplicates.

## Implementation Order

### Phase 1: Backend

1. Add `UIPlugin` interface to `plugin/interface.go`
2. Add `GlyphDef` message to `domain.proto`
3. Add `RegisterGlyphs` RPC to `DomainPluginService`
4. Wire `RegisterGlyphs` through `ExternalDomainProxy` and `PluginServer`
5. Add `GET /api/plugins/glyphs` endpoint to `server/routing.go`
6. Cache glyph defs in plugin registry

### Phase 2: Frontend

1. Add `registerGlyphType()` to `glyph-registry.ts`
2. Create `plugin-glyphs.ts` with `loadPluginGlyphs()`
3. Implement plugin content renderer (HTML fetch + mount)
4. Add CSS lazy-loading
5. Call `loadPluginGlyphs()` at app startup (after plugin panel init)

### Phase 3: Spawn Menu + Watcher Routing

1. Extend spawn menu to show plugin glyph types
2. Add `plugin-glyph-` prefix routing in `watcher_match` handler
3. Implement `refreshPluginGlyphContent()` for live updates

### Phase 4: Reference Implementation

1. Add a glyph to `qntx-code` (e.g., file browser glyph)
2. Verify full lifecycle: spawn → render → update → minimize → restore

## Related

- [ADR-001: Domain Plugin Architecture](../adr/ADR-001-domain-plugin-architecture.md) — plugin isolation model
- [External Plugin Guide](./external-plugin-guide.md) — building plugins
- [Glyph Vision](../vision/glyphs.md) — glyph-as-universal-primitive
- [Domain Plugin API Reference](./domain-plugin-api-reference.md) — existing plugin interface
