/**
 * Plugin Glyph SDK — the interface plugins use to build their glyphs.
 *
 * Instead of serving raw HTML from Go, plugins ship a TypeScript module
 * that exports a render function. The frontend dynamically imports it
 * and injects this SDK, giving the plugin type-safe access to QNTX
 * primitives: canvasPlaced, drag protection, plugin fetch, logging, cleanup.
 *
 * Usage from a plugin module:
 *
 *   import type { PluginGlyphSDK, PluginRenderFn } from '@qntx/glyph-sdk';
 *
 *   export const render: PluginRenderFn = (glyph, sdk) => {
 *       const { element } = sdk.container({
 *           defaults: { x: 200, y: 200, width: 600, height: 700 },
 *           titleBar: { label: 'My Plugin' },
 *           resizable: true,
 *       });
 *
 *       const input = sdk.input({ placeholder: 'Enter URL...' });
 *       element.appendChild(input);
 *
 *       return element;
 *   };
 */

import type { Glyph } from './glyph';
import { canvasPlaced, type CanvasPlacedConfig } from './manifestations/canvas-placed';
import { preventDrag, storeCleanup } from './glyph-interaction';
import { apiFetch } from '../../api';
import { log, SEG } from '../../logger';

// ── Public types (plugin-facing) ────────────────────────────────────

/** The render function a plugin module must export. */
export type PluginRenderFn = (glyph: Glyph, sdk: PluginGlyphSDK) => HTMLElement | Promise<HTMLElement>;

/** Plugin module shape — the default or named export. */
export interface PluginGlyphModule {
    render: PluginRenderFn;
}

/** SDK injected into plugin render functions. */
export interface PluginGlyphSDK {
    /**
     * Create a canvas-placed container with title bar, drag, and resize.
     * This is the root element — the plugin appends its content into it.
     */
    container(opts: PluginContainerOpts): { element: HTMLElement; titleBar: HTMLElement | null };

    /** Prevent drag from starting on interactive children. */
    preventDrag(...elements: HTMLElement[]): void;

    /**
     * Fetch from this plugin's HTTP endpoints.
     * Path is relative to /api/{plugin}/ — e.g., pluginFetch('/test-fetch', ...).
     */
    pluginFetch(path: string, opts?: PluginFetchOpts): Promise<Response>;

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

    /** Create a status line for showing feedback messages. */
    statusLine(): { element: HTMLElement; show(msg: string, isError?: boolean): void; clear(): void };

    /** Load this glyph's persisted config from the server. Returns null if no config saved. */
    loadConfig(): Promise<Record<string, unknown> | null>;

    /** Save config for this glyph to the server. */
    saveConfig(config: Record<string, unknown>): Promise<void>;
}

export interface PluginContainerOpts {
    defaults: { x: number; y: number; width: number; height: number };
    titleBar?: { label: string; actions?: HTMLElement[] };
    resizable?: boolean | { minWidth?: number; minHeight?: number };
    className?: string;
}

export interface PluginFetchOpts {
    method?: string;
    body?: unknown;
    headers?: Record<string, string>;
}

// ── SDK factory ─────────────────────────────────────────────────────

/** Create an SDK instance scoped to a specific glyph and plugin. */
export function createPluginSDK(glyph: Glyph, pluginName: string): PluginGlyphSDK {
    // Element reference — set when container() is called
    let rootElement: HTMLElement | null = null;
    // Cleanups registered before container() — flushed when container is created
    const pendingCleanups: Array<() => void> = [];

    const prefix = `[Plugin:${pluginName}]`;

    const sdk: PluginGlyphSDK = {
        container(opts: PluginContainerOpts) {
            const config: CanvasPlacedConfig = {
                glyph,
                className: opts.className ?? `canvas-plugin-glyph plugin-${pluginName}`,
                defaults: opts.defaults,
                titleBar: opts.titleBar,
                resizable: opts.resizable ?? false,
                logLabel: `Plugin:${pluginName}`,
            };

            const result = canvasPlaced(config);
            rootElement = result.element;

            // Flush any cleanups registered before container() was called
            for (const fn of pendingCleanups) {
                storeCleanup(rootElement, fn);
            }
            pendingCleanups.length = 0;

            return result;
        },

        preventDrag(...elements: HTMLElement[]) {
            preventDrag(...elements);
        },

        async pluginFetch(path: string, opts?: PluginFetchOpts): Promise<Response> {
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
            wrapper.className = 'plugin-sdk-form-group';

            if (opts?.label) {
                const label = document.createElement('label');
                label.className = 'plugin-sdk-label';
                label.textContent = opts.label;
                wrapper.appendChild(label);
            }

            const input = document.createElement('input');
            input.className = 'plugin-sdk-input';
            input.type = opts?.type ?? 'text';
            if (opts?.placeholder) input.placeholder = opts.placeholder;
            if (opts?.value) input.value = opts.value;

            preventDrag(input);
            wrapper.appendChild(input);
            return wrapper;
        },

        button(opts) {
            const btn = document.createElement('button');
            btn.className = opts.primary ? 'plugin-sdk-btn plugin-sdk-btn--primary' : 'plugin-sdk-btn';
            btn.textContent = opts.label;
            btn.addEventListener('click', opts.onClick);
            preventDrag(btn);
            return btn;
        },

        statusLine() {
            const el = document.createElement('div');
            el.className = 'plugin-sdk-status';
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

    return sdk;
}
