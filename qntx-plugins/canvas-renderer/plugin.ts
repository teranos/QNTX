/**
 * Canvas Renderer Plugin
 *
 * Renders QNTX canvases to static HTML using server-side DOM (happy-dom).
 * Uses minimal canvas-building logic focused on structure, not interactivity.
 */

import { Window } from 'happy-dom';
import { readFileSync, readdirSync, existsSync } from 'fs';
import { join, dirname } from 'path';

export default {
    name: 'canvas-renderer',
    version: '1.0.0',
    qntx_version: '>= 0.1.0',
    description: 'Server-side canvas HTML renderer',
    author: 'QNTX Team',
    license: 'MIT',

    async init(config: any) {
        console.log('[CanvasRenderer] Plugin initialized');
        return { success: true };
    },

    registerHTTP(mux: any) {
        // POST /render - Render canvas to HTML
        mux.handle('POST', '/render', async (req: any, res: any) => {
            try {
                const { canvas_id, glyphs } = await req.json();

                if (!canvas_id) {
                    res.status(400).json({ error: 'canvas_id is required' });
                    return;
                }

                if (!Array.isArray(glyphs)) {
                    res.status(400).json({ error: 'glyphs must be an array' });
                    return;
                }

                console.log(`[CanvasRenderer] Rendering canvas ${canvas_id} with ${glyphs.length} glyphs`);

                // Create server-side DOM environment
                const window = new Window({
                    url: 'http://localhost',
                    width: 1920,
                    height: 1080,
                });
                const document = window.document;

                // Build canvas HTML structure
                const workspace = buildCanvasHTML(document, canvas_id, glyphs);

                // Load CSS
                const css = loadCanvasCSS();

                // Build complete HTML document
                const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>QNTX Canvas - ${canvas_id}</title>
<style>
${css}
</style>
</head>
<body>
${workspace.outerHTML}
</body>
</html>`;

                res.json({ html });
            } catch (error) {
                console.error('[CanvasRenderer] Render error:', error);
                res.status(500).json({ error: String(error) });
            }
        });
    },

    async shutdown() {
        console.log('[CanvasRenderer] Plugin shutting down');
    }
};

/**
 * Build canvas HTML structure (server-side, no interactivity)
 */
function buildCanvasHTML(document: Document, canvasId: string, glyphs: any[]): HTMLElement {
    // Create workspace container
    const workspace = document.createElement('div');
    workspace.className = 'canvas-workspace';
    workspace.setAttribute('data-canvas-id', canvasId);
    workspace.style.cssText = `
        width: 100%;
        height: 100vh;
        position: relative;
        overflow: hidden;
        background-color: var(--bg-primary, #1a1a1a);
    `;

    // Create content layer
    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';
    contentLayer.style.cssText = `
        position: absolute;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
    `;

    // Render each glyph
    for (const glyph of glyphs) {
        const glyphElement = renderGlyphSimple(document, glyph);
        contentLayer.appendChild(glyphElement);
    }

    workspace.appendChild(contentLayer);
    return workspace;
}

/**
 * Render a single glyph (simplified for server-side)
 */
function renderGlyphSimple(document: Document, glyph: any): HTMLElement {
    const container = document.createElement('div');
    container.className = 'canvas-glyph';
    container.setAttribute('data-glyph-id', glyph.id);
    container.style.cssText = `
        position: absolute;
        left: ${glyph.x || 0}px;
        top: ${glyph.y || 0}px;
        width: ${glyph.width || 200}px;
        height: ${glyph.height || 150}px;
        background: #252625;
        border: 1px solid rgba(220, 222, 221, 0.35);
        border-radius: 4px;
        padding: 12px;
        box-sizing: border-box;
        box-shadow: 2px 2px 8px rgba(0, 0, 0, 0.2);
    `;

    // Simple content rendering based on symbol
    if (glyph.symbol === '▣' || glyph.symbol === 'note') {
        // Note glyph - render text content
        const noteContent = document.createElement('div');
        noteContent.className = 'note-content';
        noteContent.textContent = glyph.content || '(empty note)';
        noteContent.style.cssText = `
            color: rgba(255, 255, 255, 0.85);
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            font-size: 14px;
            line-height: 1.5;
            white-space: pre-wrap;
            word-break: break-word;
        `;
        container.appendChild(noteContent);
    } else {
        // Generic glyph - just show symbol and id
        const placeholder = document.createElement('div');
        placeholder.style.cssText = `
            color: rgba(255, 255, 255, 0.55);
            font-family: monospace;
            font-size: 12px;
        `;
        placeholder.textContent = `${glyph.symbol || 'glyph'}: ${glyph.id}`;
        container.appendChild(placeholder);
    }

    return container;
}

/**
 * Find QNTX root by walking up from plugin directory looking for go.mod
 */
function findQNTXRoot(): string | null {
    // Start from plugin file location (import.meta.dir is Bun-specific)
    let dir = import.meta.dir;

    // Walk up looking for go.mod (QNTX root marker)
    while (dir !== dirname(dir)) {
        if (existsSync(join(dir, 'go.mod'))) {
            return dir;
        }
        dir = dirname(dir);
    }

    return null;
}

/**
 * Load canvas CSS files
 */
function loadCanvasCSS(): string {
    // Find QNTX root - check env var first, then walk up from plugin dir
    let root = process.env.QNTX_ROOT;
    if (!root) {
        // Walk up from plugin directory looking for go.mod
        root = findQNTXRoot();
    }
    if (!root) {
        console.error('[CanvasRenderer] Cannot find QNTX root - CSS will fail to load');
        return '/* QNTX root not found - set QNTX_ROOT env var */';
    }

    const cssFiles: string[] = [];

    // Core CSS files
    const coreFiles = ['web/css/core.css', 'web/css/canvas.css'];
    for (const file of coreFiles) {
        try {
            const path = join(root, file);
            cssFiles.push(readFileSync(path, 'utf-8'));
        } catch (error) {
            console.warn(`[CanvasRenderer] Failed to load CSS: ${file}`, error);
            cssFiles.push(`/* Failed to load ${file} */`);
        }
    }

    // Load all CSS files from glyph directory
    try {
        const glyphDir = join(root, 'web/css/glyph');
        const glyphFiles = readdirSync(glyphDir)
            .filter(f => f.endsWith('.css'))
            .map(f => join(glyphDir, f));

        for (const file of glyphFiles) {
            try {
                cssFiles.push(readFileSync(file, 'utf-8'));
            } catch (error) {
                console.warn(`[CanvasRenderer] Failed to load glyph CSS: ${file}`, error);
            }
        }
    } catch (error) {
        console.warn(`[CanvasRenderer] Failed to read glyph CSS directory`, error);
        cssFiles.push('/* Failed to load glyph CSS directory */');
    }

    return cssFiles.join('\n\n');
}
