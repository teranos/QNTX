/**
 * Canvas-Window Manifestation — morph a canvas-placed glyph into a floating window and back.
 *
 * Unlike the tray→window path (manifestations/window.ts) which clears the element
 * and rebuilds via renderContent(), this path wraps/unwraps existing children so
 * DOM state (scroll position, textarea content, rendered markdown) is preserved.
 *
 * Canvas coordinate transforms are injected via configureGlyphs({ canvas }).
 */

import { getLogger, getLogSegment, getCanvasBridge } from '../config';
import {
    setCanvasOrigin,
    getCanvasOrigin,
    clearCanvasOrigin,
    setWindowState,
    isInWindowState,
    getLastPosition,
    setLastPosition,
} from '../dataset';
import { beginMaximizeMorph, beginMinimizeMorph, beginRestoreMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
} from '../glyph';
import { addWindowControls, removeWindowControls } from './title-bar-controls';
import { setupWindowDrag, teardownWindowDrag } from '../window-drag';
import { calculateTrayTarget } from './morphology';
import { stashContent } from './stash';

// ── Default window dimensions ────────────────────────────────────────

const DEFAULT_WIDTH = 520;
const DEFAULT_HEIGHT = 420;

// Key for storing original canvas parent on the element
const CANVAS_PARENT_KEY = '__canvasParent';

// Inline styles applied during window state that must be cleared on restore.
// Clearing only these preserves glyph-specific styles (border, minHeight, zIndex, etc.).
const WINDOW_STYLE_PROPS: (keyof CSSStyleDeclaration)[] = [
    'position', 'left', 'top', 'width', 'height',
    'zIndex', 'borderRadius', 'boxShadow',
    'display', 'flexDirection', 'overflow',
];

// ── Public API ───────────────────────────────────────────────────────

export interface CanvasWindowConfig {
    title: string;
    canvasId: string;
    onClose?: () => void;
    /** When provided, the − button minimizes to tray instead of returning to canvas. Receives the element for adoption. */
    onMinimize?: (element: HTMLElement) => void;
    /** Called after restore completes. Drag/resize handlers persist through morphs — use this for UI updates only. */
    onRestoreComplete: (element: HTMLElement) => void;
}

/**
 * Morph a canvas-placed glyph into a draggable floating window.
 * Wraps existing children into a scrollable content div — no rebuild.
 */
export function morphCanvasPlacedToWindow(
    element: HTMLElement,
    config: CanvasWindowConfig,
): void {
    if (isInWindowState(element)) return;

    const log = getLogger();
    const seg = getLogSegment();
    const { title, canvasId, onClose } = config;

    // 1. Save canvas-local position for morph-back
    setCanvasOrigin(element, {
        x: element.offsetLeft,
        y: element.offsetTop,
        width: element.offsetWidth,
        height: element.offsetHeight,
        canvasId,
    });

    // 2. Capture screen rect and original parent before detaching
    const fromRect = element.getBoundingClientRect();
    const originalParent = element.parentElement;

    // 3. Detect existing glyph title bar (belongs to the glyph, not the manifestation)
    const existingTitleBar = element.querySelector(':scope > .glyph-title-bar') as HTMLElement | null;

    // 4. Wrap non-title-bar children into a scrollable content div
    const contentDiv = document.createElement('div');
    contentDiv.className = 'canvas-window-content glyph-content-area';
    contentDiv.style.padding = '0';
    const children = Array.from(element.childNodes);
    for (const child of children) {
        if (child === existingTitleBar) continue;
        contentDiv.appendChild(child);
    }

    // 5. Reuse existing title bar or create a generic one
    let titleBar: HTMLElement;
    if (existingTitleBar) {
        titleBar = existingTitleBar;
    } else {
        titleBar = document.createElement('div');
        titleBar.className = 'glyph-title-bar';
        titleBar.dataset.windowCreated = 'true'; // Mark for removal on restore
        const titleText = document.createElement('span');
        titleText.textContent = title;
        titleText.style.flex = '1';
        titleBar.appendChild(titleText);
    }

    // 6. Add window controls (minimize/close) to the title bar
    addWindowControls(titleBar, {
        onMinimize: config.onMinimize
            ? () => minimizeCanvasWindowToTray(element, config)
            : () => morphWindowToCanvasPlaced(element, config),
        onClose,
    });

    // 7. Assemble: title bar + content
    element.appendChild(titleBar);
    element.appendChild(contentDiv);

    // 8. Save original parent, detach from canvas, reparent to body as fixed
    (element as any)[CANVAS_PARENT_KEY] = originalParent;
    element.remove();
    element.style.position = 'fixed';
    element.style.zIndex = '1000';
    document.body.appendChild(element);

    // 9. Mark window state
    setWindowState(element, true);

    // 10. Calculate target window rect
    const remembered = getLastPosition(element);
    const targetW = DEFAULT_WIDTH;
    const targetH = DEFAULT_HEIGHT;
    const targetX = remembered?.x ?? Math.round((window.innerWidth - targetW) / 2);
    const targetY = remembered?.y ?? Math.round((window.innerHeight - targetH) / 2);

    // 11. Animate
    beginMaximizeMorph(
        element,
        fromRect,
        { x: targetX, y: targetY, width: targetW, height: targetH },
        getMaximizeDuration(),
    ).then(() => {
        // Commit final window styles
        element.style.left = `${targetX}px`;
        element.style.top = `${targetY}px`;
        element.style.width = `${targetW}px`;
        element.style.height = `${targetH}px`;
        element.style.borderRadius = WINDOW_BORDER_RADIUS;
        element.style.boxShadow = WINDOW_BOX_SHADOW;
        element.style.display = 'flex';
        element.style.flexDirection = 'column';
        element.style.overflow = 'hidden';
        // Clear canvas-specific styles that bleed through
        element.style.minHeight = '';
        element.style.border = '';
        element.style.borderTop = '';

        // Set up window dragging
        setupWindowDrag(element, titleBar);

        log.debug(seg, `[CanvasWindow] Morphed to window at ${targetX},${targetY}`);
    }).catch(err => {
        log.warn(seg, `[CanvasWindow] Maximize animation failed: ${err}`);
    });
}

