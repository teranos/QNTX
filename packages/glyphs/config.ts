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
    debug(segment: string, message: string): void;
    info(segment: string, message: string): void;
    warn(segment: string, message: string): void;
    error(segment: string, message: string): void;
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

export interface GlyphConfig {
    logger?: GlyphLogger;
    logSegment?: string;
    persistence?: GlyphPersistence;
    stripHtml?: (html: string) => string;
    canvas?: CanvasHost;
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
let config = {
    logger: noopLogger,
    logSegment: 'GLYPH',
    persistence: noopPersistence,
    stripHtml: defaultStripHtml,
    canvas: noopCanvasHost,
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
    return config.canvas;
}

/** Strip HTML tags from a string */
export function stripHtml(html: string): string {
    return config.stripHtml(html);
}
