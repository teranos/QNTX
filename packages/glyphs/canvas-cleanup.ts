/**
 * Glyph element lifecycle cleanup.
 *
 * Cleanup registry for teardown functions stored on glyph elements,
 * and ResizeObserver management for content-driven auto-sizing.
 */

import { CANVAS_GLYPH_TITLE_BAR_HEIGHT, MAX_VIEWPORT_HEIGHT_RATIO } from './glyph';
import { getLogger, getLogSegment } from './config';

// ── Cleanup registry ────────────────────────────────────────────────

const CLEANUP_KEY = '__glyphCleanup';

/**
 * Store a cleanup function on a glyph element.
 * Called by glyph setup code so conversions can tear down handlers
 * before repopulating the same element as a different glyph type.
 */
export function storeCleanup(element: HTMLElement, fn: () => void): void {
    const list: Array<() => void> = (element as any)[CLEANUP_KEY] ??= [];
    list.push(fn);
}

/**
 * Run all stored cleanup functions and clear the list.
 * Tears down drag, resize, editor, and observer handlers
 * so the element can be repopulated as a different glyph type.
 */
export function runCleanup(element: HTMLElement): void {
    const list: Array<() => void> | undefined = (element as any)[CLEANUP_KEY];
    if (list) {
        for (const fn of list) fn();
        (element as any)[CLEANUP_KEY] = [];
    }
}

// ── ResizeObserver management ───────────────────────────────────────

/**
 * Clean up ResizeObserver attached to an element.
 * Prevents memory leaks when glyphs are removed or re-rendered.
 */
export function cleanupResizeObserver(element: HTMLElement, glyphId?: string): void {
    const log = getLogger();
    const seg = getLogSegment();
    const existing = (element as any).__resizeObserver;
    if (existing && typeof existing.disconnect === 'function') {
        existing.disconnect();
        delete (element as any).__resizeObserver;
        if (glyphId) {
            log.debug(seg, `[${glyphId}] Disconnected ResizeObserver`);
        }
    }
}

/**
 * Set up a ResizeObserver that auto-sizes a glyph element to its content.
 *
 * Cleans up any existing observer first, caps height at MAX_VIEWPORT_HEIGHT_RATIO,
 * and stores the observer on the element for later cleanup.
 *
 * @param glyphElement - The glyph DOM element whose minHeight is adjusted
 * @param contentElement - The inner element to observe for size changes
 * @param label - Log label (e.g. "AX abc123")
 * @param heightOffset - Pixels to add to content height (default: CANVAS_GLYPH_TITLE_BAR_HEIGHT)
 */
export function setupGlyphResizeObserver(
    glyphElement: HTMLElement,
    contentElement: HTMLElement,
    label: string,
    heightOffset?: number,
): void {
    const log = getLogger();
    const seg = getLogSegment();

    cleanupResizeObserver(glyphElement, label);

    const offset = heightOffset ?? CANVAS_GLYPH_TITLE_BAR_HEIGHT;
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + offset, maxHeight);
            glyphElement.style.minHeight = `${totalHeight}px`;
            log.debug(seg, `[${label}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(contentElement);
    (glyphElement as any).__resizeObserver = resizeObserver;
}
