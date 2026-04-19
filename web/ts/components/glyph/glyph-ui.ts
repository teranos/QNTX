/**
 * GlyphUI — host-side factory that creates GlyphUI instances.
 * Types are canonical in @qntx/glyphs; this file owns the QNTX-specific factory.
 */

import type { Glyph, GlyphUI, GlyphOpts, FetchOpts, MeldEvent, SpawnResultDetail } from '@qntx/glyphs';
import { canvasPlaced, type CanvasPlacedConfig } from './manifestations/canvas-placed';
import { preventDrag, storeCleanup } from '@qntx/glyphs';
import { apiFetch, getBackendUrl } from '../../api';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';

// Re-export types so existing consumers don't break
export type { RenderFn, GlyphModule, GlyphDef, GlyphUI, GlyphOpts, FetchOpts, MeldEvent, SpawnResultDetail } from '@qntx/glyphs';

// ── Factory ─────────────────────────────────────────────────────────

/** Create a GlyphUI instance scoped to a specific glyph. */
export function createGlyphUI(glyph: Glyph, name: string): GlyphUI {
    // Element reference — set when container() is called
    let rootElement: HTMLElement | null = null;
    // Cleanups registered before container() — flushed when container is created
    const pendingCleanups: Array<() => void> = [];

    const prefix = `[${name}]`;

    const ui: GlyphUI = {
        glyph(opts: GlyphOpts) {
            const config: CanvasPlacedConfig = {
                glyph,
                className: opts.className ?? `canvas-glyph glyph-${name}`,
                defaults: opts.defaults,
                titleBar: opts.titleBar,
                dragHandle: opts.dragHandle,
                draggableOptions: opts.draggableOptions,
                resizable: opts.resizable ?? false,
                useMinHeight: opts.useMinHeight,
                logLabel: name,
            };

            const result = canvasPlaced(config);
            rootElement = result.element;

            if (opts.titleBar?.color && result.titleBar) {
                result.titleBar.style.backgroundColor = opts.titleBar.color;
            }
            if (opts.titleBar?.labelColor && result.titleBar) {
                const label = result.titleBar.querySelector('span:first-child') as HTMLElement | null;
                if (label) label.style.color = opts.titleBar.labelColor;
            }

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
            const url = `/api/${name}${path}`;
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
            return new WebSocket(`${base}/ws/${name}${qs}`);
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
            el.style.fontFamily = 'monospace';
            el.style.fontSize = 'var(--font-size-xs, 10px)';
            el.style.minHeight = '16px';
            el.style.lineHeight = '16px';
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
                `/api/glyph-config?plugin=${encodeURIComponent(name)}&glyph_id=${encodeURIComponent(glyph.id)}`
            );
            if (!resp.ok) return null;
            const data = await resp.json();
            return data.config ?? null;
        },

        async saveConfig(config: Record<string, unknown>): Promise<void> {
            await apiFetch('/api/glyph-config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ plugin: name, glyph_id: glyph.id, config }),
            });
        },

        spawnResult(result) {
            if (!rootElement) {
                log.error(SEG.GLYPH, `${prefix} spawnResult called before glyph() — no root element`);
                return;
            }
            const detail: SpawnResultDetail = { glyphId: glyph.id, name, result };
            const CE = rootElement.ownerDocument.defaultView?.CustomEvent ?? CustomEvent;
            rootElement.dispatchEvent(new CE('glyph:spawn-result', {
                bubbles: true,
                detail,
            }));
        },
    };

    return ui;
}
