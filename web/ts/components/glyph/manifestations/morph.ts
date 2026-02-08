/**
 * Manifestation Morphing - Canvas ↔ Window
 *
 * Generic system for morphing any canvas-positioned glyph to a floating
 * window manifestation and back. Same DOM element throughout (single-element axiom).
 *
 * Unlike conversions (which change glyph type), manifestation morphs change
 * how a glyph presents itself while keeping the same identity and content.
 *
 * Flow:
 * 1. Canvas content is stashed in a DocumentFragment (preserving DOM nodes + listeners)
 * 2. Element detaches from canvas, reparents to body, animates to window position
 * 3. Window chrome (title bar, buttons) wraps the stashed content
 * 4. On morph-back: content unstashed, element reparents to canvas, onRestore re-sets handlers
 */

import { log, SEG } from '../../../logger';
import { stripHtml } from '../../../html-utils';
import type { Glyph } from '../glyph';
import { uiState } from '../../../state/ui';
import { runCleanup } from '../glyph-interaction';
import { beginReshapeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_HEIGHT,
    TITLE_BAR_PADDING,
    WINDOW_BUTTON_SIZE,
} from '../glyph';
import { makeWindowDraggable } from './window';

// Key for stashed canvas content on element
const CANVAS_STASH_KEY = '__canvasContent';

export interface CanvasToWindowOptions {
    /** Window title bar text */
    title: string;
    /** Window width in pixels (default: 600) */
    width?: number;
    /** Window height in pixels (default: 400) */
    height?: number;
    /** Called when close button is clicked (after element removal) */
    onClose?: () => void;
    /** Called after morphing back to canvas to re-setup handlers (drag, resize, etc.) */
    onRestore?: (element: HTMLElement) => void;
}

/**
 * Morph a canvas glyph to a floating window manifestation.
 *
 * The same DOM element transforms from a canvas-positioned glyph to a
 * body-fixed window with chrome (title bar, minimize, close, drag).
 * Canvas content is preserved in a DocumentFragment and restored on morph-back.
 */
