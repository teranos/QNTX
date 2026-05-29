/**
 * Canvas Pan & Zoom - Navigation for canvas workspace
 *
 * Desktop:
 * - Two-finger trackpad scroll: pan
 * - Middle mouse button drag: pan
 * - Ctrl+wheel (or Cmd+wheel): zoom
 *
 * Mobile:
 * - Single finger drag: pan
 * - Two-finger pinch: zoom
 */

import { log, SEG } from '../../../logger';
import { uiState } from '../../../state/ui';

// Zoom configuration
const ZOOM_MIN = 0.25; // 25% - maximum zoom out
const ZOOM_MAX = 4.0;  // 400% - maximum zoom in
const ZOOM_SPEED = 0.001; // Desktop wheel sensitivity

interface CanvasTransformState {
    panX: number;
    panY: number;
    scale: number;
    isPanning: boolean;
    isPinching: boolean;
    startX: number;
    startY: number;
    startPanX: number;
    startPanY: number;
    startDistance: number;
    startScale: number;
}

// Per-canvas state map
const canvasStates = new Map<string, CanvasTransformState>();

function getState(canvasId: string): CanvasTransformState {
    if (!canvasStates.has(canvasId)) {
        canvasStates.set(canvasId, {
            panX: 0,
            panY: 0,
            scale: 1.0,
            isPanning: false,
            isPinching: false,
            startX: 0,
            startY: 0,
            startPanX: 0,
            startPanY: 0,
            startDistance: 0,
            startScale: 1.0,
        });
    }
    return canvasStates.get(canvasId)!;
}

/**
 * Apply pan and zoom transform to canvas content layer
 * Order: translate first, then scale (applies translation in original coordinate space)
 */
function applyTransform(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
    if (contentLayer) {
        contentLayer.style.transform = `translate(${state.panX}px, ${state.panY}px) scale(${state.scale})`;
        contentLayer.style.setProperty('--canvas-scale', String(state.scale));
        // Border opacity: 0.4 at scale=1, 0.1 at scale=0.25 (fades when zoomed out)
        const borderOpacity = 0.1 + Math.min(state.scale, 1) * 0.3;
        contentLayer.style.setProperty('--glyph-border-opacity', String(borderOpacity.toFixed(3)));
    }
}

/**
 * Load persisted pan and zoom state from uiState
 */
function loadTransformState(canvasId: string): void {
    // Check if uiState methods are available (may not be in test environments)
    if (typeof uiState.getCanvasPan !== 'function') return;

    const saved = uiState.getCanvasPan(canvasId);
    if (saved) {
        const state = getState(canvasId);
        state.panX = saved.panX;
        state.panY = saved.panY;
        state.scale = saved.scale ?? 1.0; // Backward compatible - default to 1.0
        log.debug(SEG.GLYPH, '[CanvasPan] Loaded transform state', {
            canvasId,
            panX: state.panX,
            panY: state.panY,
            scale: state.scale
        });
    }
}

/**
 * Save pan and zoom state to uiState
 */
let saveTimer: ReturnType<typeof setTimeout> | null = null;
let pendingSaveCanvasId: string | null = null;

function saveTransformState(canvasId: string): void {
    // Check if uiState methods are available (may not be in test environments)
    if (typeof uiState.setCanvasPan !== 'function') return;

    pendingSaveCanvasId = canvasId;
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
        saveTimer = null;
        pendingSaveCanvasId = null;
        const state = getState(canvasId);
        uiState.setCanvasPan(canvasId, { panX: state.panX, panY: state.panY, scale: state.scale });
    }, 200);
}

/**
 * Setup canvas pan handlers
 */
