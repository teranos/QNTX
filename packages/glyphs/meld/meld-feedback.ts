/**
 * Meld feedback — visual proximity cues during glyph dragging.
 *
 * Direction-aware box shadows that glow toward the meld edge.
 * Shared by both detection (mousemove) and composition (performMeld/extendComposition).
 *
 * A glyph that owns an inline boxShadow keeps it: the prior value is saved
 * before the glow is applied and put back when feedback clears — everything
 * about a glyph survives every transition (Element Axioma).
 */

import type { EdgeDirection } from './meldability';
import { PROXIMITY_THRESHOLD, MELD_THRESHOLD } from './meld-detect';

// The saved pre-feedback shadow lives on the element while the glow does.
// Its presence (even empty) also marks the element for cleanup — including
// the "approaching" state, which glows without a meld class.
const PRIOR_SHADOW_ATTR = 'data-meld-prior-shadow';

function applyFeedbackShadow(element: HTMLElement, shadow: string): void {
    if (!element.hasAttribute(PRIOR_SHADOW_ATTR)) {
        element.setAttribute(PRIOR_SHADOW_ATTR, element.style.boxShadow);
    }
    element.style.boxShadow = shadow;
}

/** Remove the glow from one element, restoring the shadow it owned before. */
export function clearFeedbackShadow(element: HTMLElement): void {
    if (element.hasAttribute(PRIOR_SHADOW_ATTR)) {
        element.style.boxShadow = element.getAttribute(PRIOR_SHADOW_ATTR) ?? '';
        element.removeAttribute(PRIOR_SHADOW_ATTR);
    }
    element.classList.remove('meld-ready');
    element.classList.remove('meld-target');
}

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
        applyFeedbackShadow(initiatorElement, `${strongOffset} 20px rgba(255, 69, 0, ${intensity * 0.6})`);
        applyFeedbackShadow(targetElement, `${strongOffsetReverse} 20px rgba(255, 69, 0, ${intensity * 0.6})`);
        initiatorElement.classList.add('meld-ready');
        targetElement.classList.add('meld-target');
    } else {
        // Approaching - mild glow
        const glowIntensity = intensity * 0.3;
        applyFeedbackShadow(initiatorElement, `${mildOffset} 10px rgba(255, 140, 0, ${glowIntensity})`);
        applyFeedbackShadow(targetElement, `${mildOffsetReverse} 10px rgba(255, 140, 0, ${glowIntensity})`);
    }
}

/**
 * Clear meld feedback from elements.
 *
 * Clears the element itself AND any glowing element on the canvas — found by
 * meld class or by the saved-shadow attribute, which also catches the
 * "approaching" state where the glow is applied without a class. Each element
 * gets back the boxShadow it owned before the glow.
 */
export function clearMeldFeedback(element: HTMLElement): void {
    clearFeedbackShadow(element);

    // Walk up to canvas and clear ALL elements with meld feedback
    const canvas = element.closest('.canvas-workspace') ?? element.parentElement;
    if (canvas) {
        canvas.querySelectorAll(`.meld-target, .meld-ready, [${PRIOR_SHADOW_ATTR}]`).forEach(el => {
            clearFeedbackShadow(el as HTMLElement);
        });
    }
}
