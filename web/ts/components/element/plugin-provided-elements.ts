/**
 * Plugin Element Discovery — fetches and registers plugin-provided element types.
 *
 * Called at app startup to discover elements from plugins.
 * Registers each element type in the global registry for spawn menu and canvas rendering.
 *
 * Two rendering paths:
 * 1. module_url set → TypeScript module with ElementUI injection (preferred)
 * 2. content_url only → server-rendered HTML via innerHTML (legacy)
 *
 * A module whose elementDef says 'panel' is added to the tray.
 */

import { registerElementType, getElementTypeBySymbol, replacePluginElementType } from './element-registry';
import { createPluginElement } from './plugin-element';
import { createPluginElementFromModule, wrapInCanvasPlaced } from './element-module-loader';
import { redrawPlacedElements } from './canvas/canvas-workspace-builder';
import { apiFetch } from '../../client';
import { importScript } from '../../client/url';
import { log, SEG } from '../../logger';
import { tray, runCleanup } from '@teranos/elements';
import type { Element } from '@teranos/elements';
import type { ElementDef, ElementModule } from './element-ui';
import { createElementUI } from './element-ui';

export interface PluginElementDef {
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
// Whatever provided the element a symbol belongs to — a plugin or a published
// module. What it is called is enough to ask the node what it is.
const pluginSymbols = new Map<string, string>(); // symbol → element name

// Which build of each plugin's module is the one currently registered, so a
// re-run can tell a module that moved from one that did not.
const registeredDigests = new Map<string, string>(); // plugin name → digest

/** Get plugin name for a given symbol, or null if not a plugin element */
export function getPluginNameBySymbol(symbol: string): string | null {
    return pluginSymbols.get(symbol) ?? null;
}

/** Load CSS stylesheet for plugin element (cached globally) */
export function loadPluginCSS(url: string): void {
    if (loadedCSS.has(url)) return;
    loadedCSS.add(url);

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = url;
    document.head.appendChild(link);
    log.debug(SEG.ELEMENT, `[PluginElements] Loaded CSS: ${url}`);
}

/** Fetch plugin element definitions and register them */
export async function loadPluginElements(): Promise<void> {
    // Phase 1: Go plugins — element defs announced via gRPC
    try {
        const resp = await apiFetch('/api/plugins/elements');
        if (!resp.ok) {
            log.warn(SEG.ELEMENT, `[PluginElements] Failed to fetch plugin elements: ${resp.status}`);
        } else {
            const defs: PluginElementDef[] = await resp.json();
            for (const def of defs) {
                registerPluginElementType(def);
            }
            log.info(SEG.ELEMENT, `[PluginElements] Loaded ${defs.length} plugin element type(s) from backend`);
        }
    } catch (err) {
        log.error(SEG.ELEMENT, '[PluginElements] Error loading plugin elements:', err);
    }

    // Phase 2: TS plugin modules — self-describing via elementDef export
    await discoverTSPluginModules();

    // Phase 3: element modules the node publishes, served same-origin from /g/
    await discoverPublishedElements();
}

/**
 * The watcher every node is born holding, which tells the pages in its
 * namespace that an element module was published.
 *
 * The same string as ats/watcher's StandingElementPublished. standing-watcher-id.test.ts
 * reads the Go source and fails if the two ever say different things.
 */
export const STANDING_ELEMENT_PUBLISHED = 'standing-element-published';

/** One element the node serves, as /g/ reports it. */
interface PublishedElement {
    name: string;
    as: string;
    url: string;
}

/** Which published module is the one currently registered, by element name. */
const publishedAs = new Map<string, string>();

/**
 * Why a published element is not on the canvas, by element name.
 *
 * A console line is only read by somebody who thought to open a console. The
 * element that is missing is the thing being looked at, so the reason is kept
 * here and drawn in its place.
 */
const whyAbsent = new Map<string, string>();

/** What went wrong the last time this element was asked for, if anything. */
export function elementAbsenceReason(name: string): string | undefined {
    return whyAbsent.get(name);
}

/** Say why, both to the log and to whatever draws in the element's place. */
function absent(name: string, why: string, err?: unknown): void {
    const said = err === undefined ? why : `${why}: ${err instanceof Error ? err.message : String(err)}`;
    whyAbsent.set(name, said);
    log.error(SEG.ELEMENT, `[Elements] ${name} ${said}`);
}

/**
 * Register every element the node publishes.
 *
 * Every failure here is a fault. The node said the element is published; being
 * unable to load it is never expected, and never a debug line.
 */
export async function discoverPublishedElements(): Promise<void> {
    let published: PublishedElement[];
    try {
        const resp = await apiFetch('/g/');
        if (!resp.ok) {
            log.error(SEG.ELEMENT, `[Elements] /g/ answered ${resp.status}; no published element is registered`);
            return;
        }
        const data: { elements?: PublishedElement[] } = await resp.json();
        published = data.elements ?? [];
    } catch (err) {
        log.error(SEG.ELEMENT, '[Elements] Could not list published elements; none are registered:', err);
        return;
    }

    for (const item of published) {
        if (publishedAs.get(item.name) === item.as) continue;

        // The attestation id is in the URL, so a published module is one the
        // browser has not imported and cannot answer from what it holds.
        const url = `${item.url}?v=${item.as}`;
        try {
            const raw: Record<string, unknown> = await importScript(url);
            const mod = (raw.default ?? raw) as ElementModule & { elementDef?: ElementDef };
            const def = mod.elementDef;

            if (!def) {
                absent(item.name, `is published as ${item.as} and exports no elementDef, so it cannot be placed`);
                continue;
            }
            if (typeof mod.render !== 'function') {
                absent(item.name, `is published as ${item.as} and exports no render, so it cannot be drawn`);
                continue;
            }

            await place(item, def, mod as ElementModule);
            whyAbsent.delete(item.name);
        } catch (err) {
            // The node published this. Failing to load it is a fault, and the
            // reason is the only thing that will ever say why it is absent.
            absent(item.name, `is published as ${item.as} and failed to load from ${url}`, err);
        }
    }
}

/** Put a published element where its own elementDef says it goes. */
async function place(item: PublishedElement, def: ElementDef, mod: ElementModule): Promise<void> {
    const name = item.name;

    if (def.form === 'panel') {
        const id = `element-${name}`;
        if (replacePanel(id, `${name} (${item.as})`, def, mod)) {
            publishedAs.set(name, item.as);
            return;
        }
        if (tray.has(id)) {
            // Nothing on this page put it there, so replacing it would take
            // over something this code does not own.
            log.error(SEG.ELEMENT, `[Elements] ${name} cannot take tray id ${id}; something else holds it`);
            return;
        }
        addPanel(id, name, def, mod);
        publishedAs.set(name, item.as);
        log.info(SEG.ELEMENT, `[Elements] ${name} (${def.symbol}) is in the tray, at ${item.as}`);
        return;
    }

    const entry = {
        symbol: def.symbol,
        className: `canvas-published-element element-${name}`,
        title: def.title,
        label: def.label,
        // Published, not a plugin. There is no process, no am.toml line and
        // nothing to enable — the module is an attestation this node serves.
        publishedName: name,
        render: async (canvasElement: Element) => {
            const ui = createElementUI(canvasElement, name);
            const rendered = await mod.render(canvasElement, ui);
            if (!rendered.dataset.elementId) {
                return wrapInCanvasPlaced(canvasElement, rendered, {
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

    if (getElementTypeBySymbol(def.symbol)) {
        if (!replacePluginElementType(entry)) {
            log.error(SEG.ELEMENT, `[Elements] ${name} claims ${def.symbol}, which is held by something else`);
            return;
        }
        publishedAs.set(name, item.as);
        log.info(SEG.ELEMENT, `[Elements] ${name} (${def.symbol}) reloaded, at ${item.as}`);
        // The entry is what the next render reads. What is drawn already came
        // from the module this one replaces, and catches up here.
        await redrawPlacedElements(def.symbol);
        return;
    }

    pluginSymbols.set(def.symbol, name);
    registerElementType(entry);
    publishedAs.set(name, item.as);
    log.info(SEG.ELEMENT, `[Elements] ${name} (${def.symbol}) is on the canvas, at ${item.as}`);
}

/**
 * Discover pure TS plugin modules by probing enabled plugins for an element module.
 *
 * For each enabled plugin, tries to import /api/{name}/element-module.js.
 * If the module exports an elementDef, it's a self-describing TS plugin — register it.
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
        // Discovery skipped means every TS plugin element is absent this
        // session — that must not look like there being none.
        log.error(SEG.ELEMENT, '[PluginElements] Could not list plugins; TS plugin elements are not registered:', err);
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
            ? `/api/${name}/element-module.js?v=${digest}`
            : `/api/${name}/element-module.js`;
        try {
            const raw: Record<string, unknown> = await import(/* @vite-ignore */ moduleUrl);
            const mod = (raw.default ?? raw) as ElementModule & { elementDef?: ElementDef };
            const def = mod.elementDef;
            if (!def || typeof mod.render !== 'function') continue;

            const cachedMod = mod as ElementModule;

            if (def.form === 'panel') {
                // A tray element is keyed by id, and discovery runs more than once per page.
                const id = `plugin-${name}`;
                if (livePanels.has(id)) {
                    // Only a new digest is news; the same module again is not.
                    if (!digest || registeredDigests.get(name) === digest) continue;
                    if (replacePanel(id, `${name} (${digest})`, def, cachedMod)) {
                        registeredDigests.set(name, digest);
                    }
                    continue;
                }
                if (tray.has(id)) continue;

                addPanel(id, name, def, cachedMod);
                if (digest) registeredDigests.set(name, digest);
                count++;
                log.info(SEG.ELEMENT, `[PluginElements] Discovered TS plugin panel: ${name} (${def.symbol})`);
                continue;
            }

            const entry = {
                symbol: def.symbol,
                className: `canvas-plugin-element plugin-${name}`,
                title: def.title,
                label: def.label,
                pluginName: name,
                render: async (item: Element) => {
                    const ui = createElementUI(item, name);
                    const rendered = await cachedMod.render(item, ui);
                    if (!rendered.dataset.elementId) {
                        return wrapInCanvasPlaced(item, rendered, {
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

            if (getElementTypeBySymbol(def.symbol)) {
                // Already registered — by the Go plugin path, or by this one
                // before the module was replaced. Only a new digest is news.
                if (!digest || registeredDigests.get(name) === digest) continue;
                if (!replacePluginElementType(entry)) continue;

                registeredDigests.set(name, digest);
                log.info(SEG.ELEMENT, `[PluginElements] Reloaded TS plugin module: ${name} (${def.symbol}) at ${digest}`);
                await redrawPlacedElements(def.symbol);
                continue;
            }

            pluginSymbols.set(def.symbol, name);
            registerElementType(entry);
            if (digest) registeredDigests.set(name, digest);
            count++;
            log.info(SEG.ELEMENT, `[PluginElements] Discovered TS plugin module: ${name} (${def.symbol})`);
        } catch (err) {
            // A plugin the node gave no digest for serves no module, and this
            // probe was always going to fail. One that has a digest was going
            // to work, so failing is a fault and never a line nobody reads.
            if (digest) {
                log.error(SEG.ELEMENT, `[PluginElements] ${name} publishes module ${digest} and it failed to load from ${moduleUrl}:`, err);
            } else {
                log.debug(SEG.ELEMENT, `[PluginElements] ${name} serves no TS module:`, err);
            }
        }
    }

    if (count > 0) {
        log.info(SEG.ELEMENT, `[PluginElements] Discovered ${count} TS plugin module(s)`);
    }
}

/**
 * The module a tray element is drawing from now, held apart from the Element so a
 * replacement reaches the panel that is already open.
 *
 * redraw is set while the panel holds an element and cleared when it lets go.
 */
interface LiveModule {
    mod: ElementModule;
    def: ElementDef;
    redraw: (() => void) | null;
}

/** The module behind each tray element this page put there, by its tray id. */
const livePanels = new Map<string, LiveModule>();

/**
 * Put a tray element on the newest module, redrawing it if it is open.
 *
 * Returns false when nothing on this page owns that id, which is a collision
 * rather than a replacement.
 */
function replacePanel(id: string, what: string, def: ElementDef, mod: ElementModule): boolean {
    const live = livePanels.get(id);
    if (!live) return false;

    live.mod = mod;
    live.def = def;
    if (!live.redraw) {
        log.info(SEG.ELEMENT, `[Elements] ${what} is in the tray and closed; it opens on the module just published`);
        return true;
    }
    live.redraw();
    log.info(SEG.ELEMENT, `[Elements] ${what} is open in the tray and was redrawn from the module just published`);
    return true;
}

// The tray's contract, tray.add, met by a module's render(). The ui it gets is the one a canvas element gets.
function makePanelElement(id: string, name: string, live: LiveModule): Element {
    let container: HTMLElement | null = null;

    // Whatever the module registered runs before its element is filled again,
    // so a redraw leaves nothing of the module it replaced behind.
    const draw = (el: HTMLElement): void => {
        runCleanup(el);
        const ui = createElementUI(item, name, el);
        Promise.resolve()
            .then(() => live.mod.render(item, ui))
            .then(rendered => {
                el.replaceChildren(rendered);
            })
            .catch((err: unknown) => {
                log.error(SEG.ELEMENT, `[PluginElements] ${name} panel render failed:`, err);
                el.replaceChildren(`${name}: ${err instanceof Error ? err.message : String(err)}`);
            });
    };

    const item: Element = {
        id,
        title: live.def.title,
        symbol: live.def.symbol,
        opensAs: 'panel',
        renderContent: () => {
            // The panel wants its element now; the module fills it when render() resolves.
            const el = document.createElement('div');
            el.className = `plugin-panel-element plugin-${name}`;
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
    return item;
}

/** Put an element in the tray for the first time, holding the module it draws from. */
function addPanel(id: string, name: string, def: ElementDef, mod: ElementModule): void {
    const live: LiveModule = { mod, def, redraw: null };
    livePanels.set(id, live);
    tray.add(makePanelElement(id, name, live));
}

function registerPluginElementType(def: PluginElementDef): void {
    // Track symbol → plugin name mapping for placeholder fallback
    pluginSymbols.set(def.symbol, def.plugin);

    // module_url → TypeScript SDK path; content_url → legacy HTML path
    const renderer = def.module_url
        ? (item: Element) => createPluginElementFromModule(item, def)
        : (item: Element) => createPluginElement(item, def);

    registerElementType({
        symbol: def.symbol,
        className: `canvas-plugin-element plugin-${def.plugin}`,
        title: def.title,
        label: def.label,
        pluginName: def.plugin,
        render: renderer,
    });
}
