/**
 * Panel Manifestation - Full-width resizable workspace panel
 *
 * The glyph element morphs from its tray dot into a full-width panel
 * via beginMaximizeMorph. The panel's target position is the OPPOSITE
 * edge of the system drawer (#system-drawer):
 * - Desktop: system drawer at bottom -> panel anchored to top
 * - Mobile: system drawer at top -> panel anchored to bottom
 *
 * The panel is the primary interaction surface for sustained work.
 * It opens at full viewport height, can be resized by dragging the
 * edge, and snaps to fullscreen when dragged past 90% height.
 * No overlay — the panel is a workspace, not a modal.
 *
 * Same single DOM element axiom — the glyph element itself
 * becomes the panel, no cloning.
 */

import { getLogger, getLogSegment } from '../config';
import { type Glyph, DEFAULT_GLYPH_COLOR, DEFAULT_GLYPH_TEXT_COLOR } from '../glyph';
import { addWindowControls } from './title-bar-controls';
import { stashContent } from './stash';
import { renderGlyphContent } from './render-content';
import {
    setWindowState,
    setGlyphId
} from '../dataset';
import { prepareMorphTo, calculateTrayTarget, resetGlyphElement } from './morphology';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    PANEL_Z_INDEX
} from '../glyph';

// Type-safe element state — avoids `as any` on DOM elements
const escapeHandlers = new WeakMap<HTMLElement, (e: KeyboardEvent) => void>();
const minimizing = new WeakSet<HTMLElement>();
const resizeCleanups = new WeakMap<HTMLElement, () => void>();

/** Fraction of viewport height at which panel snaps to fullscreen */
const FULLSCREEN_SNAP_THRESHOLD = 0.9;
/** Minimum panel height as fraction of viewport */
const MIN_PANEL_HEIGHT_FRACTION = 0.3;

/**
 * Determine panel anchor edge — opposite of system drawer position.
 * The glyph morphs to this target; there is no separate slide animation.
 */
function detectSlideDirection(): 'from-top' | 'from-bottom' {
    const drawer = document.getElementById('system-drawer');
    if (!drawer) return 'from-top'; // Default: desktop layout

    const rect = drawer.getBoundingClientRect();
    const viewportMid = window.innerHeight / 2;
    // If drawer center is below viewport midpoint, it's at the bottom -> slide from top
    return (rect.top + rect.height / 2) > viewportMid ? 'from-top' : 'from-bottom';
}

/**
 * Attach a resize handle to the panel's bottom (from-top) or top (from-bottom) edge.
 * Dragging adjusts panel height. Snaps to fullscreen past 90% viewport height.
 * Returns a cleanup function.
 */
function attachResizeHandle(
    panelElement: HTMLElement,
    direction: 'from-top' | 'from-bottom'
): () => void {
    const log = getLogger();
    const seg = getLogSegment();
    const handle = document.createElement('div');
    handle.className = direction === 'from-top'
        ? 'glyph-panel-resize-handle glyph-panel-resize-handle--bottom'
        : 'glyph-panel-resize-handle glyph-panel-resize-handle--top';
    panelElement.appendChild(handle);

    const controller = new AbortController();
    let isDragging = false;
    let startY = 0;
    let startHeight = 0;
    let dragController: AbortController | null = null;

    const onMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        const delta = direction === 'from-top'
            ? e.clientY - startY    // Dragging bottom edge down = taller
            : startY - e.clientY;   // Dragging top edge up = taller

        let newHeight = startHeight + delta;
        const vh = window.innerHeight;
        const minHeight = Math.round(vh * MIN_PANEL_HEIGHT_FRACTION);

        newHeight = Math.max(minHeight, Math.min(vh, newHeight));

        // Snap to fullscreen
        const isFullscreen = newHeight / vh >= FULLSCREEN_SNAP_THRESHOLD;
        if (isFullscreen) {
            newHeight = vh;
        }

        panelElement.style.height = `${newHeight}px`;
        panelElement.classList.toggle('glyph-panel--fullscreen', isFullscreen);

        // For from-bottom panels, also adjust top position
        if (direction === 'from-bottom') {
            panelElement.style.top = `${vh - newHeight}px`;
        }
    };

    const onMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;
        panelElement.classList.remove('glyph-panel--resizing');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        dragController?.abort();
        dragController = null;

        log.debug(seg, `[Panel] Resized to ${panelElement.offsetHeight}px`);
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isDragging = true;
        startY = e.clientY;
        startHeight = panelElement.offsetHeight;

        panelElement.classList.add('glyph-panel--resizing');
        document.body.style.cursor = 'ns-resize';
        document.body.style.userSelect = 'none';

        dragController = new AbortController();
        document.addEventListener('mousemove', onMouseMove, { signal: dragController.signal });
        document.addEventListener('mouseup', onMouseUp, { signal: dragController.signal });
    }, { signal: controller.signal });

    return () => {
        controller.abort();
        dragController?.abort();
        handle.remove();
    };
}

