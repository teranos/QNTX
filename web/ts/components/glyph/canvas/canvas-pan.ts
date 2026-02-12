/**
 * Canvas Pan - Two-finger scroll and middle mouse pan for canvas navigation
 *
 * Desktop:
 * - Two-finger trackpad scroll (primary)
 * - Middle mouse button drag (fallback for external mouse)
 *
 * Mobile:
 * - Single finger drag to pan
 */

import { log, SEG } from '../../../logger';
import { uiState } from '../../../state/ui';

interface PanState {
    panX: number;
    panY: number;
    isPanning: boolean;
    startX: number;
    startY: number;
    startPanX: number;
    startPanY: number;
}

// Per-canvas state map
const canvasStates = new Map<string, PanState>();

function getState(canvasId: string): PanState {
    if (!canvasStates.has(canvasId)) {
        canvasStates.set(canvasId, {
            panX: 0,
            panY: 0,
            isPanning: false,
            startX: 0,
            startY: 0,
            startPanX: 0,
            startPanY: 0,
        });
    }
    return canvasStates.get(canvasId)!;
}

/**
 * Apply pan transform to canvas content layer
 */
function applyPanTransform(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
    if (contentLayer) {
        contentLayer.style.transform = `translate(${state.panX}px, ${state.panY}px)`;
    }
}

/**
 * Load persisted pan state from uiState
 */
function loadPanState(canvasId: string): void {
    // Check if uiState methods are available (may not be in test environments)
    if (typeof uiState.getCanvasPan !== 'function') return;

    const saved = uiState.getCanvasPan(canvasId);
    if (saved) {
        const state = getState(canvasId);
        state.panX = saved.panX;
        state.panY = saved.panY;
        log.debug(SEG.GLYPH, '[CanvasPan] Loaded pan state', { canvasId, panX: state.panX, panY: state.panY });
    }
}

/**
 * Save pan state to uiState
 */
function savePanState(canvasId: string): void {
    // Check if uiState methods are available (may not be in test environments)
    if (typeof uiState.setCanvasPan !== 'function') return;

    const state = getState(canvasId);
    uiState.setCanvasPan(canvasId, { panX: state.panX, panY: state.panY });
}

/**
 * Setup canvas pan handlers
 */
export function setupCanvasPan(container: HTMLElement, canvasId: string): AbortController {
    const controller = new AbortController();
    const signal = controller.signal;
    const state = getState(canvasId);

    // Load persisted pan state
    loadPanState(canvasId);
    applyPanTransform(container, canvasId);

    // Detect mobile
    const isMobile = window.matchMedia('(max-width: 768px)').matches;

    // Desktop: Two-finger trackpad scroll (wheel event)
    if (!isMobile) {
        container.addEventListener('wheel', (e: WheelEvent) => {
            // Only handle non-pinch scroll (two-finger scroll has ctrlKey === false)
            if (e.ctrlKey) return;

            e.preventDefault();

            // Apply wheel delta to pan
            state.panX -= e.deltaX;
            state.panY -= e.deltaY;

            applyPanTransform(container, canvasId);
            savePanState(canvasId);
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

            applyPanTransform(container, canvasId);
        }, { signal });

        document.addEventListener('mouseup', (e: MouseEvent) => {
            if (!state.isPanning) return;
            if (e.button !== 1) return;

            state.isPanning = false;
            container.style.cursor = '';

            savePanState(canvasId);
            log.debug(SEG.GLYPH, '[CanvasPan] Middle mouse pan end', { panX: state.panX, panY: state.panY });
        }, { signal });
    } else {
        // Mobile: Single finger drag to pan
        let touchIdentifier: number | null = null;

        container.addEventListener('touchstart', (e: TouchEvent) => {
            // Only handle single touch
            if (e.touches.length !== 1) return;

            // Don't pan if touching a glyph or interactive element
            const target = e.target as HTMLElement;
            if (target.closest('[data-glyph-id]') && target.closest('[data-glyph-id]') !== container) {
                return;
            }

            const touch = e.touches[0];
            touchIdentifier = touch.identifier;
            state.isPanning = true;
            state.startX = touch.clientX;
            state.startY = touch.clientY;
            state.startPanX = state.panX;
            state.startPanY = state.panY;
        }, { signal, passive: true });

        container.addEventListener('touchmove', (e: TouchEvent) => {
            if (!state.isPanning || touchIdentifier === null) return;

            // Find our touch
            const touch = Array.from(e.touches).find(t => t.identifier === touchIdentifier);
            if (!touch) return;

            e.preventDefault();

            const deltaX = touch.clientX - state.startX;
            const deltaY = touch.clientY - state.startY;

            state.panX = state.startPanX + deltaX;
            state.panY = state.startPanY + deltaY;

            applyPanTransform(container, canvasId);
        }, { signal, passive: false });

        container.addEventListener('touchend', (e: TouchEvent) => {
            if (!state.isPanning || touchIdentifier === null) return;

            // Check if our touch ended
            const ended = Array.from(e.changedTouches).some(t => t.identifier === touchIdentifier);
            if (!ended) return;

            state.isPanning = false;
            touchIdentifier = null;

            savePanState(canvasId);
        }, { signal });
    }

    return controller;
}

/**
 * Get current pan offset (for coordinate transforms)
 */
export function getPanOffset(canvasId: string): { panX: number; panY: number } {
    const state = getState(canvasId);
    return { panX: state.panX, panY: state.panY };
}

/**
 * Reset canvas state (for testing)
 */
export function resetCanvasState(canvasId: string): void {
    canvasStates.delete(canvasId);
}
