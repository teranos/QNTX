/**
 * Meld feedback — visual proximity cues during glyph dragging.
 *
 * Direction-aware box shadows that glow toward the meld edge.
 * Shared by both detection (mousemove) and composition (performMeld/extendComposition).
 */

import type { EdgeDirection } from './meldability';
import { PROXIMITY_THRESHOLD, MELD_THRESHOLD } from './meld-detect';

/**
 * Apply visual feedback for meld proximity
 * This modifies styles in place - no new elements created
 */
export function applyMeldFeedback(
    initiatorElement: HTMLElement,
    targetElement: HTMLElement | null,
    distance: number,
    direction: EdgeDirection = 'right'
): void {
    // Clear any existing feedback
    clearMeldFeedback(initiatorElement);

    if (!targetElement || distance >= PROXIMITY_THRESHOLD) {
        return;
    }

    const intensity = 1 - (distance / PROXIMITY_THRESHOLD);
    const isVertical = direction === 'bottom' || direction === 'top';

    // Shadow offsets: glow toward the meld edge
    const strongOffset = isVertical ? '0 10px' : '10px 0';
    const strongOffsetReverse = isVertical ? '0 -10px' : '-10px 0';
    const mildOffset = isVertical ? '0 5px' : '5px 0';
    const mildOffsetReverse = isVertical ? '0 -5px' : '-5px 0';

    // Apply glow based on distance
    if (distance < MELD_THRESHOLD) {
        // Ready to meld - strong glow
        initiatorElement.style.boxShadow = `${strongOffset} 20px rgba(255, 69, 0, ${intensity * 0.6})`;
        targetElement.style.boxShadow = `${strongOffsetReverse} 20px rgba(255, 69, 0, ${intensity * 0.6})`;
        initiatorElement.classList.add('meld-ready');
        targetElement.classList.add('meld-target');
    } else {
        // Approaching - mild glow
        const glowIntensity = intensity * 0.3;
        initiatorElement.style.boxShadow = `${mildOffset} 10px rgba(255, 140, 0, ${glowIntensity})`;
        targetElement.style.boxShadow = `${mildOffsetReverse} 10px rgba(255, 140, 0, ${glowIntensity})`;
    }
}

/**
 * Clear meld feedback from elements
 */
export function clearMeldFeedback(element: HTMLElement): void {
    element.style.boxShadow = '';
    element.classList.remove('meld-ready');

    // Clear from any potential targets — walk up to canvas to handle
    // elements inside compositions or sub-containers
    const canvas = element.closest('.canvas-workspace') ?? element.parentElement;
    if (canvas) {
        canvas.querySelectorAll('.meld-target').forEach(target => {
            target.classList.remove('meld-target');
            (target as HTMLElement).style.boxShadow = '';
        });
    }
}