/** Shared unwrap logic: remove content div wrapper and window controls from title bar. */
function unwrapWindowContent(element: HTMLElement): void {
    const contentDiv = element.querySelector('.canvas-window-content');
    const titleBar = element.querySelector('.glyph-title-bar');

    if (titleBar) {
        if ((titleBar as HTMLElement).dataset.windowCreated) {
            titleBar.remove(); // Manifestation-created: remove entirely
        } else {
            removeWindowControls(titleBar as HTMLElement); // Glyph-owned: just strip controls
        }
    }
    if (contentDiv) {
        while (contentDiv.firstChild) {
            element.appendChild(contentDiv.firstChild);
        }
        contentDiv.remove();
    }
}

/**
 * Morph a floating window back to its canvas-placed position.
 * Unwraps children from content div — preserving DOM state.
 */
export function morphWindowToCanvasPlaced(
    element: HTMLElement,
    config: Pick<CanvasWindowConfig, 'onRestoreComplete'>,
): void {
    if (!isInWindowState(element)) return;

    const log = getLogger();
    const seg = getLogSegment();
    const bridge = getCanvasBridge();
    const { onRestoreComplete } = config;

    // 1. Remember window position for next expand
    const windowRect = element.getBoundingClientRect();
    setLastPosition(element, windowRect.left, windowRect.top);

    // 2. Tear down window drag
    teardownWindowDrag(element);

    // 3. Read canvas origin, convert to current screen coords
    const origin = getCanvasOrigin(element);
    if (!origin) {
        log.warn(seg, `[CanvasWindow] No canvas origin stored, aborting restore`);
        return;
    }

    // canvasToScreen returns coords relative to the canvas container's content layer,
    // not the viewport. Add the container's viewport offset for the fixed-position animation.
    const canvasContainer = document.querySelector(`[data-canvas-id="${origin.canvasId}"]`) as HTMLElement | null;
    const canvasRect = canvasContainer?.getBoundingClientRect() ?? { left: 0, top: 0 };
    const screenPos = bridge
        ? bridge.toScreen(origin.canvasId, origin.x, origin.y)
        : { x: origin.x, y: origin.y };
    const scale = bridge ? bridge.getScale(origin.canvasId) : 1;
    const toRect = {
        x: screenPos.x + canvasRect.left,
        y: screenPos.y + canvasRect.top,
        width: origin.width * scale,
        height: origin.height * scale,
    };

    // 4. Animate back to canvas rect
    beginRestoreMorph(element, windowRect, toRect, getMinimizeDuration())
        .then(() => {
            // 5. Unwrap window content
            unwrapWindowContent(element);

            // 6. Clear state
            setWindowState(element, false);
            clearCanvasOrigin(element);

            // 7. Remove from body, clear window-specific inline styles
            element.remove();
            for (const prop of WINDOW_STYLE_PROPS) {
                (element.style as any)[prop] = '';
            }

            // 8. Restore canvas layout from origin
            element.style.position = 'absolute';
            element.style.left = `${origin.x}px`;
            element.style.top = `${origin.y}px`;
            element.style.width = `${origin.width}px`;
            element.style.height = `${origin.height}px`;

            // 9. Reparent to original canvas container
            const savedParent = (element as any)[CANVAS_PARENT_KEY] as HTMLElement | undefined;
            delete (element as any)[CANVAS_PARENT_KEY];
            if (savedParent && savedParent.isConnected) {
                savedParent.appendChild(element);
            }

            // 10. Notify caller (drag/resize handlers persist through morph)
            onRestoreComplete(element);

            log.debug(seg, `[CanvasWindow] Restored to canvas at ${origin.x},${origin.y}`);
        })
        .catch(err => {
            log.warn(seg, `[CanvasWindow] Restore animation failed: ${err}`);
        });
}

