/**
 * GlyphUI — the interface plugins use to build their glyphs.
 *
 * Instead of serving raw HTML from Go, plugins ship a TypeScript module
 * that exports a render function. The frontend dynamically imports it
 * and injects this UI interface, giving the plugin type-safe access to QNTX
 * primitives: canvasPlaced, drag protection, plugin fetch, logging, cleanup.
 *
 * Usage from a plugin module:
 *
 *   import type { GlyphUI, RenderFn } from '@qntx/glyphs';
 *
 *   export const render: RenderFn = (glyph, ui) => {
 *       const { element, content } = ui.glyph({
 *           defaults: { x: 200, y: 200, width: 600, height: 700 },
 *           titleBar: { label: 'My Plugin' },
 *           resizable: true,
 *       });
 *
 *       const input = ui.input({ placeholder: 'Enter URL...' });
 *       content.appendChild(input);
 *
 *       return element;
 *   };
 */

import type { Glyph } from './glyph';
import { canvasPlaced, type CanvasPlacedConfig } from './manifestations/canvas-placed';
import { preventDrag, storeCleanup } from './glyph-interaction';
import { apiFetch, getBackendUrl } from '../../api';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';

// ── Public types (plugin-facing) ────────────────────────────────────

/** The render function a plugin module must export. */
export type RenderFn = (glyph: Glyph, ui: GlyphUI) => HTMLElement | Promise<HTMLElement>;

/** Plugin module shape — the default or named export. */
export interface GlyphModule {
    render: RenderFn;
    glyphDef?: GlyphDef;
}

/** Self-describing metadata exported by pure TS plugin modules. */
export interface GlyphDef {
    symbol: string;
    title: string;
    label: string;
    defaultWidth?: number;
    defaultHeight?: number;
}

/** UI interface injected into plugin render functions. */
export interface GlyphUI {
    /**
     * Create a canvas-placed glyph with title bar, drag, and resize.
     * Returns a content area — the scrollable body below the title bar.
     * Append plugin content into `content`, not `element`.
     */
    glyph(opts: GlyphOpts): { element: HTMLElement; titleBar: HTMLElement | null; content: HTMLElement };

    /** Prevent drag from starting on interactive children. */
    preventDrag(...elements: HTMLElement[]): void;

    /**
     * Fetch from this plugin's HTTP endpoints.
     * Path is relative to /api/{plugin}/ — e.g., pluginFetch('/test-fetch', ...).
     */
    pluginFetch(path: string, opts?: FetchOpts): Promise<Response>;

    /** Structured logging with [Plugin:{name}] prefix. */
    log: {
        debug(msg: string, ...args: unknown[]): void;
        info(msg: string, ...args: unknown[]): void;
        warn(msg: string, ...args: unknown[]): void;
        error(msg: string, ...args: unknown[]): void;
    };

    /** Register a cleanup function called when the glyph is removed. */
    onCleanup(fn: () => void): void;

    /** Create a text input with drag protection already applied. */
    input(opts?: { label?: string; placeholder?: string; value?: string; type?: string }): HTMLElement;

    /** Create a button. */
    button(opts: { label: string; onClick: () => void; primary?: boolean }): HTMLButtonElement;

    /**
     * Create a status line for showing feedback messages.
     * TODO: Weak design element — useful concept (contextual feedback next to the
     * action that caused it) but visually underwhelming. Rethink the presentation.
     */
    statusLine(): { element: HTMLElement; show(msg: string, isError?: boolean): void; clear(): void };

    /**
     * Open a WebSocket to this plugin's WS endpoint.
     * Constructs the full URL from backend config — no hardcoded ports.
     */
    pluginWebSocket(params?: Record<string, string>): WebSocket;

    /**
     * Subscribe to meld events — called when another glyph melds onto this one.
     * Returns unsubscribe function.
     */
    onMeld(callback: (event: MeldEvent) => void): () => void;

    /** Load this glyph's persisted config from the server. Returns null if no config saved. */
    loadConfig(): Promise<Record<string, unknown> | null>;

