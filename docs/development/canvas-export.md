# Canvas Export - Evolution and Architecture

This document traces the evolution of canvas export functionality in QNTX, from the original use case to the current implementation and future direction.

---

## Original Use Case: Bluesky Posts with Provenance

**Problem:** QNTX learns from user patterns and generates LLM-augmented Bluesky posts. These posts should include a link to "the canvas as it was at that moment" for transparency and provenance.

**Requirements:**
- Server-initiated (no user interaction)
- Captures canvas state at posting time
- Permanent, immutable snapshot (IPFS)
- Verifiable (this is what QNTX was "seeing" when it generated the post)

**Example flow:**
```
QNTX detects pattern in user's work
  ↓
LLM generates insight: "You're building a recursive type system"
  ↓
Server creates Bluesky post:
  "You're building a recursive type system 🧠
   Canvas snapshot: https://ipfs.io/ipfs/Qm..."
  ↓
Link shows frozen canvas state - notes, code, connections
```

This requires **server-side rendering** because the server is creating the post.

---

## Attempt 1: Server-Side HTML Reconstruction

**Branch:** `claude/canvas-static-export-hjzI5` (PR #600)

**Approach:**
- Server queries glyphs from database
- Go code reconstructs canvas HTML
- Duplicates rendering logic from TypeScript
- Publishes to IPFS and git

**Files:**
- `glyph/handlers/canvas_export.go` - Go HTML builder
- `glyph/handlers/canvas_publish.go` - IPFS + git publishing

**Problems:**
1. **Duplication:** Canvas rendering logic exists in both TS and Go
2. **Divergence:** Two implementations drift over time
3. **Maintenance:** Every canvas feature needs two implementations
4. **Fragility:** Go version doesn't match browser output exactly

**Status:** Superseded by client-side approach, but ideas (IPFS publishing) still valuable.

---

## Attempt 2: Client-Side DOM Capture

**Branch:** `claude/canvas-dom-export` (merged to main)

**Approach:**
- Browser captures already-rendered DOM
- Extracts all CSS from document.styleSheets
- Builds self-contained HTML
- Sends to server which writes to disk

**Files:**
- `web/ts/api/canvas-export.ts` - DOM capture + CSS extraction
- `web/ts/components/glyph/manifestations/canvas-expanded.ts` - Export button
- `glyph/handlers/canvas.go` - HandleExportDOM endpoint
- `docs/demo/index.html` - Generated demo file

**Why it works:**
- Browser is the canonical renderer
- Captures exactly what user sees (WYSIWYG)
- No rendering logic duplication
- Simple, maintainable

**Features:**
- **Two-stage confirmation** - Prevents accidental overwrites
- **Demo mode only** - Feature gated behind `QNTX_DEMO=1`
- **Pan/zoom** - Exported HTML includes interactive navigation (extracted from canvas-pan.ts)
- **Self-contained** - Single HTML file with all CSS and scripts

**Limitations:**
- Requires user interaction (click Export button)
- Can't be used for server-initiated Bluesky posts
- Browser must be running

**Status:** Shipped, works great for demos and manual exports.

---

## The Insight: TypeScript as a Plugin Language

**Problem:** We need both approaches
- **Client-side** for user-initiated exports (demos, sharing)
- **Server-side** for automated posts (Bluesky, IPFS)

**Traditional solution:** Maintain two implementations (fragile)

**Better solution:** Run the same TypeScript code in both environments

### How Next.js/Remix Do It

React components render both server-side (Node) and client-side (browser):

```jsx
// This same component runs in both places
function Canvas({ glyphs }) {
    return (
        <div className="canvas-workspace">
            {glyphs.map(g => <Glyph key={g.id} {...g} />)}
        </div>
    );
}

// Server: const html = renderToString(<Canvas glyphs={data} />)
// Client: ReactDOM.render(<Canvas glyphs={data} />, root)
```

### QNTX Equivalent

Canvas-building code runs both in browser and in TypeScript plugin:

```typescript
// Shared code - works in both environments
export function buildCanvasWorkspace(
    canvasId: string,
    glyphs: Glyph[],
    document: Document // Injected: browser or jsdom
): HTMLElement {
    const workspace = document.createElement('div');
    workspace.className = 'canvas-workspace';
    // ... canvas building logic
    return workspace;
}

// Browser usage
const workspace = buildCanvasWorkspace(id, glyphs, window.document);

// Server plugin usage (jsdom)
const dom = new JSDOM('<!DOCTYPE html>');
const workspace = buildCanvasWorkspace(id, glyphs, dom.window.document);
const html = workspace.outerHTML;
```

**Key:** Same code, different Document implementation.

---

## Future: TypeScript Plugin Architecture

See [ts-plugin.md](./ts-plugin.md) for full implementation plan.

**Vision:**
```
┌─────────────────────────────────────────────────┐
│ QNTX Server                                     │
│                                                 │
│  ┌──────────────────────────────────────────┐  │
│  │ TypeScript Plugin: canvas-renderer       │  │
│  │                                          │  │
│  │  Imports: web/ts/shared/canvas-builder  │  │
│  │  Uses: jsdom for server-side DOM        │  │
│  │  Exposes: POST /render                   │  │
│  └──────────────────────────────────────────┘  │
│                                                 │
│  Server calls plugin when posting to Bluesky   │
└─────────────────────────────────────────────────┘
```

**Benefits:**
1. **No duplication** - One canvas builder, two runtimes
2. **Perfect fidelity** - Server output matches browser exactly
3. **Maintainable** - Canvas features only implemented once
4. **Extensible** - TypeScript plugins enable npm ecosystem server-side

---

## Use Cases

### Current (Client-Side Export)

**Demo/showcase:**
```
User creates impressive canvas
  ↓
Clicks Export button
  ↓
HTML saved to docs/demo/index.html
  ↓
Deploys to GitHub Pages
```

**Manual sharing:**
```
User wants to share canvas with colleague
  ↓
Exports to HTML
  ↓
Emails file or hosts on web server
```

### Future (Server-Side Rendering via TS Plugin)

**Automated Bluesky posts:**
```
QNTX detects interesting pattern
  ↓
LLM generates insight
  ↓
Server calls canvas-renderer plugin
  ↓
Plugin renders canvas HTML
  ↓
Pin to IPFS → permanent link
  ↓
Post to Bluesky with IPFS link
```

**Site builder:**
```
User arranges glyphs as website layout
  ↓
Exports multiple canvases (pages)
  ↓
Plugin generates site:
  - index.html (home canvas)
  - about.html (about canvas)
  - Navigation between pages
  ↓
Deploy to Vercel/Netlify
```

**Documentation generation:**
```
User creates canvas with code + diagrams
  ↓
Server renders nightly snapshots
  ↓
Builds versioned documentation site
```

---

## Architecture Comparison

### Client-Side (Current)

```
┌──────────┐
│ Browser  │
│          │
│ Canvas   │───► DOM Capture ───► HTML + CSS
│ (Live)   │
└──────────┘
     │
     ↓ POST /api/canvas/export-dom
┌──────────┐
│ Server   │───► Write to disk
└──────────┘
```

**Pros:**
- Fast (no subprocess)
- Perfect rendering (uses browser)
- Simple code

**Cons:**
- Requires user interaction
- Can't automate from server

### Server-Side (Future - TypeScript Plugin)

```
┌──────────┐
│ Server   │
│          │
│ Trigger  │───► POST /api/canvas/snapshot
│ (Bluesky)│
└──────────┘
     │
     ↓
┌─────────────────────┐
│ TS Plugin           │
│                     │
│ 1. Load glyphs      │
│ 2. Build canvas     │
│    (shared TS code) │
│ 3. Render to HTML   │
│    (jsdom)          │
└─────────────────────┘
     │
     ↓
   HTML ───► Pin to IPFS ───► Return URL
```

**Pros:**
- Server-initiated (automation)
- Permanent snapshots (IPFS)
- No duplication (uses same TS code)

**Cons:**
- Requires plugin infrastructure
- Slightly slower (subprocess)

### Hybrid (Best of Both)

Both approaches coexist:
- **User-initiated:** Use client-side (faster)
- **Server-initiated:** Use TS plugin (automation)

Same HTML output from both paths.

---

## Canvas Export as Static Site Generation

**Unexpected discovery:** The canvas export primitives naturally support static site generation.

**What we built:**
- Glyphs (visual primitives)
- Canvas (spatial arrangement)
- Export (DOM → static HTML)
- Pan/zoom (navigation)

**What it became:**
A site builder where canvases are pages and glyphs are content.

**Example:**
```
Canvas 1 (home.qntx):
  - Note glyph: "Welcome to my site"
  - Image glyph: hero.png
  - Link to Canvas 2

Canvas 2 (about.qntx):
  - Note glyph: "About me"
  - Code glyph: GitHub embed

Export → Static site:
  - home.html (Canvas 1)
  - about.html (Canvas 2)
  - Navigation preserved
```

**Meta vision:** The QNTX website itself is a canvas. View source → it's an exported QNTX canvas.

**Attestation layer:** Each exported site includes provenance
- "Built with QNTX by @user on 2026-02-25"
- Cryptographic signatures
- Version history via attestations

---

## Technical Details

### CSS Cascade Fix

**Problem:** Exported HTML was blank because captured `body` styles overrode layout styles.

**Solution:** Place critical layout CSS *after* captured CSS:

```html
<style>
/* Captured from document.styleSheets */
body { font-family: system-ui; margin: 0; }

/* Critical overrides - must come last */
html, body { width: 100%; height: 100%; overflow: hidden; }
body { display: flex !important; flex-direction: column !important; }
</style>
```

**Lesson:** CSS cascade order matters. Layout styles need `!important` to win.

### Pan/Zoom Extraction

**Problem:** Initial standalone pan/zoom had buggy touch gestures.

**Solution:** Extract actual `canvas-pan.ts` code, strip dependencies (logger, uiState):

```typescript
// Before: Buggy standalone implementation
// After: Extracted from canvas-pan.ts with minimal changes

// Key: Same gesture detection logic
// - Touch identifier tracking
// - Math.hypot for distance
// - Proper isPanning vs isPinching states
```

**Lesson:** Don't reimplement complex logic. Extract and adapt.

### Demo Mode

**Gating:** Export features only work when `QNTX_DEMO=1`:

```go
func (h *CanvasHandler) HandleExportDOM(w http.ResponseWriter, r *http.Request) {
    if os.Getenv("QNTX_DEMO") != "1" {
        h.writeError(w, errors.New("export only available in demo mode"), http.StatusForbidden)
        return
    }
    // ...
}
```

**Why:** Export is for demos/showcases, not production. Prevents accidental use in real deployments.

---

## Future Enhancements

### Multi-Page Export

**Goal:** Export multiple interconnected canvases as a complete site.

```typescript
interface SiteExport {
    pages: {
        [canvasId: string]: {
            html: string;
            title: string;
            path: string; // e.g., "/about"
        }
    };
    navigation: {
        from: string; // canvas ID
        to: string;   // canvas ID
        label: string;
    }[];
}
```

### SEO Metadata

**Goal:** Add metadata to exported HTML for search engines.

```html
<head>
<meta name="description" content="...">
<meta property="og:title" content="...">
<meta property="og:image" content="...">
<link rel="canonical" href="...">
</head>
```

Source: Glyph attributes or canvas metadata.

### Template System

**Goal:** Provide starter canvases for common site types.

Templates:
- **Portfolio** - Project cards, about section, contact
- **Blog** - Post list, individual post layout
- **Documentation** - Sidebar navigation, content area
- **Landing page** - Hero, features, CTA

Each template is a pre-configured canvas with placeholder glyphs.

### Responsive Layouts

**Goal:** Make exported canvases adapt to screen sizes.

Options:
1. **Fixed aspect ratio** - Canvas scales but maintains layout
2. **Breakpoints** - Different glyph positions for mobile/desktop
3. **Flex/grid** - Convert absolute positioning to flexbox

### Custom Domains

**Goal:** Host exported sites on custom domains.

Flow:
```
Export canvas → Deploy to Vercel → Configure DNS → Live site
```

Integration with deployment platforms (Vercel, Netlify, GitHub Pages).

---

## Related Documents

- [ts-plugin.md](./ts-plugin.md) - TypeScript plugin implementation plan
- [glyphs.md](../vision/glyphs.md) - Glyph architecture vision
- [GLOSSARY.md](../GLOSSARY.md) - Symbol definitions

---

## Timeline

- **2026-02-23:** PR #600 - Server-side HTML reconstruction (superseded)
- **2026-02-25:** PR #620 - Client-side DOM capture (merged)
- **2026-02-25:** Insight - TypeScript as plugin language
- **Future:** TypeScript plugin infrastructure (see ts-plugin.md)

---

## Key Takeaways

1. **Primitives over frameworks** - Build composable primitives (glyphs, canvas), discover applications (site builder)
2. **Code reuse > duplication** - Run same TS code client and server via plugin system
3. **Emergent design** - Site builder wasn't the goal, but primitives naturally support it
4. **Both/and thinking** - Client-side for speed, server-side for automation - keep both
5. **Follow the patterns** - Same approach as Next.js/Remix (SSR) applies to canvas rendering