/**
 * Minimize a canvas-morphed window to the tray.
 * Animates toward the tray, cleans up the element, then calls onMinimize.
 */
function minimizeCanvasWindowToTray(
    element: HTMLElement,
    config: CanvasWindowConfig,
): void {
    if (!isInWindowState(element)) return;

    const log = getLogger();
    const seg = getLogSegment();

    // 1. Remember window position for next expand
    const windowRect = element.getBoundingClientRect();
    setLastPosition(element, windowRect.left, windowRect.top);

    // 2. Tear down window drag
    teardownWindowDrag(element);

    // 3. Find tray target position
    const trayTarget = calculateTrayTarget(element.dataset.glyphId);

    // 4. Animate toward tray
    beginMinimizeMorph(element, windowRect, trayTarget, getMinimizeDuration())
        .then(() => {
            // 5. Stash content, clear state, pass element through
            stashContent(element);
            setWindowState(element, false);
            clearCanvasOrigin(element);
            element.remove();
            element.style.cssText = '';

            // 6. Pass element to caller for tray adoption (same element, no new creation)
            config.onMinimize!(element);

            log.debug(seg, `[CanvasWindow] Minimized to tray`);
        })
        .catch(err => {
            log.warn(seg, `[CanvasWindow] Minimize to tray animation failed: ${err}`);
        });
}

/**
 * Place a canvas-morphed window back onto the currently visible canvas
 * at the screen position where the window is — not at its original origin.
 */
export function placeWindowOnCanvas(
    element: HTMLElement,
    config: Pick<CanvasWindowConfig, 'onRestoreComplete'>,
): void {
    if (!isInWindowState(element)) return;

    const log = getLogger();
    const seg = getLogSegment();
    const bridge = getCanvasBridge();
    const { onRestoreComplete } = config;

    // 1. Find the visible canvas
    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(seg, `[CanvasWindow] No canvas workspace found, aborting place`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const canvasRect = canvasEl.getBoundingClientRect();
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(seg, `[CanvasWindow] No content layer in canvas ${canvasId}`);
        return;
    }

    // 2. Remember window position
    const windowRect = element.getBoundingClientRect();
    setLastPosition(element, windowRect.left, windowRect.top);

    // 3. Tear down window drag
    teardownWindowDrag(element);

    // 4. Convert window position to canvas-local coordinates
    const relX = windowRect.left - canvasRect.left;
    const relY = windowRect.top - canvasRect.top;
    const canvasPos = bridge
        ? bridge.fromScreen(canvasId, relX, relY)
        : { x: relX, y: relY };

    // 5. Glyph dimensions (from stored origin or default)
    const origin = getCanvasOrigin(element);
    const glyphW = origin?.width ?? 400;
    const glyphH = origin?.height ?? 250;

    // 6. Animation target: same screen position, glyph size scaled by canvas zoom
    const scale = bridge ? bridge.getScale(canvasId) : 1;
    const toRect = {
        x: windowRect.left,
        y: windowRect.top,
        width: glyphW * scale,
        height: glyphH * scale,
    };

    // 7. Animate
    beginRestoreMorph(element, windowRect, toRect, getMinimizeDuration())
        .then(() => {
            // 8. Unwrap window content
            unwrapWindowContent(element);

            // 9. Clear state
            setWindowState(element, false);
            clearCanvasOrigin(element);

            // 10. Remove from body, clear window-specific inline styles
            element.remove();
            for (const prop of WINDOW_STYLE_PROPS) {
                (element.style as any)[prop] = '';
            }

            // 11. Place at computed canvas-local position
            element.style.position = 'absolute';
            element.style.left = `${Math.round(canvasPos.x)}px`;
            element.style.top = `${Math.round(canvasPos.y)}px`;
            element.style.width = `${glyphW}px`;
            element.style.height = `${glyphH}px`;

            // 12. Reparent to canvas content layer
            contentLayer.appendChild(element);

            // 13. Notify caller
            onRestoreComplete(element);

            log.debug(seg, `[CanvasWindow] Placed on canvas ${canvasId} at ${Math.round(canvasPos.x)},${Math.round(canvasPos.y)}`);
        })
        .catch(err => {
            log.warn(seg, `[CanvasWindow] Place on canvas animation failed: ${err}`);
        });
}