export function setupCanvasPan(container: HTMLElement, canvasId: string): AbortController {
    const controller = new AbortController();
    const signal = controller.signal;
    const state = getState(canvasId);

    // Reset gesture flags — stale isPanning/isPinching from a previous session
    // (e.g. subcanvas destroyed mid-gesture) must not leak into the new setup
    state.isPanning = false;
    state.isPinching = false;

    // Load persisted pan state
    loadTransformState(canvasId);
    applyTransform(container, canvasId);

    // Touch identifier tracks active single-finger pan (null = no active touch pan).
    // Shared with desktop mousemove guard to prevent touch→mouse state leakage.
    let touchIdentifier: number | null = null;

    // Desktop: Two-finger trackpad scroll (wheel event) and middle mouse drag
    // Always register — user may resize browser between mobile/desktop widths
    {
        container.addEventListener('wheel', (e: WheelEvent) => {
            e.preventDefault();

            if (e.ctrlKey || e.metaKey) {
                // Pinch zoom (Ctrl+wheel or Cmd+wheel on Mac)
                const delta = -e.deltaY * ZOOM_SPEED;
                const oldScale = state.scale;
                const newScale = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, oldScale * (1 + delta)));

                // Get cursor position relative to container
                const rect = container.getBoundingClientRect();
                const cursorX = e.clientX - rect.left;
                const cursorY = e.clientY - rect.top;

                // Apply zoom origin math - keep point under cursor stationary
                const scaleFactor = newScale / oldScale;
                state.panX = cursorX - (cursorX - state.panX) * scaleFactor;
                state.panY = cursorY - (cursorY - state.panY) * scaleFactor;
                state.scale = newScale;

                applyTransform(container, canvasId);
                saveTransformState(canvasId);
            } else {
                // Two-finger scroll = pan
                state.panX -= e.deltaX;
                state.panY -= e.deltaY;

                applyTransform(container, canvasId);
                saveTransformState(canvasId);
            }
        }, { signal, passive: false });

        // Desktop: Middle mouse button drag
        container.addEventListener('mousedown', (e: MouseEvent) => {
            // Middle button (button === 1)
            if (e.button !== 1) return;

            e.preventDefault();
            state.isPanning = true;
            state.startX = e.clientX;
            state.startY = e.clientY;
            state.startPanX = state.panX;
            state.startPanY = state.panY;

            container.style.cursor = 'grabbing';
        }, { signal });

        document.addEventListener('mousemove', (e: MouseEvent) => {
            // Only pan on middle-mouse drag (not touch-initiated panning)
            if (!state.isPanning || touchIdentifier !== null) return;

            const deltaX = e.clientX - state.startX;
            const deltaY = e.clientY - state.startY;

            state.panX = state.startPanX + deltaX;
            state.panY = state.startPanY + deltaY;

            applyTransform(container, canvasId);
        }, { signal });

        document.addEventListener('mouseup', (e: MouseEvent) => {
            if (!state.isPanning || touchIdentifier !== null) return;
            if (e.button !== 1) return;

            state.isPanning = false;
            container.style.cursor = '';

            saveTransformState(canvasId);
            log.debug(SEG.GLYPH, '[CanvasPan] Middle mouse pan end', { panX: state.panX, panY: state.panY });
        }, { signal });
    }

    // Touch handlers: Always set up for mobile and responsive design mode support
    container.addEventListener('touchstart', (e: TouchEvent) => {
        if (e.touches.length === 2) {
            // Two-finger pinch zoom
            const touch1 = e.touches[0];
            const touch2 = e.touches[1];

            state.isPinching = true;
            state.isPanning = false;
            touchIdentifier = null;

            // Calculate initial distance between touches
            state.startDistance = Math.hypot(
                touch2.clientX - touch1.clientX,
                touch2.clientY - touch1.clientY
            );
            state.startScale = state.scale;

            // Calculate pinch center
            state.startX = (touch1.clientX + touch2.clientX) / 2;
            state.startY = (touch1.clientY + touch2.clientY) / 2;
        } else if (e.touches.length === 1) {
            // Single touch pan
            const touch = e.touches[0];
            touchIdentifier = touch.identifier;
            state.isPanning = true;
            state.isPinching = false;
            state.startX = touch.clientX;
            state.startY = touch.clientY;
            state.startPanX = state.panX;
            state.startPanY = state.panY;
        }
    }, { signal, passive: true });

    container.addEventListener('touchmove', (e: TouchEvent) => {
        if (state.isPinching && e.touches.length === 2) {
            // Two-finger pinch zoom
            e.preventDefault();

            const touch1 = e.touches[0];
            const touch2 = e.touches[1];

            // Calculate current distance between touches
            const currentDistance = Math.hypot(
                touch2.clientX - touch1.clientX,
                touch2.clientY - touch1.clientY
            );

            // Calculate scale change
            const scaleChange = currentDistance / state.startDistance;
            const oldScale = state.startScale;
            const newScale = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, oldScale * scaleChange));

            // Calculate pinch center relative to container
            const rect = container.getBoundingClientRect();
            const centerX = (touch1.clientX + touch2.clientX) / 2 - rect.left;
            const centerY = (touch1.clientY + touch2.clientY) / 2 - rect.top;

            // Apply zoom origin math - keep pinch center stationary
            const scaleFactor = newScale / oldScale;
            state.panX = centerX - (centerX - state.panX) * scaleFactor;
            state.panY = centerY - (centerY - state.panY) * scaleFactor;
            state.scale = newScale;

            applyTransform(container, canvasId);
        } else if (state.isPanning && touchIdentifier !== null) {
            // Single touch pan
            const touch = Array.from(e.touches).find(t => t.identifier === touchIdentifier);
            if (!touch) return;

            e.preventDefault();

            const deltaX = touch.clientX - state.startX;
            const deltaY = touch.clientY - state.startY;

            state.panX = state.startPanX + deltaX;
            state.panY = state.startPanY + deltaY;

            applyTransform(container, canvasId);
        }
    }, { signal, passive: false });

    container.addEventListener('touchend', (e: TouchEvent) => {
        if (state.isPinching) {
            // End pinch zoom
            if (e.touches.length < 2) {
                state.isPinching = false;
                saveTransformState(canvasId);
            }
        } else if (state.isPanning && touchIdentifier !== null) {
            // Check if our touch ended
            const ended = Array.from(e.changedTouches).some(t => t.identifier === touchIdentifier);
            if (!ended) return;

            state.isPanning = false;
            touchIdentifier = null;

            saveTransformState(canvasId);
        }
    }, { signal });

    return controller;
}