/**
 * Morph a glyph to a full-width panel (no overlay)
 */
export function morphToPanel(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onRemove: (id: string) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    const glyphRect = prepareMorphTo(glyphElement, glyph, verifyElement, 'glyph-morphing-to-panel', PANEL_Z_INDEX);

    const direction = detectSlideDirection();

    // Panel dimensions: full viewport width, full viewport height
    const panelWidth = window.innerWidth;
    const panelHeight = window.innerHeight;

    // Target position based on slide direction
    const targetX = 0;
    const targetY = direction === 'from-top' ? 0 : window.innerHeight - panelHeight;

    // Close on Escape
    const escapeHandler = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            document.removeEventListener('keydown', escapeHandler);
            morphFromPanel(glyphElement, glyph, verifyElement, onMinimize);
        }
    };
    document.addEventListener('keydown', escapeHandler);
    escapeHandlers.set(glyphElement, escapeHandler);

    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: panelWidth, height: panelHeight },
        getMaximizeDuration()
    ).then(() => {
        log.debug(seg, `[Panel] Animation committed for ${glyph.id}`);

        const directionClass = direction === 'from-top' ? 'glyph-panel--from-top' : 'glyph-panel--from-bottom';
        glyphElement.className = `glyph-panel glyph-panel--fullscreen ${directionClass}`;
        glyphElement.style.cssText = '';
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = `${panelWidth}px`;
        glyphElement.style.height = `${panelHeight}px`;
        glyphElement.style.zIndex = PANEL_Z_INDEX;
        glyphElement.style.backgroundColor = glyph.color ?? DEFAULT_GLYPH_COLOR;
        glyphElement.style.color = glyph.textColor ?? DEFAULT_GLYPH_TEXT_COLOR;

        // Restore stashed content or render fresh (shared with window.ts)
        const { titleBar } = renderGlyphContent(glyphElement, glyph, 'Panel');

        // Add window controls (minimize/close) to the title bar
        addWindowControls(titleBar, {
            onMinimize: () => morphFromPanel(glyphElement, glyph, verifyElement, onMinimize),
            onClose: glyph.onClose ? () => {
                const handler = escapeHandlers.get(glyphElement);
                if (handler) {
                    document.removeEventListener('keydown', handler);
                    escapeHandlers.delete(glyphElement);
                }
                cleanupResize(glyphElement);
                onRemove(glyph.id);
                glyphElement.remove();
                try { glyph.onClose!(); } catch (error) {
                    log.error(seg, `[Panel ${glyph.id}] Error in onClose: ${error instanceof Error ? error.message : String(error)}`);
                }
            } : undefined,
        });

        // Attach resize handle
        const cleanupFn = attachResizeHandle(glyphElement, direction);
        resizeCleanups.set(glyphElement, cleanupFn);
    }).catch(error => {
        log.warn(seg, `[Panel] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
        const handler = escapeHandlers.get(glyphElement);
        if (handler) {
            document.removeEventListener('keydown', handler);
            escapeHandlers.delete(glyphElement);
        }
        // Reattach to tray so the glyph isn't orphaned
        setWindowState(glyphElement, false);
        glyphElement.remove();
        glyphElement.style.cssText = '';
        glyphElement.className = 'glyph-run-glyph';
        setGlyphId(glyphElement, glyph.id);
        onMinimize(glyphElement, glyph);
    });
}

/** Clean up resize handler for a panel element */
function cleanupResize(element: HTMLElement): void {
    const cleanup = resizeCleanups.get(element);
    if (cleanup) {
        cleanup();
        resizeCleanups.delete(element);
    }
}

/**
 * Morph a panel back into a glyph (dot)
 */
export function morphFromPanel(
    panelElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    // Re-entrance guard: Escape can fire in quick succession
    if (minimizing.has(panelElement)) return;
    minimizing.add(panelElement);

    verifyElement(glyph.id, panelElement);
    log.debug(seg, `[Panel] Minimizing ${glyph.id}`);

    const currentRect = panelElement.getBoundingClientRect();

    // Clean up escape handler
    const handler = escapeHandlers.get(panelElement);
    if (handler) {
        document.removeEventListener('keydown', handler);
        escapeHandlers.delete(panelElement);
    }

    // Clean up resize handle
    cleanupResize(panelElement);

    // Stash content (strips window controls, preserves glyph identity off-DOM)
    stashContent(panelElement);

    const trayTarget = calculateTrayTarget(glyph.id);

    beginMinimizeMorph(panelElement, currentRect, trayTarget, getMinimizeDuration())
        .then(() => {
            minimizing.delete(panelElement);
            resetGlyphElement(panelElement, glyph, 'Panel', onMorphComplete);
        })
        .catch(error => {
            log.warn(seg, `[Panel] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
            minimizing.delete(panelElement);
        });
}
