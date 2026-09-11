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
 * A module whose glyphDef says 'panel' is added to the tray.
 */

import { registerGlyphType, getGlyphTypeBySymbol, replacePluginGlyphType } from './glyph-registry';
import { createPluginGlyph } from './plugin-glyph';
import { createPluginGlyphFromModule, wrapInCanvasPlaced } from './glyph-module-loader';
import { redrawPlacedGlyphs } from './canvas/canvas-workspace-builder';
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
// Whatever provided the glyph a symbol belongs to — a plugin or a published
// module. What it is called is enough to ask the node what it is.
const pluginSymbols = new Map<string, string>(); // symbol → glyph name

// Which build of each plugin's module is the one currently registered, so a
// re-run can tell a module that moved from one that did not.
const registeredDigests = new Map<string, string>(); // plugin name → digest

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

    // Phase 3: glyph modules the node publishes, served same-origin from /g/
    await discoverPublishedGlyphs();
}

/**
 * The watcher every node is born holding, which tells the pages in its
 * namespace that a glyph module was published.
 *
 * The same string as ats/watcher's StandingGlyphPublished. standing-watcher-id.test.ts
 * reads the Go source and fails if the two ever say different things.
 */
export const STANDING_GLYPH_PUBLISHED = 'standing-glyph-published';

/** One glyph the node serves, as /g/ reports it. */
interface PublishedGlyph {
    name: string;
    as: string;
    url: string;
}

/** Which published module is the one currently registered, by glyph name. */
const publishedAs = new Map<string, string>();

/**
 * Why a published glyph is not on the canvas, by glyph name.
 *
 * A console line is only read by somebody who thought to open a console. The
 * glyph that is missing is the thing being looked at, so the reason is kept
 * here and drawn in its place.
 */
const whyAbsent = new Map<string, string>();

/** What went wrong the last time this glyph was asked for, if anything. */
export function glyphAbsenceReason(name: string): string | undefined {
    return whyAbsent.get(name);
}

/** Say why, both to the log and to whatever draws in the glyph's place. */
function absent(name: string, why: string, err?: unknown): void {
    const said = err === undefined ? why : `${why}: ${err instanceof Error ? err.message : String(err)}`;
    whyAbsent.set(name, said);
    log.error(SEG.GLYPH, `[Glyphs] ${name} ${said}`);
}

/**
 * Register every glyph the node publishes.
 *
 * The module is served same-origin, so importing it is what script-src already
 * permits — a cross-origin import is refused before a request is made, with
 * nothing in the network log to find.
 *
 * Every failure here is a fault. The node said the glyph is published; being
 * unable to load it is never expected, and never a debug line.
 */
export async function discoverPublishedGlyphs(): Promise<void> {
    let published: PublishedGlyph[];
    try {
        const resp = await apiFetch('/g/');
        if (!resp.ok) {
            log.error(SEG.GLYPH, `[Glyphs] /g/ answered ${resp.status}; no published glyph is registered`);
            return;
        }
        const data: { glyphs?: PublishedGlyph[] } = await resp.json();
        published = data.glyphs ?? [];
    } catch (err) {
        log.error(SEG.GLYPH, '[Glyphs] Could not list published glyphs; none are registered:', err);
        return;
    }

    for (const glyph of published) {
        if (publishedAs.get(glyph.name) === glyph.as) continue;

        // The attestation id is in the URL, so a published module is one the
        // browser has not imported and cannot answer from what it holds.
        const url = `${glyph.url}?v=${glyph.as}`;
        try {
            const raw: Record<string, unknown> = await import(/* @vite-ignore */ url);
            const mod = (raw.default ?? raw) as GlyphModule & { glyphDef?: GlyphDef };
            const def = mod.glyphDef;

            if (!def) {
                absent(glyph.name, `is published as ${glyph.as} and exports no glyphDef, so it cannot be placed`);
                continue;
            }
            if (typeof mod.render !== 'function') {
                absent(glyph.name, `is published as ${glyph.as} and exports no render, so it cannot be drawn`);
                continue;
            }

            await place(glyph, def, mod as GlyphModule);
            whyAbsent.delete(glyph.name);
        } catch (err) {
            // The node published this. Failing to load it is a fault, and the
            // reason is the only thing that will ever say why it is absent.
            absent(glyph.name, `is published as ${glyph.as} and failed to load from ${url}`, err);
        }
    }
}

