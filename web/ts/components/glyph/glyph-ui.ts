/**
 * GlyphUI — host-side factory that creates GlyphUI instances.
 * Types are canonical in @qntx/glyphs; this file owns the QNTX-specific factory.
 */

import type { Glyph, GlyphUI, GlyphOpts, FetchOpts, MeldEvent, SpawnResultDetail, AttestationQuery, Attestation } from '@qntx/glyphs';
import { canvasPlaced } from '@qntx/glyphs';
import type { CanvasPlacedConfig } from '@qntx/glyphs';
import { preventDrag, storeCleanup, createInput, createButton, createStatusLine, wireExpandToWindow } from '@qntx/glyphs';
import { apiFetch, apiJson, backendWsUrl, backendUrl } from '../../client';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { createAutoSave } from './glyph-autosave';

// Re-export types so existing consumers don't break
export type { RenderFn, GlyphModule, GlyphDef, GlyphUI, GlyphOpts, FetchOpts, MeldEvent, SpawnResultDetail, AttestationQuery, Attestation } from '@qntx/glyphs';

// The node's query keys, in its own spelling — nothing else on the query reaches it.
const ATTESTATION_QUERY_KEYS = ['subject', 'predicate', 'context', 'actor', 'source', 'limit'] as const;

/** The ⬆ in a glyph's title bar. The same one the result glyph carries. */
function liftButton(): HTMLButtonElement {
    const lift = document.createElement('button');
    lift.textContent = '⬆'; // ⬆
    lift.title = 'Expand to window';
    lift.setAttribute('aria-label', 'Expand to window');
    preventDrag(lift);
    return lift;
}

// ── Factory ─────────────────────────────────────────────────────────

/**
 * Create a GlyphUI instance scoped to a specific glyph.
 * `root` is for a glyph whose element the host already owns (a tray panel's
 * content): cleanups are stored on it at once, and whoever discards it runs them.
 */
export function createGlyphUI(glyph: Glyph, name: string, root?: HTMLElement): GlyphUI {
    // Element reference — set when container() is called
    let rootElement: HTMLElement | null = root ?? null;
    // Cleanups registered before container() — flushed when container is created
    const pendingCleanups: Array<() => void> = [];

    // What saveContent last wrote, and the debounce that carries it. Made on
    // first use: a glyph that never saves pays for no timer.
    let pending = '';
    let saveContent: ReturnType<typeof createAutoSave> | null = null;

    const prefix = `[${name}]`;

    const ui: GlyphUI = {
        glyph(opts: GlyphOpts) {
            // The lift off the canvas: the button that makes a placed glyph a
            // window and puts it back. It belongs to the canvas rather than to
            // each glyph, and a module that builds its own frame was the one
            // path that did not get it.
            const lift = opts.titleBar && opts.lift !== false ? liftButton() : null;
            const titleBar = opts.titleBar && lift
                ? { ...opts.titleBar, actions: [...(opts.titleBar.actions ?? []), lift] }
                : opts.titleBar;

            const config: CanvasPlacedConfig = {
                glyph,
                className: opts.className ?? `canvas-glyph glyph-${name}`,
                defaults: opts.defaults,
                titleBar,
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
                // The title bar holds a symbol span and a label span; color the label
                const label = result.titleBar.querySelector('span:not(.glyph-symbol)') as HTMLElement | null;
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

            if (lift) {
                // The window is drawn from the same content this returns, so
                // what a module put on the canvas is what lifts off it.
                wireExpandToWindow({
                    element: rootElement,
                    expandBtn: lift,
                    glyphId: glyph.id,
                    title: opts.titleBar?.label ?? name,
                    symbol: glyph.symbol ?? '',
                    renderContent: () => content,
                    logLabel: name,
                    // No colours handed along: the window is this element
                    // morphed and the tray reparents the same one, so what it
                    // was painted stays painted (Element Axioma).
                    adoptExtras: { manifestationType: 'window' as const },
                });
            }

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
            const base = backendWsUrl();
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

        // DOM building blocks are package-owned; only I/O lives here
        input: createInput,
        button: createButton,
        statusLine: createStatusLine,

        nodeUrl(path: string): string {
            return `${backendUrl()}${path.startsWith('/') ? path : '/' + path}`;
        },

        content(): string | undefined {
            return uiState.getCanvasGlyph(glyph.id)?.content;
        },

        saveContent(content: string): void {
            // Through the same debounced save every built-in uses, so a
            // published glyph that saves as you type costs the canvas no more
            // than one that lives in the shell.
            if (!saveContent) {
                saveContent = createAutoSave(glyph.id, () => pending, name);
                // The glyph may close mid-debounce; the cancel keeps a write
                // from landing on a canvas that has moved on.
                ui.onCleanup(() => saveContent?.cancel());
            }
            pending = content;
            saveContent.save();
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

        async attestations(query: AttestationQuery): Promise<Attestation[]> {
            const params = new URLSearchParams();
            for (const key of ATTESTATION_QUERY_KEYS) {
                const value = query[key];
                if (value !== undefined) params.set(key, String(value));
            }
            const qs = params.toString();
            // apiJson rejects on a non-ok response with the status and body
            return apiJson<Attestation[]>(`/api/attestations${qs ? '?' + qs : ''}`);
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
