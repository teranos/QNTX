/**
 * Canvas-Window Manifestation — morph a canvas-placed glyph into a floating window and back.
 *
 * Unlike the tray→window path (manifestations/window.ts) which clears the element
 * and rebuilds via renderContent(), this path wraps/unwraps existing children so
 * DOM state (scroll position, textarea content, rendered markdown) is preserved.
 */

import { log, SEG } from '../../../logger';
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
import { canvasToScreen, screenToCanvas, getTransform } from '../canvas/canvas-pan';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_PADDING,
} from '../glyph';
import { runCleanup } from '../glyph-interaction';

// ── Default window dimensions ────────────────────────────────────────

const DEFAULT_WIDTH = 520;
const DEFAULT_HEIGHT = 420;

// Key for storing original canvas parent on the element
const CANVAS_PARENT_KEY = '__canvasParent';

// ── Public API ───────────────────────────────────────────────────────

export interface CanvasWindowConfig {
    title: string;
    canvasId: string;
    onClose?: () => void;
    /** When provided, the − button minimizes to tray instead of returning to canvas. */
    onMinimize?: () => void;
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

    // 3. Tear down canvas drag/resize handlers
    runCleanup(element);

    // 4. Wrap existing children into a scrollable content div
    const contentDiv = document.createElement('div');
    contentDiv.className = 'canvas-window-content';
    contentDiv.style.flex = '1';
    contentDiv.style.overflow = 'auto';
    while (element.firstChild) {
        contentDiv.appendChild(element.firstChild);
    }

    // 5. Build window title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'window-title-bar';
    titleBar.style.width = '100%';
    titleBar.style.flexShrink = '0';
    titleBar.style.boxSizing = 'border-box';
    titleBar.style.padding = TITLE_BAR_PADDING;

    const titleText = document.createElement('span');
    titleText.textContent = title;
    titleBar.appendChild(titleText);

    const minimizeBtn = document.createElement('button');
    minimizeBtn.textContent = '−';
    if (config.onMinimize) {
        minimizeBtn.title = 'Minimize to tray';
        minimizeBtn.onclick = () => minimizeCanvasWindowToTray(element, config);
    } else {
        minimizeBtn.title = 'Minimize back to canvas';
        minimizeBtn.onclick = () => morphWindowToCanvasPlaced(element, config);
    }
    titleBar.appendChild(minimizeBtn);

    if (onClose) {
        const closeBtn = document.createElement('button');
        closeBtn.textContent = '×';
        closeBtn.title = 'Close';
        closeBtn.onclick = () => onClose();
        titleBar.appendChild(closeBtn);
    }

    // 6. Assemble: title bar + content
    element.appendChild(titleBar);
    element.appendChild(contentDiv);

    // 7. Save original parent, detach from canvas, reparent to body as fixed
    (element as any)[CANVAS_PARENT_KEY] = originalParent;
    element.remove();
    element.style.position = 'fixed';
    element.style.zIndex = '1000';
    document.body.appendChild(element);

    // 8. Mark window state
    setWindowState(element, true);

    // 9. Calculate target window rect
    const remembered = getLastPosition(element);
    const targetW = DEFAULT_WIDTH;
    const targetH = DEFAULT_HEIGHT;
    const targetX = remembered?.x ?? Math.round((window.innerWidth - targetW) / 2);
    const targetY = remembered?.y ?? Math.round((window.innerHeight - targetH) / 2);

    // 10. Animate
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
        element.style.backgroundColor = 'var(--bg-almost-black)';
        element.style.display = 'flex';
        element.style.flexDirection = 'column';
        element.style.overflow = 'hidden';
        // Clear canvas-specific styles that bleed through
        element.style.minHeight = '';
        element.style.border = '';
        element.style.borderTop = '';

        // Set up window dragging
        setupWindowDrag(element, titleBar);

