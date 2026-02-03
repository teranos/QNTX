/**
 * Meld System - Axiom-respecting glyph melding
 *
 * CRITICAL: This implementation respects the core glyph axiom:
 * "A Glyph is exactly ONE DOM element for its entire lifetime"
 *
 * NO cloneNode. NO createElement for existing glyphs.
 * Melding is achieved through reparenting, not cloning.
 */

import { log, SEG } from '../../logger';
import type { Glyph } from './glyph';
import { MELDABILITY, getInitiatorClasses, getTargetClasses } from './meldability';

// Configuration
export const PROXIMITY_THRESHOLD = 100; // px - distance at which attraction starts
export const MELD_THRESHOLD = 30; // px - distance at which glyphs meld
const UNMELD_OFFSET = 420; // px - horizontal spacing between glyphs when unmelding
const MIN_VERTICAL_ALIGNMENT = 0.3; // fraction - minimum vertical overlap required (30%)

/**
 * Check if element can initiate melding
 */
export function canInitiateMeld(element: HTMLElement): boolean {
    return getInitiatorClasses().some(cls =>
        element.classList.contains(cls)
    );
}

/**
 * Check if element can receive meld
 */
export function canReceiveMeld(element: HTMLElement): boolean {
    return getTargetClasses().some(cls =>
        element.classList.contains(cls)
    );
}

/**
 * Check if two elements are compatible for melding
 */
function areCompatible(initiator: HTMLElement, target: HTMLElement): boolean {
    for (const [initiatorClass, targetClasses] of Object.entries(MELDABILITY)) {
        if (initiator.classList.contains(initiatorClass)) {
            return targetClasses.some(cls => target.classList.contains(cls));
        }
    }
    return false;
}

/**
 * Find nearest meldable target for an initiator glyph
 */
export function findMeldTarget(initiatorElement: HTMLElement): { target: HTMLElement | null; distance: number } {
    if (!canInitiateMeld(initiatorElement)) {
        return { target: null, distance: Infinity };
    }

    const canvas = initiatorElement.parentElement;
    if (!canvas) {
        return { target: null, distance: Infinity };
    }

    // Find all potential targets based on meldability rules
    const potentialTargets: HTMLElement[] = [];
    for (const targetClass of getTargetClasses()) {
        canvas.querySelectorAll(`.${targetClass}`).forEach(el => {
            potentialTargets.push(el as HTMLElement);
        });
    }

    let closestTarget: HTMLElement | null = null;
    let closestDistance = Infinity;

    const initiatorRect = initiatorElement.getBoundingClientRect();

    potentialTargets.forEach(targetElement => {
        // Skip if not compatible with this specific initiator
        if (!areCompatible(initiatorElement, targetElement)) {
            return;
        }

        // Skip if already in a meld
        if (targetElement.parentElement?.classList.contains('melded-composition')) {
            return;
        }

        const targetRect = targetElement.getBoundingClientRect();

        // Check vertical alignment
        const verticalOverlap = Math.min(initiatorRect.bottom, targetRect.bottom) -
                              Math.max(initiatorRect.top, targetRect.top);
        const minHeight = Math.min(initiatorRect.height, targetRect.height);

        if (verticalOverlap < minHeight * MIN_VERTICAL_ALIGNMENT) {
            return; // Not aligned vertically
        }

        // Check if initiator is to the left of target (correct orientation)
        if (initiatorRect.right > targetRect.left) {
            return; // Wrong orientation
        }

        // Calculate distance between right edge of initiator and left edge of target
        const distance = targetRect.left - initiatorRect.right;

        if (distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
            closestDistance = distance;
            closestTarget = targetElement;
        }
    });

    return { target: closestTarget, distance: closestDistance };
}

/**
 * Apply visual feedback for meld proximity
 * This modifies styles in place - no new elements created
 */
export function applyMeldFeedback(
    axElement: HTMLElement,
    promptElement: HTMLElement | null,
    distance: number
): void {
    // Clear any existing feedback
    clearMeldFeedback(axElement);

    if (!promptElement || distance >= PROXIMITY_THRESHOLD) {
        return;
    }

    const intensity = 1 - (distance / PROXIMITY_THRESHOLD);

    // Apply glow based on distance
    if (distance < MELD_THRESHOLD) {
        // Ready to meld - strong glow
        axElement.style.boxShadow = `10px 0 20px rgba(255, 69, 0, ${intensity * 0.6})`;
        promptElement.style.boxShadow = `-10px 0 20px rgba(255, 69, 0, ${intensity * 0.6})`;
        axElement.classList.add('meld-ready');
        promptElement.classList.add('meld-target');
    } else {
        // Approaching - mild glow
        const glowIntensity = intensity * 0.3;
        axElement.style.boxShadow = `5px 0 10px rgba(255, 140, 0, ${glowIntensity})`;
        promptElement.style.boxShadow = `-5px 0 10px rgba(255, 140, 0, ${glowIntensity})`;
    }
}

