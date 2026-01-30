// UI element visibility management for focus mode
// Handles sliding panels, overlay, and responsive layout adaptations

import { GRAPH_PHYSICS } from '../../config.ts';
import { getDomCache } from '../state.ts';

// Forward declare unfocus function to avoid circular dependency
// This will be set by the focus module when it imports this
let unfocusCallback: (() => void) | null = null;

export function setUnfocusCallback(callback: () => void): void {
    unfocusCallback = callback;
}

/**
 * Show or hide UI elements based on focus state
 * Slides panels out of view during focus, restores them on unfocus
 * Creates dark overlay to emphasize focused tile
 */
export function setFocusUIVisibility(visible: boolean): void {
    const duration = GRAPH_PHYSICS.ANIMATION_DURATION;
    const transition = `transform ${duration}ms ease, opacity ${duration}ms ease`;

    // Darken the graph background when focused using a CSS overlay
    // This ensures the overlay is clipped to the graph-viewer bounds
    const container = document.getElementById('graph-viewer');
    if (container) {
        let overlay = container.querySelector('.focus-overlay') as HTMLElement;

        if (visible) {
            // Remove overlay when unfocusing
            if (overlay) {
                overlay.style.opacity = '0';
                setTimeout(() => overlay?.remove(), duration);
            }
        } else {
            // Create overlay when focusing
            if (!overlay) {
                overlay = document.createElement('div');
                overlay.className = 'focus-overlay';
                overlay.style.cssText = `
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    bottom: 0;
                    background: rgba(0, 0, 0, 0.4);
                    pointer-events: auto;
                    cursor: pointer;
                    opacity: 0;
                    transition: opacity ${duration}ms ease;
                    z-index: 0;
                `;
                // Click overlay to unfocus
                overlay.addEventListener('click', (event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    if (unfocusCallback) unfocusCallback();
                });
                container.appendChild(overlay);
            }
            // Trigger opacity transition
            requestAnimationFrame(() => {
                if (overlay) overlay.style.opacity = '1';
            });
        }
    }

    // Helper to slide element left
    const slideLeft = (el: HTMLElement | null) => {
        if (!el) return;
        el.style.setProperty('transition', transition, 'important');
        if (visible) {
            el.style.setProperty('transform', 'translateX(0)', 'important');
            el.style.setProperty('opacity', '1', 'important');
            el.style.setProperty('pointer-events', 'auto', 'important');
        } else {
            el.style.setProperty('transform', 'translateX(-100%)', 'important');
            el.style.setProperty('opacity', '0', 'important');
            el.style.setProperty('pointer-events', 'none', 'important');
        }
    };

    const domCache = getDomCache();

    // Left side elements (slide left)
    slideLeft(domCache.get('typeAttestations', '.type-attestations'));
    slideLeft(document.getElementById('left-panel'));
    // TODO: When #controls is renamed to #type-attestations-container, update this selector
    slideLeft(document.getElementById('controls')); // Contains type attestations

    // Expand graph-viewer to full width when focused
    const graphContainer = domCache.get('graphViewer', '#graph-viewer');
    if (graphContainer) {
        graphContainer.style.setProperty('transition', transition, 'important');
        if (visible) {
            // Restore normal flex layout
            graphContainer.style.removeProperty('position');
            graphContainer.style.removeProperty('left');
            graphContainer.style.removeProperty('right');
            graphContainer.style.removeProperty('width');
        } else {
            // Expand to fill entire viewport
            graphContainer.style.setProperty('position', 'fixed', 'important');
            graphContainer.style.setProperty('left', '0', 'important');
            graphContainer.style.setProperty('right', '0', 'important');
            graphContainer.style.setProperty('width', '100%', 'important');
        }
    }

    // Virtue #9: Responsive Intent - Adapt to device context, not just size
    // System drawer slides based on position (top on mobile, bottom on desktop)
    const systemDrawer = document.getElementById('system-drawer');
    if (systemDrawer) {
        const computedStyle = window.getComputedStyle(systemDrawer);
        const isAtTop = computedStyle.top !== 'auto' && computedStyle.bottom === 'auto';

        systemDrawer.style.transition = transition;
        if (visible) {
            systemDrawer.style.transform = 'translateY(0)';
            systemDrawer.style.opacity = '1';
            systemDrawer.style.pointerEvents = 'auto';
        } else {
            // Slide up if at top (mobile), slide down if at bottom (desktop)
            systemDrawer.style.transform = isAtTop ? 'translateY(-120%)' : 'translateY(120%)';
            systemDrawer.style.opacity = '0.5';
            systemDrawer.style.pointerEvents = 'none';
        }
    }

    // Symbol palette (slides based on position)
    // - Mobile (bottom): slides down
    // - Tablet portrait (fixed left): slides left
    // - Desktop (in left panel): slides left with parent
    const symbolPalette = document.getElementById('symbolPalette');
    if (symbolPalette) {
        const computedStyle = window.getComputedStyle(symbolPalette);
        const isFixed = computedStyle.position === 'fixed';
        const isAtLeft = isFixed && computedStyle.left === '0px';

        symbolPalette.style.transition = transition;
        if (visible) {
            symbolPalette.style.transform = isAtLeft ? 'translateX(0) translateY(-50%)' : 'translateY(0)';
            symbolPalette.style.opacity = '1';
            symbolPalette.style.pointerEvents = 'auto';
        } else {
            if (isAtLeft) {
                // Tablet portrait - fixed left column, slide left
                symbolPalette.style.transform = 'translateX(-120%) translateY(-50%)';
            } else {
                // Mobile/desktop - slide down (mobile at bottom) or handled by parent
                symbolPalette.style.transform = 'translateY(120%)';
            }
            symbolPalette.style.opacity = '0.5';
            symbolPalette.style.pointerEvents = 'none';
        }
    }
}
