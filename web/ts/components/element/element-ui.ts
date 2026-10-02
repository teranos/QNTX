/**
 * ElementUI — host-side factory that creates ElementUI instances.
 * Types are canonical in @teranos/elements; this file owns the QNTX-specific factory.
 */

import type { Element, ElementUI, ElementOpts, FetchOpts, MeldEvent, SpawnResultDetail, AttestationQuery, Attestation } from '@teranos/elements';
import { canvasPlaced } from '@teranos/elements';
import type { CanvasPlacedConfig } from '@teranos/elements';
import { preventDrag, storeCleanup, createInput, createButton, createStatusLine, wireExpandToWindow } from '@teranos/elements';
import { apiFetch, apiJson, backendWsUrl, backendUrl } from '../../client';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { createAutoSave } from './element-autosave';

// Re-export types so existing consumers don't break
export type { RenderFn, ElementModule, ElementDef, ElementUI, ElementOpts, FetchOpts, MeldEvent, SpawnResultDetail, AttestationQuery, Attestation } from '@teranos/elements';

// The node's query keys, in its own spelling — nothing else on the query reaches it.
const ATTESTATION_QUERY_KEYS = ['subject', 'predicate', 'context', 'actor', 'source', 'limit'] as const;

/** The ⬆ in an element's title bar. The same one the result element carries. */
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
 * Create a ElementUI instance scoped to a specific element.
 * `root` is for an element whose element the host already owns (a tray panel's
 * content): cleanups are stored on it at once, and whoever discards it runs them.
 */
export function createElementUI(item: Element, name: string, root?: HTMLElement): ElementUI {
    // Element reference — set when container() is called
    let rootElement: HTMLElement | null = root ?? null;
    // Cleanups registered before container() — flushed when container is created
    const pendingCleanups: Array<() => void> = [];

    // What saveContent last wrote, and the debounce that carries it. Made on
    // first use: an element that never saves pays for no timer.
    let pending = '';
    let saveContent: ReturnType<typeof createAutoSave> | null = null;

    const prefix = `[${name}]`;

    const ui: ElementUI = {
        element(opts: ElementOpts) {
            // The lift off the canvas: the button that makes a placed element a
            // window and puts it back. It belongs to the canvas rather than to
            // each element, and a module that builds its own frame was the one
            // path that did not get it.
            const lift = opts.titleBar && opts.lift !== false ? liftButton() : null;
            const titleBar = opts.titleBar && lift
                ? { ...opts.titleBar, actions: [...(opts.titleBar.actions ?? []), lift] }
                : opts.titleBar;

            const config: CanvasPlacedConfig = {
                item: item,
                className: opts.className ?? `canvas-element element-${name}`,
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
                const label = result.titleBar.querySelector('span:not(.symbol)') as HTMLElement | null;
                if (label) label.style.color = opts.titleBar.labelColor;
            }

            // Flush any cleanups registered before container() was called
            for (const fn of pendingCleanups) {
                storeCleanup(rootElement, fn);
            }
            pendingCleanups.length = 0;

            // Create the content area — scrollable body below the title bar
            const content = document.createElement('div');
            content.className = 'content-area';
            rootElement.appendChild(content);

            if (lift) {
                // The window is drawn from the same content this returns, so
                // what a module put on the canvas is what lifts off it.
                wireExpandToWindow({
                    element: rootElement,
                    expandBtn: lift,
                    elementId: item.id,
                    title: opts.titleBar?.label ?? name,
                    symbol: item.symbol ?? '',
                    renderContent: () => content,
                    logLabel: name,
                    // No colours handed along: the window is this element
                    // morphed and the tray reparents the same one, so what it
                    // was painted stays painted (Element Axioma).
                    adoptExtras: { opensAs: 'window' as const },
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
                log.debug(SEG.ELEMENT, `${prefix} ${msg}`, ...args);
            },
            info(msg: string, ...args: unknown[]) {
                log.info(SEG.ELEMENT, `${prefix} ${msg}`, ...args);
            },
            warn(msg: string, ...args: unknown[]) {
                log.warn(SEG.ELEMENT, `${prefix} ${msg}`, ...args);
            },
            error(msg: string, ...args: unknown[]) {
                log.error(SEG.ELEMENT, `${prefix} ${msg}`, ...args);
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
            return uiState.getCanvasElement(item.id)?.content;
        },

        saveContent(content: string): void {
            // Through the same debounced save every built-in uses, so a
            // published element that saves as you type costs the canvas no more
            // than one that lives in the shell.
            if (!saveContent) {
                saveContent = createAutoSave(item.id, () => pending, name);
                // The element may close mid-debounce; the cancel keeps a write
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
                    if (edge.from === item.id || edge.to === item.id) {
                        seenEdges.add(`${edge.from}-${edge.direction}-${edge.to}`);
                    }
                }
            }

            const unsubscribe = uiState.subscribe('canvasCompositions', (comps) => {
                for (const comp of comps) {
                    for (const edge of comp.edges) {
                        // Only care about edges where this element is the target
                        if (edge.to !== item.id) continue;

                        const edgeKey = `${edge.from}-${edge.direction}-${edge.to}`;
                        if (seenEdges.has(edgeKey)) continue;
                        seenEdges.add(edgeKey);

                        // Look up the melded element's data
                        const canvasElements = uiState.getCanvasElements();
                        const melded = canvasElements.find(g => g.id === edge.from);

                        callback({
                            elementId: edge.from,
                            symbol: melded?.symbol ?? '',
                            direction: edge.direction,
                            content: melded?.content ?? '',
                        });

                        log.info(SEG.ELEMENT, `${prefix} Meld received from ${edge.from} (${edge.direction})`);
                    }
                }
            });

            ui.onCleanup(unsubscribe);
            return unsubscribe;
        },

        async loadConfig(): Promise<Record<string, unknown> | null> {
            const resp = await apiFetch(
                `/api/element-config?plugin=${encodeURIComponent(name)}&element_id=${encodeURIComponent(item.id)}`
            );
            if (!resp.ok) return null;
            const data = await resp.json();
            return data.config ?? null;
        },

        async saveConfig(config: Record<string, unknown>): Promise<void> {
            await apiFetch('/api/element-config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ plugin: name, element_id: item.id, config }),
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
                log.error(SEG.ELEMENT, `${prefix} spawnResult called before element() — no root element`);
                return;
            }
            const detail: SpawnResultDetail = { elementId: item.id, name, result };
            const CE = rootElement.ownerDocument.defaultView?.CustomEvent ?? CustomEvent;
            rootElement.dispatchEvent(new CE('element:spawn-result', {
                bubbles: true,
                detail,
            }));
        },
    };

    return ui;
}
