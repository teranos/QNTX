/**
 * Plugin Glyph Module Loader — dynamic import + SDK injection for TS-authored plugin glyphs.
 *
 * When a plugin declares a module_url in its GlyphDef, the frontend uses this
 * loader instead of the legacy HTML pipeline. The module is dynamically imported,
 * its render function is called with a PluginGlyphSDK, and the returned element
 * is mounted on the canvas.
 *
 * This eliminates: innerHTML, script re-execution, duplicated escapeHTML,
 * global window.* pollution, and gives plugins type-safe access to QNTX primitives.
 */

import type { Glyph } from './glyph';
import type { PluginGlyphDef } from './plugin-provided-glyphs';
import type { PluginGlyphModule } from './plugin-glyph-sdk';
import { createPluginSDK } from './plugin-glyph-sdk';
import { loadPluginCSS } from './plugin-provided-glyphs';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';

// Cache imported modules — one import per module_url
const moduleCache = new Map<string, Promise<PluginGlyphModule>>();

/** Create a plugin glyph by dynamically importing its TypeScript module. */
export async function createPluginGlyphFromModule(
    glyph: Glyph,
    def: PluginGlyphDef
): Promise<HTMLElement> {
    // Load CSS if provided (cached globally)
    if (def.css_url) {
        loadPluginCSS(def.css_url);
    }

    const moduleUrl = def.module_url!;

    // Resolve to absolute URL (dev mode: frontend on :8826, backend on :8776)
    const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const absoluteUrl = backendUrl + moduleUrl;

    try {
        // Import module (cached per URL)
        const mod = await loadModule(absoluteUrl);

        // Create SDK scoped to this glyph + plugin
        const sdk = createPluginSDK(glyph, def.plugin);

        // Call the plugin's render function
        const element = await mod.render(glyph, sdk);

        log.debug(SEG.GLYPH, `[PluginModule] Rendered ${def.plugin} glyph ${glyph.id} from module`);
        return element;
    } catch (err) {
        log.error(SEG.GLYPH, `[PluginModule] Failed to load module for ${def.plugin}: ${moduleUrl}`, err);
        return createModuleErrorGlyph(glyph, def, err);
    }
}

/** Load and cache a plugin module. Only caches successful imports. */
function loadModule(url: string): Promise<PluginGlyphModule> {
    const cached = moduleCache.get(url);
    if (cached) return cached;

    const pending = import(/* @vite-ignore */ url).then((mod) => {
        // Support both default export and named export
        if (typeof mod.render === 'function') return mod as PluginGlyphModule;
        if (mod.default && typeof mod.default.render === 'function') return mod.default as PluginGlyphModule;
        throw new Error(`Module does not export a render function: ${url}`);
    });

    // Cache the promise immediately for dedup, but evict on failure
    // so the next attempt can retry (e.g., after plugin restart)
    moduleCache.set(url, pending);
    pending.catch(() => { moduleCache.delete(url); });

    return pending;
}

/** Create an error placeholder when module loading fails. */
function createModuleErrorGlyph(glyph: Glyph, def: PluginGlyphDef, err: unknown): HTMLElement {
    const { element } = canvasPlaced({
        glyph,
        className: `canvas-plugin-glyph plugin-${def.plugin}`,
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: def.default_width ?? 400,
            height: def.default_height ?? 300,
        },
        resizable: false,
        logLabel: 'PluginModuleError',
    });

    const content = document.createElement('div');
    content.style.padding = '16px';
    content.style.color = 'var(--color-error, #ef4444)';
    content.style.fontFamily = 'var(--font-mono)';
    content.style.fontSize = '12px';

    const title = document.createElement('div');
    title.style.fontWeight = 'bold';
    title.style.marginBottom = '8px';
    title.textContent = `Failed to load ${def.plugin} module`;

    const detail = document.createElement('div');
    detail.style.opacity = '0.8';
    detail.textContent = err instanceof Error ? err.message : String(err);

    content.appendChild(title);
    content.appendChild(detail);
    element.appendChild(content);

    return element;
}
