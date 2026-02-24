/**
 * Plugin Glyph Discovery — fetches and registers plugin-provided glyph types.
 *
 * Called at app startup to discover glyphs from plugins.
 * Registers each glyph type in the global registry for spawn menu and canvas rendering.
 */

import { registerGlyphType } from './glyph-registry';
import { createPluginGlyph } from './plugin-glyph';
import { apiFetch } from '../../api';
import { log, SEG } from '../../logger';
import type { Glyph } from './glyph';

export interface PluginGlyphDef {
    plugin: string;
    symbol: string;
    title: string;
    label: string;
    content_url: string;
    css_url?: string;
    default_width?: number;
    default_height?: number;
}

const loadedCSS = new Set<string>();

// Track which symbols belong to which plugins (for placeholder fallback)
const pluginSymbols = new Map<string, string>(); // symbol → plugin name

/** Get plugin name for a given symbol, or null if not a plugin glyph */
export function getPluginNameBySymbol(symbol: string): string | null {
    return pluginSymbols.get(symbol) ?? null;
}

/** Load CSS stylesheet for plugin glyph (cached globally) */
export function loadPluginCSS(url: string): void {
    if (loadedCSS.has(url)) return;
    loadedCSS.add(url);

    // Use absolute URL to backend (dev mode: frontend on :8826, backend on :8776)
    const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const absoluteUrl = backendUrl + url;

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = absoluteUrl;
    document.head.appendChild(link);
    log.debug(SEG.GLYPH, `[PluginGlyphs] Loaded CSS: ${absoluteUrl}`);
}

/** Fetch plugin glyph definitions and register them */
export async function loadPluginGlyphs(): Promise<void> {
    try {
        const resp = await apiFetch('/api/plugins/glyphs');
        if (!resp.ok) {
            log.warn(SEG.GLYPH, `[PluginGlyphs] Failed to fetch plugin glyphs: ${resp.status}`);
            return;
        }

        const defs: PluginGlyphDef[] = await resp.json();

        for (const def of defs) {
            registerPluginGlyphType(def);
        }

        log.info(SEG.GLYPH, `[PluginGlyphs] Loaded ${defs.length} plugin glyph type(s)`);
    } catch (err) {
        log.error(SEG.GLYPH, '[PluginGlyphs] Error loading plugin glyphs:', err);
    }
}

function registerPluginGlyphType(def: PluginGlyphDef): void {
    // Track symbol → plugin name mapping for placeholder fallback
    pluginSymbols.set(def.symbol, def.plugin);

    registerGlyphType({
        symbol: def.symbol,
        className: `canvas-plugin-glyph plugin-${def.plugin}`,
        title: def.title,
        label: def.label,
        render: (glyph: Glyph) => createPluginGlyph(glyph, def),
    });
}
