/**
 * Plugin Glyph Discovery — fetches and registers plugin-provided glyph types.
 *
 * Called at app startup to discover glyphs from plugins.
 * Registers each glyph type in the global registry for spawn menu and canvas rendering.
 *
 * Two rendering paths:
 * 1. module_url set → TypeScript module with GlyphUI injection (preferred)
 * 2. content_url only → server-rendered HTML via innerHTML (legacy)
 *
 * A self-describing TS module whose glyphDef says manifestation 'panel' is
 * not a canvas type at all: it goes into the tray as a fullscreen panel.
 */

import { registerGlyphType, getGlyphTypeBySymbol } from './glyph-registry';
import { createPluginGlyph } from './plugin-glyph';
import { createPluginGlyphFromModule, wrapInCanvasPlaced } from './glyph-module-loader';
import { apiFetch } from '../../client';
import { log, SEG } from '../../logger';
import { glyphRun, runCleanup } from '@qntx/glyphs';
import type { Glyph } from '@qntx/glyphs';
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
 * For each enabled plugin, tries to import /api/{name}/glyph-module.js.
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
    } catch (err) {
        // Discovery skipped means every TS plugin glyph is absent this
        // session — that must not look like there being none.
        log.error(SEG.GLYPH, '[PluginGlyphs] Could not list plugins; TS plugin glyphs are not registered:', err);
        return;
    }

    // A plugin served from a directory on this machine by the dev server is
    // not on the node's list — the dev server names it instead (dev-server.ts).
    const devPlugins = (window as { __DEV_PLUGINS__?: string[] }).__DEV_PLUGINS__ ?? [];
    for (const name of devPlugins) {
        if (!pluginNames.includes(name)) pluginNames.push(name);
    }

    let count = 0;
    for (const name of pluginNames) {
        const moduleUrl = `/api/${name}/glyph-module.js`;
        try {
            const raw: Record<string, unknown> = await import(/* @vite-ignore */ moduleUrl);
            const mod = (raw.default ?? raw) as GlyphModule & { glyphDef?: GlyphDef };
            const def = mod.glyphDef;
            if (!def || typeof mod.render !== 'function') continue;

            const cachedMod = mod as GlyphModule;

            if (def.manifestation === 'panel') {
                // A tray glyph is keyed by id, not symbol; two panels may share
                // a symbol, and the canvas placeholder retry runs discovery again.
                const id = `plugin-${name}`;
                if (glyphRun.has(id)) continue;

                glyphRun.add(makePanelGlyph(id, name, def, cachedMod));
                count++;
                log.info(SEG.GLYPH, `[PluginGlyphs] Discovered TS plugin panel: ${name} (${def.symbol})`);
                continue;
            }

            // Skip if this symbol was already registered (e.g., by the Go plugin path)
            if (getGlyphTypeBySymbol(def.symbol)) continue;

            pluginSymbols.set(def.symbol, name);

            registerGlyphType({
                symbol: def.symbol,
                className: `canvas-plugin-glyph plugin-${name}`,
                title: def.title,
                label: def.label,
                pluginName: name,
                render: async (glyph: Glyph) => {
                    const ui = createGlyphUI(glyph, name);
                    const rendered = await cachedMod.render(glyph, ui);
                    if (!rendered.dataset.glyphId) {
                        return wrapInCanvasPlaced(glyph, rendered, {
                            plugin: name,
                            title: def.title,
                            symbol: def.symbol,
                            defaultWidth: def.defaultWidth,
                            defaultHeight: def.defaultHeight,
                        });
                    }
                    return rendered;
                },
            });
            count++;
            log.info(SEG.GLYPH, `[PluginGlyphs] Discovered TS plugin module: ${name} (${def.symbol})`);
        } catch (err) {
            // A Go-only plugin lands here by design; a broken TS module also
            // does — the log line keeps the two tellable apart.
            log.debug(SEG.GLYPH, `[PluginGlyphs] No TS module registered for ${name}:`, err);
        }
    }

    if (count > 0) {
        log.info(SEG.GLYPH, `[PluginGlyphs] Discovered ${count} TS plugin module(s)`);
    }
}

/** A tray glyph whose panel content is whatever the module's render() resolves to. */
function makePanelGlyph(id: string, name: string, def: GlyphDef, mod: GlyphModule): Glyph {
    let container: HTMLElement | null = null;
    const glyph: Glyph = {
        id,
        title: def.title,
        symbol: def.symbol,
        manifestationType: 'panel',
        renderContent: () => {
            // renderContent answers synchronously; the panel wraps this in its
            // scrolling .glyph-content-area and the module fills it when ready.
            const el = document.createElement('div');
            el.className = `plugin-panel-glyph plugin-${name}`;
            container = el;

            const ui = createGlyphUI(glyph, name, el);
            Promise.resolve()
                .then(() => mod.render(glyph, ui))
                .then(rendered => {
                    el.replaceChildren(rendered);
                })
                .catch((err: unknown) => {
                    log.error(SEG.GLYPH, `[PluginGlyphs] ${name} panel render failed:`, err);
                    el.textContent = `${name}: ${err instanceof Error ? err.message : String(err)}`;
                });

            return el;
        },
        // Minimize stashes the content and brings the same nodes back; close
        // is the one point the panel discards them (panel.ts), so the module's
        // onCleanup functions run here.
        onClose: () => {
            if (container) runCleanup(container);
            container = null;
        },
    };
    return glyph;
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