/**
 * Get current pan offset (for coordinate transforms)
 * @deprecated Use getTransform instead
 */
export function getPanOffset(canvasId: string): { panX: number; panY: number } {
    const state = getState(canvasId);
    return { panX: state.panX, panY: state.panY };
}

/**
 * Get current transform state (pan and zoom)
 */
export function getTransform(canvasId: string): { panX: number; panY: number; scale: number } {
    const state = getState(canvasId);
    return { panX: state.panX, panY: state.panY, scale: state.scale };
}

/**
 * Set zoom level programmatically
 * @param container Canvas container element
 * @param canvasId Canvas identifier
 * @param scale Target zoom scale (clamped to ZOOM_MIN..ZOOM_MAX)
 * @param centerX Optional zoom center X (container-relative). Defaults to (0, 0) canvas origin.
 * @param centerY Optional zoom center Y (container-relative). Defaults to (0, 0) canvas origin.
 */
export function setZoom(container: HTMLElement, canvasId: string, scale: number, centerX?: number, centerY?: number): void {
    const state = getState(canvasId);
    const oldScale = state.scale;
    const newScale = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, scale));

    // Default to (0, 0) canvas origin - predictable zoom toward origin point
    const zoomCenterX = centerX ?? 0;
    const zoomCenterY = centerY ?? 0;

    // Apply zoom origin math
    const scaleFactor = newScale / oldScale;
    state.panX = zoomCenterX - (zoomCenterX - state.panX) * scaleFactor;
    state.panY = zoomCenterY - (zoomCenterY - state.panY) * scaleFactor;
    state.scale = newScale;

    applyTransform(container, canvasId);
    saveTransformState(canvasId);
}

/**
 * Reset transform to default (no pan, 100% zoom) with smooth animation
 * @param container Canvas container element
 * @param canvasId Canvas identifier
 */
export function resetTransform(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;

    // Enable CSS transition for smooth animation
    if (contentLayer) {
        contentLayer.style.transition = 'transform 0.55s ease-out';
    }

    state.panX = 0;
    state.panY = 0;
    state.scale = 1.0;
    applyTransform(container, canvasId);
    saveTransformState(canvasId);

    // Remove transition after animation completes
    if (contentLayer) {
        setTimeout(() => {
            contentLayer.style.transition = '';
        }, 550);
    }
}

/**
 * Pan the canvas so a glyph element is visible.
 * If the glyph center is already within the viewport, no movement occurs.
 * Otherwise, smoothly pans to bring the glyph center into the viewport center.
 */
