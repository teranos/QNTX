/**
 * @qntx/glyphs configuration
 *
 * Dependency injection for host-specific implementations.
 * Call configureGlyphs() at startup to wire in your app's logger,
 * persistence layer, and HTML stripping.
 *
 * Defaults are safe no-ops so the package works standalone.
 */

import type { CompositionState } from './composition';

export interface GlyphLogger {
    debug(segment: string, message: string, metadata?: Record<string, unknown>): void;
    info(segment: string, message: string, metadata?: Record<string, unknown>): void;
    warn(segment: string, message: string, metadata?: Record<string, unknown>): void;
    error(segment: string, message: string, metadata?: Record<string, unknown>): void;
}

export interface GlyphPersistence {
    /** Get list of minimized glyph IDs */
    getMinimizedGlyphs(): string[];
    /** Persist a glyph as minimized */
    addMinimizedGlyph(id: string): void;
    /** Remove a glyph from minimized list */
    removeMinimizedGlyph(id: string): void;
}

/** Glyph position and dimensions on a canvas. */
export interface CanvasGlyphData {
    id: string;
    symbol: string;
    x: number;
    y: number;
    width?: number;
    height?: number;
    content?: string;
    canvas_id?: string;
}

/** Host-provided canvas state — persistence, transform, selection, sync. */
export interface CanvasHost {
    saveCanvasGlyph(glyph: CanvasGlyphData): void;
    getCanvasGlyphs(canvasId?: string): CanvasGlyphData[];
    getTransform(canvasId: string): { panX: number; panY: number; scale: number };
    getSelectedGlyphIds(canvasId: string): string[];
    isGlyphSelected(canvasId: string, glyphId: string): boolean;
    saveComposition(composition: CompositionState): void;
    removeComposition(id: string): void;
    findCompositionByGlyph(glyphId: string): CompositionState | null;
    flushSync(): void;
}

/** Coordinate transforms between canvas-local and screen-space. */
export interface CanvasCoordinateBridge {
    toScreen(canvasId: string, x: number, y: number): { x: number; y: number };
    fromScreen(canvasId: string, x: number, y: number): { x: number; y: number };
    getScale(canvasId: string): number;
}

export interface GlyphConfig {
    logger?: GlyphLogger;
    logSegment?: string;
    persistence?: GlyphPersistence;
    stripHtml?: (html: string) => string;
    /** Canvas coordinate transforms — required for canvas-window morphs. */
    canvas?: CanvasCoordinateBridge;
    /** Canvas host — persistence, transform, selection, composition CRUD. */
    canvasHost?: CanvasHost;
    /** Called when a glyph is removed from the canvas (close/minimize). */
    removeCanvasGlyph?: (glyphId: string) => void;
}

// Default no-op logger
const noopLogger: GlyphLogger = {
    debug() {},
    info() {},
    warn() {},
    error() {},
};

// Default no-op persistence
const noopPersistence: GlyphPersistence = {
    getMinimizedGlyphs: () => [],
    addMinimizedGlyph() {},
    removeMinimizedGlyph() {},
};

// Default no-op canvas host
const noopCanvasHost: CanvasHost = {
    saveCanvasGlyph() {},
    getCanvasGlyphs: () => [],
    getTransform: () => ({ panX: 0, panY: 0, scale: 1 }),
    getSelectedGlyphIds: () => [],
    isGlyphSelected: () => false,
    saveComposition() {},
    removeComposition() {},
    findCompositionByGlyph: () => null,
    flushSync() {},
};

// Default stripHtml using DOMParser (works in any browser)
function defaultStripHtml(html: string): string {
    const doc = new DOMParser().parseFromString(html, 'text/html');
    return doc.body.textContent ?? '';
}

// Active configuration — starts with defaults
let config: {
    logger: GlyphLogger;
    logSegment: string;
    persistence: GlyphPersistence;
    stripHtml: (html: string) => string;
    canvas: CanvasCoordinateBridge | null;
    canvasHost: CanvasHost;
    removeCanvasGlyph: ((glyphId: string) => void) | null;
} = {
    logger: noopLogger,
    logSegment: 'GLYPH',
    persistence: noopPersistence,
    stripHtml: defaultStripHtml,
    canvas: null,
    canvasHost: noopCanvasHost,
    removeCanvasGlyph: null,
};

/**
 * Configure the glyph package with host-specific implementations.
 * Call once at app startup.
 */
export function configureGlyphs(opts: GlyphConfig): void {
    if (opts.logger) config.logger = opts.logger;
    if (opts.logSegment) config.logSegment = opts.logSegment;
    if (opts.persistence) config.persistence = opts.persistence;
    if (opts.stripHtml) config.stripHtml = opts.stripHtml;
    if (opts.canvas) config.canvas = opts.canvas;
    if (opts.canvasHost) config.canvasHost = opts.canvasHost;
    if (opts.removeCanvasGlyph) config.removeCanvasGlyph = opts.removeCanvasGlyph;
}

/** Get the active logger */
export function getLogger(): GlyphLogger {
    return config.logger;
}

/** Get the log segment string */
export function getLogSegment(): string {
    return config.logSegment;
}

/** Get the active persistence layer */
export function getPersistence(): GlyphPersistence {
    return config.persistence;
}

/** Get the active canvas host */
export function getCanvasHost(): CanvasHost {
    return config.canvasHost;
}

/** Strip HTML tags from a string */
export function stripHtml(html: string): string {
    return config.stripHtml(html);
}

/** Get the canvas coordinate bridge (null if not configured). */
export function getCanvasBridge(): CanvasCoordinateBridge | null {
    return config.canvas;
}

/** Remove a glyph from canvas state. No-op if not configured. */
export function removeCanvasGlyph(glyphId: string): void {
    config.removeCanvasGlyph?.(glyphId);
}
