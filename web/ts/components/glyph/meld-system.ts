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
import type { CompositionEdge } from '../../state/ui';
import { MELDABILITY, getInitiatorClasses, getTargetClasses } from './meldability';
import { addComposition, removeComposition, extractGlyphIds } from '../../state/compositions';

// Configuration
export const PROXIMITY_THRESHOLD = 100; // px - distance at which proximity feedback starts
export const MELD_THRESHOLD = 30; // px - distance at which glyphs meld
const UNMELD_OFFSET = 20; // px - horizontal spacing between glyphs when unmelding (gentle separation)
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
 *
 * Compositions are now persisted to storage and survive page refresh.
 *
 * TODO: Support multi-glyph chains (ax|python|prompt).
 * Current implementation only supports binary melding (two glyphs).
 * Will require: composition-to-glyph melding, recursive DOM structure.
 * Tracked in: https://github.com/teranos/QNTX/issues/411
 */
export function performMeld(
    initiatorElement: HTMLElement,
    targetElement: HTMLElement,
    initiatorGlyph: Glyph,
    targetGlyph: Glyph
): HTMLElement {
    const canvas = initiatorElement.parentElement;
    if (!canvas) {
        throw new Error('Cannot meld: no canvas parent');
    }

    log.info(SEG.GLYPH, '[MeldSystem] Performing meld - reparenting elements');

    // Generate composition ID
    const compositionId = `melded-${initiatorGlyph.id}-${targetGlyph.id}`;

    // Create edge directly (DAG-native: one edge for binary meld)
    const edges: CompositionEdge[] = [{
        from: initiatorGlyph.id,
        to: targetGlyph.id,
        direction: 'right',
        position: 0
    }];

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-glyph-id', compositionId);
    composition.setAttribute('data-initiator-id', initiatorGlyph.id);
    composition.setAttribute('data-target-id', targetGlyph.id);

    // Position at initiator location
    composition.style.position = 'absolute';
    composition.style.left = initiatorElement.style.left;
    composition.style.top = initiatorElement.style.top;
    composition.style.display = 'flex';
    composition.style.alignItems = 'center';

    // Parse position for storage
    const x = parseInt(initiatorElement.style.left || '0', 10);
    const y = parseInt(initiatorElement.style.top || '0', 10);

    if (isNaN(x) || isNaN(y)) {
        log.warn(SEG.GLYPH, '[MeldSystem] Invalid position during meld', {
            rawLeft: initiatorElement.style.left,
            rawTop: initiatorElement.style.top,
            parsedX: x,
            parsedY: y
        });
    }

    // Clear positioning from glyphs (they're now relative to composition)
    initiatorElement.style.position = 'relative';
    initiatorElement.style.left = '0';
    initiatorElement.style.top = '0';
    targetElement.style.position = 'relative';
    targetElement.style.left = '0';
    targetElement.style.top = '0';

    // Clear meld feedback
    clearMeldFeedback(initiatorElement);
    clearMeldFeedback(targetElement);

    // REPARENT the actual elements (NOT clones!)
    composition.appendChild(initiatorElement);
    composition.appendChild(targetElement);

    // Add to canvas
    canvas.appendChild(composition);

    // Persist composition to storage
    addComposition({
        id: compositionId,
        edges,
        x: isNaN(x) ? 0 : x,
        y: isNaN(y) ? 0 : y
    });

    log.info(SEG.GLYPH, '[MeldSystem] Meld complete - elements reparented and persisted', {
        compositionId,
        edges: edges.length,
        glyphs: extractGlyphIds(edges)
    });

    return composition;
}

/**
 * Reconstruct a melded composition from storage (without persisting)
 * Used when restoring compositions on page load
 */
export function reconstructMeld(
    glyphElements: HTMLElement[],
    compositionId: string,
    x: number,
    y: number
): HTMLElement {
    if (glyphElements.length === 0) {
        throw new Error('Cannot reconstruct meld: no glyph elements provided');
    }

    const canvas = glyphElements[0].parentElement;
    if (!canvas) {
        throw new Error('Cannot reconstruct meld: no canvas parent');
    }

    log.info(SEG.GLYPH, '[MeldSystem] Reconstructing meld from storage', {
        glyphCount: glyphElements.length
    });

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-glyph-id', compositionId);

    // Position at saved location
    composition.style.position = 'absolute';
    composition.style.left = `${x}px`;
    composition.style.top = `${y}px`;
    composition.style.display = 'flex';
    composition.style.alignItems = 'center';

    // Clear positioning from glyphs and reparent them
    glyphElements.forEach(element => {
        element.style.position = 'relative';
        element.style.left = '0';
        element.style.top = '0';

        // REPARENT the actual element (NOT a clone!)
        composition.appendChild(element);
    });

    // Add to canvas
    canvas.appendChild(composition);

    log.info(SEG.GLYPH, '[MeldSystem] Meld reconstructed', {
        compositionId,
        glyphCount: glyphElements.length
    });

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
 * Restores the original elements to canvas and removes from storage
 *
 * Returns the unmelded elements so caller can restore drag handlers.
 */
export function unmeldComposition(composition: HTMLElement): {
    glyphElements: HTMLElement[];
} | null {
    if (!isMeldedComposition(composition)) {
        log.warn(SEG.GLYPH, '[MeldSystem] Not a melded composition');
        return null;
    }

    const canvas = composition.parentElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[MeldSystem] Composition has no parent canvas');
        return null;
    }

    // Get composition ID for storage removal
    const compositionId = composition.getAttribute('data-glyph-id') || '';

    // Find all child glyphs in composition
    const glyphElements = Array.from(composition.querySelectorAll('[data-glyph-id]')) as HTMLElement[];

    if (glyphElements.length === 0) {
        log.error(SEG.GLYPH, '[MeldSystem] No glyphs found in composition - removing corrupted composition');
        if (compositionId) {
            removeComposition(compositionId);
        }
        composition.remove();
        return null;
    }

    // Restore absolute positioning
    const compLeft = parseInt(composition.style.left || '0', 10);
    const compTop = parseInt(composition.style.top || '0', 10);

    // Validate parsed values - fallback to 0 if NaN
    if (isNaN(compLeft)) {
        log.warn(SEG.GLYPH, `[MeldSystem] Invalid composition.style.left: "${composition.style.left}", using 0`);
    }
    if (isNaN(compTop)) {
        log.warn(SEG.GLYPH, `[MeldSystem] Invalid composition.style.top: "${composition.style.top}", using 0`);
    }
    const left = isNaN(compLeft) ? 0 : compLeft;
    const top = isNaN(compTop) ? 0 : compTop;

    // Restore absolute positioning for each glyph
    glyphElements.forEach((element, index) => {
        element.style.position = 'absolute';
        element.style.left = `${left + (index * UNMELD_OFFSET)}px`;
        element.style.top = `${top}px`;

        // Reparent back to canvas
        canvas.insertBefore(element, composition);
    });

    // Remove composition from storage
    if (compositionId) {
        removeComposition(compositionId);
    }

    // Remove composition container
    composition.remove();

    log.info(SEG.GLYPH, '[MeldSystem] Unmeld complete - elements restored and removed from storage', {
        compositionId,
        glyphCount: glyphElements.length
    });

    // Return elements so caller can restore drag handlers
    return {
        glyphElements
    };
}