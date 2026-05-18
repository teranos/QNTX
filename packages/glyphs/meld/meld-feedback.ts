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

    // Single-edge box-shadows using negative spread to confine glow to one side.
    // Format: offset-x offset-y blur spread color
    // Negative spread shrinks the shadow so it only bleeds from the offset edge.
    //   [initiator shadow, target shadow]
    const edgeShadow = (ox: string, oy: string, blur: number, spread: number, alpha: number): [string, string] => {
        const c = `rgba(255, 69, 0, ${alpha})`;
        // Initiator: glow toward target. Target: glow toward initiator (negated offset).
        return [
            `${ox} ${oy} ${blur}px ${spread}px ${c}`,
            `${ox.startsWith('-') ? ox.slice(1) : '-' + ox} ${oy.startsWith('-') ? oy.slice(1) : '-' + oy} ${blur}px ${spread}px ${c}`,
        ];
    };

    // Offset/spread per direction — initiator's meld edge
    const cfg: Record<EdgeDirection, [string, string]> = {
        right:  ['8px', '0'],
        bottom: ['0', '8px'],
        top:    ['0', '-8px'],
    };
    const [ox, oy] = cfg[direction];

    if (distance < MELD_THRESHOLD) {
        const [iShadow, tShadow] = edgeShadow(ox, oy, 12, -4, intensity * 0.6);
        initiatorElement.style.boxShadow = iShadow;
        targetElement.style.boxShadow = tShadow;
        initiatorElement.classList.add('meld-ready');
        targetElement.classList.add('meld-target');
    } else {
        const [iShadow, tShadow] = edgeShadow(ox, oy, 8, -4, intensity * 0.3);
        initiatorElement.style.boxShadow = iShadow;
        targetElement.style.boxShadow = tShadow;
    }
}

/**
 * Clear meld feedback from elements.
 *
 * Clears the element itself AND any elements tagged with .meld-ready or
 * .meld-target on the canvas. Also clears boxShadow from both classes
 * to catch the "approaching" state where boxShadow is set without a class.
 */
export function clearMeldFeedback(element: HTMLElement): void {
    element.style.boxShadow = '';
    element.classList.remove('meld-ready');
    element.classList.remove('meld-target');

    // Walk up to canvas and clear ALL elements with meld feedback classes or shadows
    const canvas = element.closest('.canvas-workspace') ?? element.parentElement;
    if (canvas) {
        canvas.querySelectorAll('.meld-target, .meld-ready').forEach(el => {
            el.classList.remove('meld-target');
            el.classList.remove('meld-ready');
            (el as HTMLElement).style.boxShadow = '';
        });
    }
}