export function panToGlyph(container: HTMLElement, canvasId: string, glyphEl: HTMLElement): void {
    const state = getState(canvasId);
    const vw = container.clientWidth;
    const vh = container.clientHeight;

    // Glyph position in canvas coords (from CSS left/top)
    const gx = glyphEl.offsetLeft + glyphEl.offsetWidth / 2;
    const gy = glyphEl.offsetTop + glyphEl.offsetHeight / 2;

    // Glyph center in screen coords
    const screenX = gx * state.scale + state.panX;
    const screenY = gy * state.scale + state.panY;

    // Margin: consider visible if within 10% inset of viewport
    const mx = vw * 0.1;
    const my = vh * 0.1;
    if (screenX >= mx && screenX <= vw - mx && screenY >= my && screenY <= vh - my) {
        return; // already visible
    }

    // Pan so glyph center maps to viewport center
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
    if (contentLayer) {
        contentLayer.style.transition = 'transform 0.35s ease-out';
    }

    state.panX = vw / 2 - gx * state.scale;
    state.panY = vh / 2 - gy * state.scale;
    applyTransform(container, canvasId);
    saveTransformState(canvasId);

    if (contentLayer) {
        setTimeout(() => { contentLayer.style.transition = ''; }, 350);
    }
}

/** Vertical position of the symbol on screen during thread nav (fraction of viewport height from top) */
const THREAD_NAV_SYMBOL_Y_FRACTION = 1 / 3;

/**
 * Frame a glyph for thread navigation.
 *
 * - Horizontal: center on the glyph wrapper (the glyph reads visually balanced
 *   even when the symbol is offset within the wrapper, e.g., title-bar icon).
 * - Vertical: place the symbol upper-aligned (at ~1/3 from the top of the
 *   viewport) so the symbol — where the thread line passes — sits in the
 *   upper portion of the screen and the glyph body extends downward into
 *   visible space.
 *
 * Always pans, no "already visible" check.
 */
export function centerOnGlyphSymbol(container: HTMLElement, canvasId: string, glyphEl: HTMLElement): void {
    const state = getState(canvasId);
    const vw = container.clientWidth;
    const vh = container.clientHeight;
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) return;

    // Horizontal target: wrapper center, accumulated through offsetParent chain
    let wrapperCx = glyphEl.offsetWidth / 2;
    let wEl: HTMLElement | null = glyphEl;
    while (wEl && wEl !== contentLayer) {
        wrapperCx += wEl.offsetLeft;
        wEl = wEl.offsetParent as HTMLElement | null;
    }

    // Vertical target: symbol center, accumulated through offsetParent chain
    const symbolEl = (glyphEl.querySelector('.glyph-symbol') as HTMLElement | null) ?? glyphEl;
    let symbolCy = symbolEl.offsetHeight / 2;
    let sEl: HTMLElement | null = symbolEl;
    while (sEl && sEl !== contentLayer) {
        symbolCy += sEl.offsetTop;
        sEl = sEl.offsetParent as HTMLElement | null;
    }

    contentLayer.style.transition = 'transform 0.35s ease-out';
    state.panX = vw / 2 - wrapperCx * state.scale;
    state.panY = vh * THREAD_NAV_SYMBOL_Y_FRACTION - symbolCy * state.scale;
    applyTransform(container, canvasId);
    saveTransformState(canvasId);
    setTimeout(() => { contentLayer.style.transition = ''; }, 350);
}

/**
 * Convert screen coordinates to canvas coordinates
 * Takes a screen point and returns the corresponding canvas point accounting for pan and zoom
 */
export function screenToCanvas(canvasId: string, screenX: number, screenY: number): { x: number; y: number } {
    const state = getState(canvasId);
    // Inverse transform: (screen - pan) / scale
    return {
        x: (screenX - state.panX) / state.scale,
        y: (screenY - state.panY) / state.scale,
    };
}

/**
 * Convert canvas coordinates to screen coordinates
 * Takes a canvas point and returns the corresponding screen point accounting for pan and zoom
 */
export function canvasToScreen(canvasId: string, canvasX: number, canvasY: number): { x: number; y: number } {
    const state = getState(canvasId);
    // Forward transform: canvas * scale + pan
    return {
        x: canvasX * state.scale + state.panX,
        y: canvasY * state.scale + state.panY,
    };
}

/**
 * Flush pending debounced save immediately (for testing)
 */
export function flushSaveState(): void {
    if (saveTimer && pendingSaveCanvasId) {
        clearTimeout(saveTimer);
        const canvasId = pendingSaveCanvasId;
        saveTimer = null;
        pendingSaveCanvasId = null;
        if (typeof uiState.setCanvasPan === 'function') {
            const state = getState(canvasId);
            uiState.setCanvasPan(canvasId, { panX: state.panX, panY: state.panY, scale: state.scale });
        }
    }
}

/**
 * Reset canvas state (for testing)
 */
export function resetCanvasState(canvasId: string): void {
    canvasStates.delete(canvasId);
}
