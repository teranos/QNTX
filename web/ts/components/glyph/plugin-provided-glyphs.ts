/**
 * Plugin Glyph Discovery — fetches and registers plugin-provided glyph types.
 *
 * Called at app startup to discover glyphs from plugins.
 * Registers each glyph type in the global registry for spawn menu and canvas rendering.
 *
 * Two rendering paths:
 * 1. module_url set → TypeScript module with GlyphUI injection (preferred)
 * 2. content_url only → server-rendered HTML via innerHTML (legacy)
 */

import { registerGlyphType, getGlyphTypeBySymbol } from './glyph-registry';
import { createPluginGlyph } from './plugin-glyph';
import { createPluginGlyphFromModule } from './glyph-module-loader';
import { apiFetch } from '../../api';
import { log, SEG } from '../../logger';
import type { Glyph } from './glyph';
import type { GlyphDef, GlyphModule } from './glyph-ui';
import { createGlyphUI } from './glyph-ui';

export interface PluginGlyphDef {
    plugin: string;
    symbol: string;
    title: string;
    label: string;
    content_url: string;
    css_url?: string;
    module_url?: string;
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

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = url;
    document.head.appendChild(link);
    log.debug(SEG.GLYPH, `[PluginGlyphs] Loaded CSS: ${url}`);
}

/** Fetch plugin glyph definitions and register them */
export async function loadPluginGlyphs(): Promise<void> {
    // Phase 1: Go plugins — glyph defs announced via gRPC
    try {
        const resp = await apiFetch('/api/plugins/glyphs');
        if (!resp.ok) {
            log.warn(SEG.GLYPH, `[PluginGlyphs] Failed to fetch plugin glyphs: ${resp.status}`);
        } else {
            const defs: PluginGlyphDef[] = await resp.json();
            for (const def of defs) {
                registerPluginGlyphType(def);
            }
            log.info(SEG.GLYPH, `[PluginGlyphs] Loaded ${defs.length} plugin glyph type(s) from backend`);
        }
    } catch (err) {
        log.error(SEG.GLYPH, '[PluginGlyphs] Error loading plugin glyphs:', err);
    }

    // Phase 2: TS plugin modules — self-describing via glyphDef export
    await discoverTSPluginModules();
}

/**
 * Discover pure TS plugin modules by probing enabled plugins for a glyph module.
 *
 * For each enabled plugin, tries to import /api/{name}/ix-glyph-module.js.
 * If the module exports a glyphDef, it's a self-describing TS plugin — register it.
 * If the import fails, the plugin is Go-only (already handled above).
 */
async function discoverTSPluginModules(): Promise<void> {
    // Get enabled plugin names from /api/plugins
    let pluginNames: string[];
    try {
        const resp = await apiFetch('/api/plugins');
        if (!resp.ok) return;
        const data: { plugins?: Array<{ name: string }> } = await resp.json();
        pluginNames = (data.plugins ?? []).map(p => p.name);
    } catch {
        return;
    }

    let count = 0;
    for (const name of pluginNames) {
        const moduleUrl = `/api/${name}/ix-glyph-module.js`;
        try {
            const mod: GlyphModule & { glyphDef?: GlyphDef } = await import(/* @vite-ignore */ moduleUrl);
            const def = mod.glyphDef ?? mod.default?.glyphDef;
            if (!def || typeof mod.render !== 'function') continue;

            // Skip if this symbol was already registered (e.g., by the Go plugin path)
            if (getGlyphTypeBySymbol(def.symbol)) continue;

            pluginSymbols.set(def.symbol, name);

            const cachedMod = mod as GlyphModule;
            registerGlyphType({
                symbol: def.symbol,
                className: `canvas-plugin-glyph plugin-${name}`,
                title: def.title,
                label: def.label,
                pluginName: name,
                render: async (glyph: Glyph) => {
                    const ui = createGlyphUI(glyph, name);
                    return cachedMod.render(glyph, ui);
                },
            });
            count++;
            log.info(SEG.GLYPH, `[PluginGlyphs] Discovered TS plugin module: ${name} (${def.symbol})`);
        } catch {
            // Not a TS plugin or module doesn't exist — expected for Go-only plugins
        }
    }

    if (count > 0) {
        log.info(SEG.GLYPH, `[PluginGlyphs] Discovered ${count} TS plugin module(s)`);
    }
}

function registerPluginGlyphType(def: PluginGlyphDef): void {
    // Track symbol → plugin name mapping for placeholder fallback
    pluginSymbols.set(def.symbol, def.plugin);

    // module_url → TypeScript SDK path; content_url → legacy HTML path
    const renderer = def.module_url
        ? (glyph: Glyph) => createPluginGlyphFromModule(glyph, def)
        : (glyph: Glyph) => createPluginGlyph(glyph, def);

    registerGlyphType({
        symbol: def.symbol,
        className: `canvas-plugin-glyph plugin-${def.plugin}`,
        title: def.title,
        label: def.label,
        pluginName: def.plugin,
        render: renderer,
    });
}