export function morphCanvasGlyphToWindow(
    element: HTMLElement,
    glyph: Glyph,
    canvasContainer: HTMLElement,
    opts: CanvasToWindowOptions,
): void {
    // Guard: already in window manifestation
    if (element.dataset.manifestation === 'window') {
        log.debug(SEG.GLYPH, `[Morph] ${glyph.id} already in window manifestation`);
        return;
    }

    const canvasRect = element.getBoundingClientRect();
    const containerRect = canvasContainer.getBoundingClientRect();

    // Save canvas-relative position and styling for restore
    element.dataset.canvasX = String(Math.round(canvasRect.left - containerRect.left));
    element.dataset.canvasY = String(Math.round(canvasRect.top - containerRect.top));
    element.dataset.canvasWidth = String(Math.round(canvasRect.width));
    element.dataset.canvasHeight = String(Math.round(canvasRect.height));
    element.dataset.canvasClass = element.className;
    element.dataset.canvasStyle = element.style.cssText;

    // Tear down canvas handlers (drag, resize, observers)
    runCleanup(element);

    // Stash canvas content - preserves DOM nodes and their event listeners
    const fragment = document.createDocumentFragment();
    while (element.firstChild) fragment.appendChild(element.firstChild);
    (element as any)[CANVAS_STASH_KEY] = fragment;

    // Calculate window target (centered in viewport)
    const windowWidth = opts.width ?? 600;
    const windowHeight = opts.height ?? 400;
    const targetX = Math.round((window.innerWidth - windowWidth) / 2);
    const targetY = Math.round((window.innerHeight - windowHeight) / 2);

    // Detach from canvas (keeps element alive)
    element.remove();

    // Prepare for morph animation
    element.className = 'glyph-morphing-to-window';
    element.style.cssText = '';
    element.style.position = 'fixed';
    element.style.zIndex = '1000';
    element.style.overflow = 'hidden';

    // Reparent to body for window positioning
    document.body.appendChild(element);

    element.dataset.manifestation = 'window';

    // Animate: canvas rect → window rect
    beginReshapeMorph(
        element,
        { x: canvasRect.left, y: canvasRect.top, width: canvasRect.width, height: canvasRect.height },
        { x: targetX, y: targetY, width: windowWidth, height: windowHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT: Apply final window state
        element.className = 'canvas-glyph-as-window';
        element.style.position = 'fixed';
        element.style.left = `${targetX}px`;
        element.style.top = `${targetY}px`;
        element.style.width = `${windowWidth}px`;
        element.style.height = `${windowHeight}px`;
        element.style.borderRadius = WINDOW_BORDER_RADIUS;
        element.style.backgroundColor = 'var(--bg-almost-black)';
        element.style.boxShadow = WINDOW_BOX_SHADOW;
        element.style.padding = '0';
        element.style.opacity = '1';
        element.style.color = 'var(--text-on-dark)';
        element.style.display = 'flex';
        element.style.flexDirection = 'column';
        element.style.overflow = 'hidden';
        element.style.zIndex = '1000';

        // Build title bar
        const titleBar = document.createElement('div');
        titleBar.className = 'morph-window-title-bar';
        titleBar.style.height = TITLE_BAR_HEIGHT;
        titleBar.style.width = '100%';
        titleBar.style.backgroundColor = 'var(--bg-almost-black)';
        titleBar.style.borderBottom = '1px solid var(--border-on-dark)';
        titleBar.style.borderRadius = '8px 8px 0 0';
        titleBar.style.display = 'flex';
        titleBar.style.alignItems = 'center';
        titleBar.style.padding = TITLE_BAR_PADDING;
        titleBar.style.flexShrink = '0';
        titleBar.style.boxSizing = 'border-box';

        const titleText = document.createElement('span');
        titleText.textContent = stripHtml(opts.title);
        titleText.style.flex = '1';
        titleText.style.color = 'var(--text-on-dark)';
        titleText.style.fontSize = '12px';
        titleBar.appendChild(titleText);

        // Minimize button → morph back to canvas
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.title = 'Collapse to canvas';
        minimizeBtn.style.width = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.height = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.border = 'none';
        minimizeBtn.style.background = 'transparent';
        minimizeBtn.style.cursor = 'pointer';
        minimizeBtn.style.color = 'var(--text-on-dark)';
        minimizeBtn.onclick = () => morphWindowBackToCanvas(element, glyph, opts);
        titleBar.appendChild(minimizeBtn);

        // Close button → remove glyph entirely
        const closeBtn = document.createElement('button');
        closeBtn.textContent = '×';
        closeBtn.title = 'Close';
        closeBtn.style.width = WINDOW_BUTTON_SIZE;
        closeBtn.style.height = WINDOW_BUTTON_SIZE;
        closeBtn.style.border = 'none';
        closeBtn.style.background = 'transparent';
        closeBtn.style.cursor = 'pointer';
        closeBtn.style.color = 'var(--text-on-dark)';
        closeBtn.onclick = () => {
            element.remove();
            delete element.dataset.manifestation;
            if (opts.onClose) {
                try {
                    opts.onClose();
                } catch (e) {
                    log.error(SEG.GLYPH, `[Morph] onClose error for ${glyph.id}:`, e);
                }
            }
        };
        titleBar.appendChild(closeBtn);

        element.appendChild(titleBar);

        // Content area with stashed canvas content
        const contentArea = document.createElement('div');
        contentArea.className = 'morph-window-content';
        contentArea.style.flex = '1';
        contentArea.style.overflow = 'auto';

        const stash = (element as any)[CANVAS_STASH_KEY] as DocumentFragment;
        if (stash) {
            contentArea.appendChild(stash);
            delete (element as any)[CANVAS_STASH_KEY];
        }

        element.appendChild(contentArea);

        // Make window draggable by title bar
        makeWindowDraggable(element, titleBar);

        // Remove from canvas UI state (glyph is now floating as a window)
        uiState.removeCanvasGlyph(glyph.id);

        log.info(SEG.GLYPH, `[Morph] Canvas → Window for ${glyph.id}`);
    }).catch(error => {
        log.warn(SEG.GLYPH, `[Morph] Canvas → Window animation failed for ${glyph.id}:`, error);
    });
}

