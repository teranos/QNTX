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
function saveTransformState(canvasId: string): void {
    // Check if uiState methods are available (may not be in test environments)
    if (typeof uiState.setCanvasPan !== 'function') return;

    const state = getState(canvasId);
    uiState.setCanvasPan(canvasId, { panX: state.panX, panY: state.panY, scale: state.scale });
}

/**
 * Setup canvas pan handlers
 */
export function setupCanvasPan(container: HTMLElement, canvasId: string): AbortController {
    const controller = new AbortController();
    const signal = controller.signal;
    const state = getState(canvasId);

    // Load persisted pan state
    loadTransformState(canvasId);
    applyTransform(container, canvasId);

    // Detect mobile for desktop-specific handlers
    const isMobile = window.matchMedia('(max-width: 768px)').matches;

    // Desktop: Two-finger trackpad scroll (wheel event) and middle mouse drag
    if (!isMobile) {
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
            if (!state.isPanning) return;

            const deltaX = e.clientX - state.startX;
            const deltaY = e.clientY - state.startY;

            state.panX = state.startPanX + deltaX;
            state.panY = state.startPanY + deltaY;

            applyTransform(container, canvasId);
        }, { signal });

        document.addEventListener('mouseup', (e: MouseEvent) => {
            if (!state.isPanning) return;
            if (e.button !== 1) return;

            state.isPanning = false;
            container.style.cursor = '';

            saveTransformState(canvasId);
            log.debug(SEG.GLYPH, '[CanvasPan] Middle mouse pan end', { panX: state.panX, panY: state.panY });
        }, { signal });
    }

    // Touch handlers: Always set up for mobile and responsive design mode support
    let touchIdentifier: number | null = null;

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

    // Keyboard shortcuts for canvas navigation
    document.addEventListener('keydown', (e: KeyboardEvent) => {
        // Only handle if canvas container or its children have focus
        if (!container.contains(document.activeElement) && document.activeElement !== container) {
            return;
        }

        // '0' key: Reset zoom and pan to origin
        if (e.key === '0' && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey) {
            e.preventDefault();
            resetTransform(container, canvasId);
            log.debug(SEG.GLYPH, '[CanvasPan] Reset transform via keyboard (0 key)');
        }

        // TODO: '1' key: Fit all glyphs in view
        // Calculate bounding box of all canvas glyphs and zoom/pan to show everything
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
 * Reset canvas state (for testing)
 */
export function resetCanvasState(canvasId: string): void {
    canvasStates.delete(canvasId);
}