/**
 * Clear meld feedback from elements
 */
export function clearMeldFeedback(element: HTMLElement): void {
    element.style.boxShadow = '';
    element.classList.remove('meld-ready');

    // Clear from any potential targets
    const canvas = element.parentElement;
    if (canvas) {
        canvas.querySelectorAll('.meld-target').forEach(target => {
            target.classList.remove('meld-target');
            (target as HTMLElement).style.boxShadow = '';
        });
    }
}

/**
 * Perform meld operation
 * CRITICAL: This reparents the actual DOM elements, does NOT clone them
 */
export function performMeld(
    axElement: HTMLElement,
    promptElement: HTMLElement,
    axGlyph: Glyph,
    promptGlyph: Glyph
): HTMLElement {
    const canvas = axElement.parentElement;
    if (!canvas) {
        throw new Error('Cannot meld: no canvas parent');
    }

    log.info(SEG.UI, '[MeldSystem] Performing meld - reparenting elements');

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-ax-id', axGlyph.id);
    composition.setAttribute('data-prompt-id', promptGlyph.id);

    // Position at ax location
    composition.style.position = 'absolute';
    composition.style.left = axElement.style.left;
    composition.style.top = axElement.style.top;
    composition.style.display = 'flex';
    composition.style.alignItems = 'center';

    // Clear positioning from glyphs (they're now relative to composition)
    axElement.style.position = 'relative';
    axElement.style.left = '0';
    axElement.style.top = '0';
    promptElement.style.position = 'relative';
    promptElement.style.left = '0';
    promptElement.style.top = '0';

    // Clear meld feedback
    clearMeldFeedback(axElement);
    clearMeldFeedback(promptElement);

    // REPARENT the actual elements (NOT clones!)
    composition.appendChild(axElement);
    composition.appendChild(promptElement);

    // Add to canvas
    canvas.appendChild(composition);

    // Dispatch event
    const meldEvent = new CustomEvent('glyph:melded', {
        detail: {
            composition,
            axElement,
            promptElement,
            axGlyph,
            promptGlyph
        }
    });
    document.dispatchEvent(meldEvent);

    log.info(SEG.UI, '[MeldSystem] Meld complete - elements reparented');

    return composition;
}

/**
 * Check if element is a melded composition
 */
export function isMeldedComposition(element: HTMLElement): boolean {
    return element.classList.contains('melded-composition');
}

/**
 * Unmeld a composition back to individual glyphs
 * Restores the original elements to canvas
 */
export function unmeldComposition(composition: HTMLElement): void {
    if (!isMeldedComposition(composition)) {
        log.warn(SEG.UI, '[MeldSystem] Not a melded composition');
        return;
    }

    const canvas = composition.parentElement;
    if (!canvas) return;

    const axElement = composition.querySelector('.canvas-ax-glyph') as HTMLElement;
    const promptElement = composition.querySelector('.canvas-prompt-glyph') as HTMLElement;

    if (!axElement || !promptElement) {
        log.error(SEG.UI, '[MeldSystem] Missing glyphs in composition');
        return;
    }

    // Restore absolute positioning
    const compLeft = parseInt(composition.style.left || '0', 10);
    const compTop = parseInt(composition.style.top || '0', 10);

    // Validate parsed values - fallback to 0 if NaN
    const left = isNaN(compLeft) ? 0 : compLeft;
    const top = isNaN(compTop) ? 0 : compTop;

    axElement.style.position = 'absolute';
    axElement.style.left = `${left}px`;
    axElement.style.top = `${top}px`;

    promptElement.style.position = 'absolute';
    promptElement.style.left = `${left + UNMELD_OFFSET}px`;
    promptElement.style.top = `${top}px`;

    // Reparent back to canvas
    canvas.insertBefore(axElement, composition);
    canvas.insertBefore(promptElement, composition);

    // Remove composition container
    composition.remove();

    // Dispatch event
    const unmeldEvent = new CustomEvent('glyph:unmelded', {
        detail: { axElement, promptElement }
    });
    document.dispatchEvent(unmeldEvent);

    log.info(SEG.UI, '[MeldSystem] Unmeld complete - elements restored');
}