/**
 * Morph a windowed glyph back to its canvas position.
 *
 * Extracts content from the window content area, animates the element
 * back to the saved canvas position, reparents to the canvas container,
 * and calls onRestore to re-setup canvas-specific handlers.
 */
function morphWindowBackToCanvas(
    element: HTMLElement,
    glyph: Glyph,
    opts: CanvasToWindowOptions,
): void {
    const windowRect = element.getBoundingClientRect();

    // Find canvas container (may have been remounted since morph)
    const canvasContainer = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasContainer) {
        log.warn(SEG.GLYPH, `[Morph] Canvas not found, closing window for ${glyph.id}`);
        element.remove();
        delete element.dataset.manifestation;
        return;
    }

    // Get saved canvas position
    const canvasX = parseInt(element.dataset.canvasX || '200');
    const canvasY = parseInt(element.dataset.canvasY || '200');
    const canvasWidth = parseInt(element.dataset.canvasWidth || '400');
    const canvasHeight = parseInt(element.dataset.canvasHeight || '200');
    const canvasClass = element.dataset.canvasClass || 'canvas-glyph';
    const canvasStyle = element.dataset.canvasStyle || '';

    // Extract content from window content area (preserve DOM nodes)
    const contentArea = element.querySelector('.morph-window-content');
    const fragment = document.createDocumentFragment();
    if (contentArea) {
        while (contentArea.firstChild) fragment.appendChild(contentArea.firstChild);
    }

    // Clear window chrome
    element.replaceChildren();

    // Calculate canvas position in viewport coordinates for animation
    const containerRect = canvasContainer.getBoundingClientRect();
    const targetViewportX = containerRect.left + canvasX;
    const targetViewportY = containerRect.top + canvasY;

    // Animate: window rect → canvas rect
    beginReshapeMorph(
        element,
        { x: windowRect.left, y: windowRect.top, width: windowRect.width, height: windowRect.height },
        { x: targetViewportX, y: targetViewportY, width: canvasWidth, height: canvasHeight },
        getMinimizeDuration()
    ).then(() => {
        // COMMIT: Restore canvas state
        element.remove();

        // Clean up morph metadata
        delete element.dataset.manifestation;
        delete element.dataset.canvasX;
        delete element.dataset.canvasY;
        delete element.dataset.canvasWidth;
        delete element.dataset.canvasHeight;
        delete element.dataset.canvasClass;
        delete element.dataset.canvasStyle;

        // Restore canvas className and inline styles
        element.className = canvasClass;
        element.style.cssText = canvasStyle;

        // Override position/size with saved canvas values (may differ from original if dragged)
        element.style.left = `${canvasX}px`;
        element.style.top = `${canvasY}px`;
        element.style.width = `${canvasWidth}px`;
        element.style.height = `${canvasHeight}px`;

        // Restore canvas content
        element.appendChild(fragment);

        // Reparent to canvas container
        canvasContainer.appendChild(element);

        // Re-setup canvas handlers via callback
        if (opts.onRestore) {
            opts.onRestore(element);
        }

        // Restore canvas UI state
        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                x: canvasX,
                y: canvasY,
                width: canvasWidth,
                height: canvasHeight,
                result: glyph.result,
            });
        }

        log.info(SEG.GLYPH, `[Morph] Window → Canvas for ${glyph.id}`);
    }).catch(error => {
        log.warn(SEG.GLYPH, `[Morph] Window → Canvas animation failed for ${glyph.id}:`, error);
    });
}