/** Put a published glyph where its own glyphDef says it goes. */
async function place(glyph: PublishedGlyph, def: GlyphDef, mod: GlyphModule): Promise<void> {
    const name = glyph.name;

    if (def.manifestation === 'panel') {
        const id = `glyph-${name}`;
        if (replacePanel(id, `${name} (${glyph.as})`, def, mod)) {
            publishedAs.set(name, glyph.as);
            return;
        }
        if (glyphRun.has(id)) {
            // Nothing on this page put it there, so replacing it would take
            // over something this code does not own.
            log.error(SEG.GLYPH, `[Glyphs] ${name} cannot take tray id ${id}; something else holds it`);
            return;
        }
        addPanel(id, name, def, mod);
        publishedAs.set(name, glyph.as);
        log.info(SEG.GLYPH, `[Glyphs] ${name} (${def.symbol}) is in the tray, at ${glyph.as}`);
        return;
    }

    const entry = {
        symbol: def.symbol,
        className: `canvas-published-glyph glyph-${name}`,
        title: def.title,
        label: def.label,
        // Published, not a plugin. There is no process, no am.toml line and
        // nothing to enable — the module is an attestation this node serves.
        publishedName: name,
        render: async (canvasGlyph: Glyph) => {
            const ui = createGlyphUI(canvasGlyph, name);
            const rendered = await mod.render(canvasGlyph, ui);
            if (!rendered.dataset.glyphId) {
                return wrapInCanvasPlaced(canvasGlyph, rendered, {
                    plugin: name,
                    title: def.title,
                    symbol: def.symbol,
                    defaultWidth: def.defaultWidth,
                    defaultHeight: def.defaultHeight,
                });
            }
            return rendered;
        },
    };

    if (getGlyphTypeBySymbol(def.symbol)) {
        if (!replacePluginGlyphType(entry)) {
            log.error(SEG.GLYPH, `[Glyphs] ${name} claims ${def.symbol}, which is held by something else`);
            return;
        }
        publishedAs.set(name, glyph.as);
        log.info(SEG.GLYPH, `[Glyphs] ${name} (${def.symbol}) reloaded, at ${glyph.as}`);
        // The entry is what the next render reads. What is drawn already came
        // from the module this one replaces, and catches up here.
        await redrawPlacedGlyphs(def.symbol);
        return;
    }

    pluginSymbols.set(def.symbol, name);
    registerGlyphType(entry);
    publishedAs.set(name, glyph.as);
    log.info(SEG.GLYPH, `[Glyphs] ${name} (${def.symbol}) is on the canvas, at ${glyph.as}`);
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
    // What the node says each plugin's module is now, by plugin name. A plugin
    // that serves no module says nothing and keeps the bare URL.
    const digests = new Map<string, string>();
    try {
        const resp = await apiFetch('/api/plugins');
        if (!resp.ok) return;
        const data: { plugins?: Array<{ name: string; module_digest?: string }> } = await resp.json();
        pluginNames = (data.plugins ?? []).map(p => p.name);
        for (const p of data.plugins ?? []) {
            if (p.module_digest) digests.set(p.name, p.module_digest);
        }
    } catch (err) {
        // Discovery skipped means every TS plugin glyph is absent this
        // session — that must not look like there being none.
        log.error(SEG.GLYPH, '[PluginGlyphs] Could not list plugins; TS plugin glyphs are not registered:', err);
        return;
    }

    // The dev server names the plugins it serves from this machine; those are probed too.
    const devPlugins = (window as { __DEV_PLUGINS__?: string[] }).__DEV_PLUGINS__ ?? [];
    for (const name of devPlugins) {
        if (!pluginNames.includes(name)) pluginNames.push(name);
    }

    let count = 0;
    for (const name of pluginNames) {
        const digest = digests.get(name);
        // The digest is in the URL because a module specifier the browser has
        // already imported returns the module it imported, whatever the file
        // on disk says now.
        const moduleUrl = digest
            ? `/api/${name}/glyph-module.js?v=${digest}`
            : `/api/${name}/glyph-module.js`;
        try {
            const raw: Record<string, unknown> = await import(/* @vite-ignore */ moduleUrl);
            const mod = (raw.default ?? raw) as GlyphModule & { glyphDef?: GlyphDef };
            const def = mod.glyphDef;
            if (!def || typeof mod.render !== 'function') continue;

            const cachedMod = mod as GlyphModule;

            if (def.manifestation === 'panel') {
                // A tray glyph is keyed by id, and discovery runs more than once per page.
                const id = `plugin-${name}`;
                if (livePanels.has(id)) {
                    // Only a new digest is news; the same module again is not.
                    if (!digest || registeredDigests.get(name) === digest) continue;
                    if (replacePanel(id, `${name} (${digest})`, def, cachedMod)) {
                        registeredDigests.set(name, digest);
                    }
                    continue;
                }
                if (glyphRun.has(id)) continue;

                addPanel(id, name, def, cachedMod);
                if (digest) registeredDigests.set(name, digest);
                count++;
                log.info(SEG.GLYPH, `[PluginGlyphs] Discovered TS plugin panel: ${name} (${def.symbol})`);
                continue;
            }

            const entry = {
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
            };

            if (getGlyphTypeBySymbol(def.symbol)) {
                // Already registered — by the Go plugin path, or by this one
                // before the module was replaced. Only a new digest is news.
                if (!digest || registeredDigests.get(name) === digest) continue;
                if (!replacePluginGlyphType(entry)) continue;

                registeredDigests.set(name, digest);
                log.info(SEG.GLYPH, `[PluginGlyphs] Reloaded TS plugin module: ${name} (${def.symbol}) at ${digest}`);
                await redrawPlacedGlyphs(def.symbol);
                continue;
            }

            pluginSymbols.set(def.symbol, name);
            registerGlyphType(entry);
            if (digest) registeredDigests.set(name, digest);
            count++;
            log.info(SEG.GLYPH, `[PluginGlyphs] Discovered TS plugin module: ${name} (${def.symbol})`);
        } catch (err) {
            // A plugin the node gave no digest for serves no module, and this
            // probe was always going to fail. One that has a digest was going
            // to work, so failing is a fault and never a line nobody reads.
            if (digest) {
                log.error(SEG.GLYPH, `[PluginGlyphs] ${name} publishes module ${digest} and it failed to load from ${moduleUrl}:`, err);
            } else {
                log.debug(SEG.GLYPH, `[PluginGlyphs] ${name} serves no TS module:`, err);
            }
        }
    }

    if (count > 0) {
        log.info(SEG.GLYPH, `[PluginGlyphs] Discovered ${count} TS plugin module(s)`);
    }
}