    /** Save config for this glyph to the server. */
    saveConfig(config: Record<string, unknown>): Promise<void>;
}

/** Data passed to onMeld callbacks when a glyph melds onto this one. */
export interface MeldEvent {
    /** ID of the glyph that melded onto this one */
    glyphId: string;
    /** Symbol of the melded glyph */
    symbol: string;
    /** Direction the meld came from (the edge direction) */
    direction: string;
    /** Content of the melded glyph (source code, URL, markdown, etc.) */
    content: string;
}

export interface GlyphOpts {
    defaults: { x: number; y: number; width: number; height: number };
    titleBar?: { label: string; actions?: HTMLElement[] };
    resizable?: boolean | { minWidth?: number; minHeight?: number };
    className?: string;
    /** Custom drag handle element. Falls back to title bar, then container. */
    dragHandle?: HTMLElement;
    /** Extra options forwarded to makeDraggable (e.g. ignoreButtons). */
    draggableOptions?: Partial<import('./glyph-interaction').MakeDraggableOptions>;
    /** Use minHeight instead of fixed height (for auto-sizing glyphs). */
    useMinHeight?: boolean;
}

export interface FetchOpts {
    method?: string;
    body?: unknown;
    headers?: Record<string, string>;
}

// ── Factory ─────────────────────────────────────────────────────────