        log.debug(SEG.GLYPH, `[CanvasWindow] Morphed to window at ${targetX},${targetY}`);
    }).catch(err => {
        log.warn(SEG.GLYPH, `[CanvasWindow] Maximize animation failed:`, err);
    });
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

    const { onRestoreComplete } = config;

    // 1. Remember window position for next expand
    const windowRect = element.getBoundingClientRect();
    setLastPosition(element, windowRect.left, windowRect.top);

    // 2. Tear down window drag
    teardownWindowDrag(element);

    // 3. Read canvas origin, convert to current screen coords
    const origin = getCanvasOrigin(element);
    if (!origin) {
        log.warn(SEG.GLYPH, `[CanvasWindow] No canvas origin stored, aborting restore`);
        return;
    }

    // canvasToScreen returns coords relative to the canvas container's content layer,
    // not the viewport. Add the container's viewport offset for the fixed-position animation.
    const canvasContainer = document.querySelector(`[data-canvas-id="${origin.canvasId}"]`) as HTMLElement | null;
    const canvasRect = canvasContainer?.getBoundingClientRect() ?? { left: 0, top: 0 };
    const screenPos = canvasToScreen(origin.canvasId, origin.x, origin.y);
    const scale = getTransform(origin.canvasId).scale;
    const toRect = {
        x: screenPos.x + canvasRect.left,
        y: screenPos.y + canvasRect.top,
        width: origin.width * scale,
        height: origin.height * scale,
    };

    // 4. Animate back to canvas rect
    beginRestoreMorph(element, windowRect, toRect, getMinimizeDuration())
        .then(() => {
            // 5. Unwrap: move children out of content div, remove title bar
            const contentDiv = element.querySelector('.canvas-window-content');
            const titleBar = element.querySelector('.window-title-bar');

            if (contentDiv) {
                while (contentDiv.firstChild) {
                    element.appendChild(contentDiv.firstChild);
                }
                contentDiv.remove();
            }
            if (titleBar) {
                titleBar.remove();
            }

            // 6. Clear state
            setWindowState(element, false);
            clearCanvasOrigin(element);

            // 7. Remove from body, clear inline styles
            element.remove();
            element.style.cssText = '';

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

            // 10. Notify caller to re-attach drag/resize handlers
            onRestoreComplete(element);

            log.debug(SEG.GLYPH, `[CanvasWindow] Restored to canvas at ${origin.x},${origin.y}`);
        })
        .catch(err => {
            log.warn(SEG.GLYPH, `[CanvasWindow] Restore animation failed:`, err);
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

    // 1. Remember window position for next expand
    const windowRect = element.getBoundingClientRect();
    setLastPosition(element, windowRect.left, windowRect.top);

    // 2. Tear down window drag
    teardownWindowDrag(element);

    // 3. Find tray target position
    const tray = document.querySelector('.glyph-run');
    let targetX = window.innerWidth - 50;
    let targetY = window.innerHeight / 2;
    if (tray) {
        const trayRect = tray.getBoundingClientRect();
        targetX = trayRect.right - 20;
        targetY = trayRect.top + trayRect.height / 2;
    }

    // 4. Animate toward tray
    beginMinimizeMorph(element, windowRect, { x: targetX, y: targetY }, getMinimizeDuration())
        .then(() => {
            // 5. Clear state and remove
            setWindowState(element, false);
            clearCanvasOrigin(element);
            element.remove();
            element.style.cssText = '';

            // 6. Notify caller
            config.onMinimize!();

            log.debug(SEG.GLYPH, `[CanvasWindow] Minimized to tray`);
        })
        .catch(err => {
            log.warn(SEG.GLYPH, `[CanvasWindow] Minimize to tray animation failed:`, err);
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

    const { onRestoreComplete } = config;

    // 1. Find the visible canvas
    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(SEG.GLYPH, `[CanvasWindow] No canvas workspace found, aborting place`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const canvasRect = canvasEl.getBoundingClientRect();
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, `[CanvasWindow] No content layer in canvas ${canvasId}`);
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
    const canvasPos = screenToCanvas(canvasId, relX, relY);

    // 5. Glyph dimensions (from stored origin or default)
    const origin = getCanvasOrigin(element);
    const glyphW = origin?.width ?? 400;
    const glyphH = origin?.height ?? 250;

    // 6. Animation target: same screen position, glyph size scaled by canvas zoom
    const scale = getTransform(canvasId).scale;
    const toRect = {
        x: windowRect.left,
        y: windowRect.top,
        width: glyphW * scale,
        height: glyphH * scale,
    };

    // 7. Animate
    beginRestoreMorph(element, windowRect, toRect, getMinimizeDuration())
        .then(() => {
            // 8. Unwrap content div and title bar
            const contentDiv = element.querySelector('.canvas-window-content');
            const titleBar = element.querySelector('.window-title-bar');
            if (contentDiv) {
                while (contentDiv.firstChild) {
                    element.appendChild(contentDiv.firstChild);
                }
                contentDiv.remove();
            }
            if (titleBar) {
                titleBar.remove();
            }

            // 9. Clear state
            setWindowState(element, false);
            clearCanvasOrigin(element);

            // 10. Remove from body, clear inline styles
            element.remove();
            element.style.cssText = '';

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

            log.debug(SEG.GLYPH, `[CanvasWindow] Placed on canvas ${canvasId} at ${Math.round(canvasPos.x)},${Math.round(canvasPos.y)}`);
        })
        .catch(err => {
            log.warn(SEG.GLYPH, `[CanvasWindow] Place on canvas animation failed:`, err);
        });
}

// ── Window drag (private) ────────────────────────────────────────────

const DRAG_KEY = '__canvasWindowDrag';

function setupWindowDrag(windowElement: HTMLElement, handle: HTMLElement): void {
    let isDragging = false;
    let offsetX = 0;
    let offsetY = 0;

    const startDrag = (clientX: number, clientY: number) => {
        isDragging = true;
        const rect = windowElement.getBoundingClientRect();
        offsetX = clientX - rect.left;
        offsetY = clientY - rect.top;
        document.body.style.cursor = 'move';

        window.addEventListener('mousemove', drag);
        window.addEventListener('mouseup', stopDrag);
        window.addEventListener('touchmove', touchDrag, { passive: false });
        window.addEventListener('touchend', stopDrag);
    };

    const drag = (e: MouseEvent) => {
        if (!isDragging) return;
        applyDragPosition(windowElement, e.clientX - offsetX, e.clientY - offsetY);
    };

    const touchDrag = (e: TouchEvent) => {
        if (!isDragging || !e.touches[0]) return;
        e.preventDefault();
        applyDragPosition(windowElement, e.touches[0].clientX - offsetX, e.touches[0].clientY - offsetY);
    };

    const stopDrag = () => {
        if (!isDragging) return;
        isDragging = false;
        document.body.style.cursor = '';

        const rect = windowElement.getBoundingClientRect();
        setLastPosition(windowElement, rect.left, rect.top);

        window.removeEventListener('mousemove', drag);
        window.removeEventListener('mouseup', stopDrag);
        window.removeEventListener('touchmove', touchDrag);
        window.removeEventListener('touchend', stopDrag);
    };

    const handleMouseDown = (e: MouseEvent) => {
        if ((e.target as HTMLElement).tagName === 'BUTTON') return;
        e.preventDefault();
        startDrag(e.clientX, e.clientY);
    };

    const handleTouchStart = (e: TouchEvent) => {
        if ((e.target as HTMLElement).tagName === 'BUTTON') return;
        if (!e.touches[0]) return;
        e.preventDefault();
        startDrag(e.touches[0].clientX, e.touches[0].clientY);
    };

    handle.addEventListener('mousedown', handleMouseDown);
    handle.addEventListener('touchstart', handleTouchStart, { passive: false });

    // Store for teardown
    (windowElement as any)[DRAG_KEY] = { isDragging, offsetX, offsetY, handleMouseDown, handleTouchStart, handle };
}

function applyDragPosition(el: HTMLElement, newX: number, newY: number): void {
    const rect = el.getBoundingClientRect();
    const minVisible = 50;
    newX = Math.max(-rect.width + minVisible, Math.min(window.innerWidth - minVisible, newX));
    newY = Math.max(0, Math.min(window.innerHeight - minVisible, newY));
    el.style.left = `${newX}px`;
    el.style.top = `${newY}px`;
}

function teardownWindowDrag(windowElement: HTMLElement): void {
    const state = (windowElement as any)[DRAG_KEY];
    if (!state) return;
    const { handleMouseDown, handleTouchStart, handle } = state;
    handle.removeEventListener('mousedown', handleMouseDown);
    handle.removeEventListener('touchstart', handleTouchStart);
    delete (windowElement as any)[DRAG_KEY];
}
