/**
 * GlyphUI — type definitions for building glyphs.
 */

import type { Glyph } from './glyph';

// ── Render contract ──────────────────────────────────────────────────

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

// ── UI interface ─────────────────────────────────────────────────────

/** UI interface injected into glyph render functions. */
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
     * Fetch from this glyph's HTTP endpoints.
     * Path is relative to /api/{name}/ — e.g., pluginFetch('/execute', ...).
     */
    pluginFetch(path: string, opts?: FetchOpts): Promise<Response>;

    /** Structured logging with [{name}] prefix. */
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
     * Open a WebSocket to this glyph's WS endpoint.
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

    /**
     * Spawn a result glyph below this glyph on the canvas.
     * Fires a DOM event — the canvas workspace handles positioning, state, and meld.
     */
    spawnResult(result: SpawnResultDetail['result']): void;
}

// ── Supporting types ─────────────────────────────────────────────────

/** Detail payload for the glyph:spawn-result DOM event. */
export interface SpawnResultDetail {
    glyphId: string;
    name: string;
    result: {
        success: boolean;
        stdout: string;
        stderr: string;
        result: unknown;
        error: string | null;
        duration_ms: number;
    };
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
    titleBar?: { label: string; actions?: HTMLElement[]; color?: string; labelColor?: string };
    resizable?: boolean | { minWidth?: number; minHeight?: number };
    className?: string;
    /** Custom drag handle element. Falls back to title bar, then container. */
    dragHandle?: HTMLElement;
    /** Extra options forwarded to makeDraggable (e.g. ignoreButtons). */
    draggableOptions?: Partial<MakeDraggableOptions>;
    /** Use minHeight instead of fixed height (for auto-sizing glyphs). */
    useMinHeight?: boolean;
}

export interface FetchOpts {
    method?: string;
    body?: unknown;
    headers?: Record<string, string>;
}

export interface MakeDraggableOptions {
    /** When true the drag is cancelled if the mousedown target is a <button>. */
    ignoreButtons?: boolean;
    /** Label used in log messages, e.g. "PyGlyph". */
    logLabel?: string;
    /** The prompt glyph object (if this is a prompt being made draggable) */
    promptGlyph?: Glyph;
}