/** Create a GlyphUI instance scoped to a specific glyph and plugin. */
export function createGlyphUI(glyph: Glyph, pluginName: string): GlyphUI {
    // Element reference — set when container() is called
    let rootElement: HTMLElement | null = null;
    // Cleanups registered before container() — flushed when container is created
    const pendingCleanups: Array<() => void> = [];

    const prefix = `[Plugin:${pluginName}]`;

    const ui: GlyphUI = {
        glyph(opts: GlyphOpts) {
            const config: CanvasPlacedConfig = {
                glyph,
                className: opts.className ?? `canvas-plugin-glyph plugin-${pluginName}`,
                defaults: opts.defaults,
                titleBar: opts.titleBar,
                dragHandle: opts.dragHandle,
                draggableOptions: opts.draggableOptions,
                resizable: opts.resizable ?? false,
                useMinHeight: opts.useMinHeight,
                logLabel: `Plugin:${pluginName}`,
            };

            const result = canvasPlaced(config);
            rootElement = result.element;

            // Flush any cleanups registered before container() was called
            for (const fn of pendingCleanups) {
                storeCleanup(rootElement, fn);
            }
            pendingCleanups.length = 0;

            // Create the content area — scrollable body below the title bar
            const content = document.createElement('div');
            content.className = 'glyph-content-area';
            rootElement.appendChild(content);

            return { ...result, content };
        },

        preventDrag(...elements: HTMLElement[]) {
            preventDrag(...elements);
        },

        async pluginFetch(path: string, opts?: FetchOpts): Promise<Response> {
            const url = `/api/${pluginName}${path}`;
            const init: RequestInit = {};

            if (opts?.method) init.method = opts.method;
            if (opts?.body) {
                init.body = JSON.stringify(opts.body);
                init.headers = { 'Content-Type': 'application/json', ...(opts.headers ?? {}) };
            } else if (opts?.headers) {
                init.headers = opts.headers;
            }

            return apiFetch(url, init);
        },

        pluginWebSocket(params?: Record<string, string>): WebSocket {
            const base = getBackendUrl().replace(/^http/, 'ws');
            const qs = params ? '?' + new URLSearchParams(params).toString() : '';
            return new WebSocket(`${base}/ws/${pluginName}${qs}`);
        },

        log: {
            debug(msg: string, ...args: unknown[]) {
                log.debug(SEG.GLYPH, `${prefix} ${msg}`, ...args);
            },
            info(msg: string, ...args: unknown[]) {
                log.info(SEG.GLYPH, `${prefix} ${msg}`, ...args);
            },
            warn(msg: string, ...args: unknown[]) {
                log.warn(SEG.GLYPH, `${prefix} ${msg}`, ...args);
            },
            error(msg: string, ...args: unknown[]) {
                log.error(SEG.GLYPH, `${prefix} ${msg}`, ...args);
            },
        },

        onCleanup(fn: () => void) {
            if (rootElement) {
                storeCleanup(rootElement, fn);
            } else {
                pendingCleanups.push(fn);
            }
        },

        input(opts) {
            const wrapper = document.createElement('div');
            wrapper.className = 'glyph-form-group';

            if (opts?.label) {
                const label = document.createElement('label');
                label.className = 'glyph-label';
                label.textContent = opts.label;
                wrapper.appendChild(label);
            }

            const input = document.createElement('input');
            input.className = 'glyph-input';
            input.type = opts?.type ?? 'text';
            if (opts?.placeholder) input.placeholder = opts.placeholder;
            if (opts?.value) input.value = opts.value;

            preventDrag(input);
            wrapper.appendChild(input);
            return wrapper;
        },

        button(opts) {
            const btn = document.createElement('button');
            btn.className = opts.primary ? 'glyph-btn glyph-btn--primary' : 'glyph-btn';
            btn.textContent = opts.label;
            btn.addEventListener('click', opts.onClick);
            preventDrag(btn);
            return btn;
        },

        statusLine() {
            const el = document.createElement('div');
            el.className = 'glyph-status';
            let timer: ReturnType<typeof setTimeout> | null = null;

            return {
                element: el,
                show(msg: string, isError = false) {
                    el.textContent = msg;
                    el.style.color = isError ? 'var(--color-error, #ef4444)' : 'var(--color-success, #22c55e)';
                    if (timer) clearTimeout(timer);
                    if (!isError) {
                        timer = setTimeout(() => { el.textContent = ''; }, 4000);
                    }
                },
                clear() {
                    if (timer) clearTimeout(timer);
                    el.textContent = '';
                },
            };
        },

        onMeld(callback: (event: MeldEvent) => void): () => void {
            // Track edges we've already seen so we only fire for new melds
            const seenEdges = new Set<string>();

            // Seed with current edges (don't fire for pre-existing melds)
            const compositions = uiState.getCanvasCompositions();
            for (const comp of compositions) {
                for (const edge of comp.edges) {
                    if (edge.from === glyph.id || edge.to === glyph.id) {
                        seenEdges.add(`${edge.from}-${edge.direction}-${edge.to}`);
                    }
                }
            }

            const unsubscribe = uiState.subscribe('canvasCompositions', (comps) => {
                for (const comp of comps) {
                    for (const edge of comp.edges) {
                        // Only care about edges where this glyph is the target
                        if (edge.to !== glyph.id) continue;

                        const edgeKey = `${edge.from}-${edge.direction}-${edge.to}`;
                        if (seenEdges.has(edgeKey)) continue;
                        seenEdges.add(edgeKey);

                        // Look up the melded glyph's data
                        const canvasGlyphs = uiState.getCanvasGlyphs();
                        const melded = canvasGlyphs.find(g => g.id === edge.from);

                        callback({
                            glyphId: edge.from,
                            symbol: melded?.symbol ?? '',
                            direction: edge.direction,
                            content: melded?.content ?? '',
                        });

                        log.info(SEG.GLYPH, `${prefix} Meld received from ${edge.from} (${edge.direction})`);
                    }
                }
            });

            ui.onCleanup(unsubscribe);
            return unsubscribe;
        },

        async loadConfig(): Promise<Record<string, unknown> | null> {
            const resp = await apiFetch(
                `/api/glyph-config?plugin=${encodeURIComponent(pluginName)}&glyph_id=${encodeURIComponent(glyph.id)}`
            );
            if (!resp.ok) return null;
            const data = await resp.json();
            return data.config ?? null;
        },

        async saveConfig(config: Record<string, unknown>): Promise<void> {
            await apiFetch('/api/glyph-config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ plugin: pluginName, glyph_id: glyph.id, config }),
            });
        },
    };

    return ui;
}
