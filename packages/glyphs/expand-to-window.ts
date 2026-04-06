/**
 * Expand-to-Window — unified lifecycle for canvas ↔ window ↔ tray morphing.
 *
 * Replaces the copy-pasted expand-button click handler that existed in every
 * glyph file (attestation, note, response, plugin-module). Each glyph now
 * passes a config object and gets the full bidirectional morph for free.
 */

import { getLogger, getLogSegment, removeCanvasGlyph } from './config';
import { isInWindowState } from './dataset';
import { getGlyphRun } from './run';
import type { Glyph } from './glyph';
import {
    morphCanvasPlacedToWindow,
    placeWindowOnCanvas,
} from './manifestations/canvas-window';

// ── Public API ───────────────────────────────────────────────────────

export interface ExpandToWindowConfig {
    /** The canvas-placed glyph element. */
    element: HTMLElement;
    /** The expand/collapse button. */
    expandBtn: HTMLElement;
    /** Glyph identity. */
    glyphId: string;
    /** Window title and tray label. */
    title: string;
    /** Symbol for the tray dot. */
    symbol: string;
    /** Factory for tray content when minimized. */
    renderContent: () => HTMLElement;
    /** Log label prefix (e.g. 'AsGlyph', 'NoteGlyph'). */
    logLabel?: string;
    /** Visual identity for tray dot. */
    color?: string;
    textColor?: string;
    /** Called after restoring to canvas — re-apply visual identity, save dims, etc. */
    onRestoreToCanvas?: (element: HTMLElement) => void;
    /** Extra fields forwarded to glyphRun.adopt() (e.g. renderTitleBar, manifestationType). */
    adoptExtras?: Partial<Glyph>;
    /** Whether to stopPropagation on click (needed for cloned buttons). */
    stopPropagation?: boolean;
}

/**
 * Wire an expand button with the full canvas ↔ window ↔ tray lifecycle.
 *
 * On click when on canvas: morphs to floating window with minimize-to-tray
 * and close. On click when in window: places back on canvas.
 */
export function wireExpandToWindow(config: ExpandToWindowConfig): void {
    const {
        element,
        expandBtn,
        glyphId,
        title,
        symbol,
        renderContent,
        logLabel,
        color,
        textColor,
        onRestoreToCanvas,
        adoptExtras,
        stopPropagation,
    } = config;

    const label = logLabel ?? 'Glyph';

    expandBtn.addEventListener('click', (e) => {
        if (stopPropagation) e.stopPropagation();

        const log = getLogger();
        const seg = getLogSegment();

        // Already in window state → place back on canvas
        if (isInWindowState(element)) {
            placeWindowOnCanvas(element, {
                onRestoreComplete: (el) => {
                    expandBtn.textContent = '\u2B06'; // ⬆
                    expandBtn.title = 'Expand to window';
                    onRestoreToCanvas?.(el);
                    log.debug(seg, `[${label}] Placed on canvas ${glyphId}`);
                },
            });
            return;
        }

        // On canvas → morph to window
        const canvas = element.closest('.canvas-workspace') as HTMLElement | null;
        const canvasId = (canvas?.closest('[data-canvas-id]') as HTMLElement | null)?.dataset?.canvasId ?? 'canvas-workspace';

        morphCanvasPlacedToWindow(element, {
            title,
            canvasId,
            onClose: () => {
                element.remove();
                removeCanvasGlyph(glyphId);
                log.debug(seg, `[${label}] Closed from window ${glyphId}`);
            },
            onMinimize: (el: HTMLElement) => {
                getGlyphRun().adopt(el, {
                    id: glyphId,
                    title,
                    symbol,
                    color,
                    textColor,
                    renderContent,
                    onClose: () => {
                        log.debug(seg, `[${label}] Closed from tray ${glyphId}`);
                    },
                    ...adoptExtras,
                });
                log.debug(seg, `[${label}] Minimized to tray ${glyphId}`);
            },
            onRestoreComplete: (el) => {
                onRestoreToCanvas?.(el);
                log.debug(seg, `[${label}] Restored to canvas ${glyphId}`);
            },
        });

        expandBtn.textContent = '\u2B07'; // ⬇
        expandBtn.title = 'Place on canvas';
    });
}