/**
 * The module a tray glyph is drawing from now, held apart from the Glyph so a
 * replacement reaches the panel that is already open.
 *
 * redraw is set while the panel holds an element and cleared when it lets go.
 */
interface LiveModule {
    mod: GlyphModule;
    def: GlyphDef;
    redraw: (() => void) | null;
}

/** The module behind each tray glyph this page put there, by its tray id. */
const livePanels = new Map<string, LiveModule>();

/**
 * Put a tray glyph on the newest module, redrawing it if it is open.
 *
 * Returns false when nothing on this page owns that id, which is a collision
 * rather than a replacement.
 */
function replacePanel(id: string, what: string, def: GlyphDef, mod: GlyphModule): boolean {
    const live = livePanels.get(id);
    if (!live) return false;

    live.mod = mod;
    live.def = def;
    if (!live.redraw) {
        log.info(SEG.GLYPH, `[Glyphs] ${what} is in the tray and closed; it opens on the module just published`);
        return true;
    }
    live.redraw();
    log.info(SEG.GLYPH, `[Glyphs] ${what} is open in the tray and was redrawn from the module just published`);
    return true;
}

// The tray's contract, glyphRun.add, met by a module's render(). The ui it gets is the one a canvas glyph gets.
function makePanelGlyph(id: string, name: string, live: LiveModule): Glyph {
    let container: HTMLElement | null = null;

    // Whatever the module registered runs before its element is filled again,
    // so a redraw leaves nothing of the module it replaced behind.
    const draw = (el: HTMLElement): void => {
        runCleanup(el);
        const ui = createGlyphUI(glyph, name, el);
        Promise.resolve()
            .then(() => live.mod.render(glyph, ui))
            .then(rendered => {
                el.replaceChildren(rendered);
            })
            .catch((err: unknown) => {
                log.error(SEG.GLYPH, `[PluginGlyphs] ${name} panel render failed:`, err);
                el.replaceChildren(`${name}: ${err instanceof Error ? err.message : String(err)}`);
            });
    };

    const glyph: Glyph = {
        id,
        title: live.def.title,
        symbol: live.def.symbol,
        manifestationType: 'panel',
        renderContent: () => {
            // The panel wants its element now; the module fills it when render() resolves.
            const el = document.createElement('div');
            el.className = `plugin-panel-glyph plugin-${name}`;
            container = el;
            live.redraw = () => draw(el);
            draw(el);
            return el;
        },
        // Close is the one point the panel discards its content; minimize keeps it. The module's cleanups run here.
        onClose: () => {
            if (container) runCleanup(container);
            container = null;
            live.redraw = null;
        },
    };
    return glyph;
}

/** Put a glyph in the tray for the first time, holding the module it draws from. */
function addPanel(id: string, name: string, def: GlyphDef, mod: GlyphModule): void {
    const live: LiveModule = { mod, def, redraw: null };
    livePanels.set(id, live);
    glyphRun.add(makePanelGlyph(id, name, live));
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
