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
import { getInitiatorClasses, getTargetClasses, areClassesCompatible, getGlyphClass, type EdgeDirection } from './meldability';
import { addComposition, removeComposition, extractGlyphIds, findCompositionByGlyph } from '../../state/compositions';

// Configuration
export const PROXIMITY_THRESHOLD = 100; // px - distance at which proximity feedback starts
export const MELD_THRESHOLD = 30; // px - distance at which glyphs meld
const UNMELD_OFFSET = 20; // px - horizontal spacing between glyphs when unmelding (gentle separation)
const MIN_ALIGNMENT = 0.3; // fraction - minimum overlap required on the alignment axis (30%)

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
 * Returns the edge direction if compatible, null otherwise
 */
function areCompatible(initiator: HTMLElement, target: HTMLElement): EdgeDirection | null {
    const initiatorClass = getGlyphClass(initiator);
    const targetClass = getGlyphClass(target);
    if (!initiatorClass || !targetClass) return null;
    return areClassesCompatible(initiatorClass, targetClass);
}

/**
 * Check proximity between two elements for a given direction
 * Returns distance if elements are correctly oriented and aligned, Infinity otherwise
 */
function checkDirectionalProximity(
    initiatorRect: DOMRect,
    targetRect: DOMRect,
    direction: EdgeDirection
): number {
    if (direction === 'right') {
        // Vertical alignment check
        const verticalOverlap = Math.min(initiatorRect.bottom, targetRect.bottom) -
                              Math.max(initiatorRect.top, targetRect.top);
        const minHeight = Math.min(initiatorRect.height, targetRect.height);
        if (minHeight > 0 && verticalOverlap < minHeight * MIN_ALIGNMENT) return Infinity;

        // Initiator must be left of target
        if (initiatorRect.right > targetRect.left) return Infinity;

        return targetRect.left - initiatorRect.right;
    }

    if (direction === 'bottom') {
        // Horizontal alignment check
        const horizontalOverlap = Math.min(initiatorRect.right, targetRect.right) -
                                Math.max(initiatorRect.left, targetRect.left);
        const minWidth = Math.min(initiatorRect.width, targetRect.width);
        if (minWidth > 0 && horizontalOverlap < minWidth * MIN_ALIGNMENT) return Infinity;

        // Initiator must be above target
        if (initiatorRect.bottom > targetRect.top) return Infinity;

        return targetRect.top - initiatorRect.bottom;
    }

    if (direction === 'top') {
        // Horizontal alignment check
        const horizontalOverlap = Math.min(initiatorRect.right, targetRect.right) -
                                Math.max(initiatorRect.left, targetRect.left);
        const minWidth = Math.min(initiatorRect.width, targetRect.width);
        if (minWidth > 0 && horizontalOverlap < minWidth * MIN_ALIGNMENT) return Infinity;

        // Initiator must be below target
        if (initiatorRect.top < targetRect.bottom) return Infinity;

        return initiatorRect.top - targetRect.bottom;
    }

    return Infinity;
}

/**
 * Find nearest meldable target for a dragged glyph
 *
 * Checks both directions:
 * 1. Forward: dragged element initiates meld toward nearby targets
 * 2. Reverse: nearby elements initiate meld toward dragged element
 *
 * When reversed, the caller must swap initiator/target in performMeld
 * so edges record the correct from→to direction.
 */
export function findMeldTarget(draggedElement: HTMLElement): {
    target: HTMLElement | null;
    distance: number;
    direction: EdgeDirection;
    reversed: boolean;
} {
    const noMatch = { target: null as HTMLElement | null, distance: Infinity, direction: 'right' as EdgeDirection, reversed: false };

    // Element must participate in melding (as initiator or target)
    if (!canInitiateMeld(draggedElement) && !canReceiveMeld(draggedElement)) {
        return noMatch;
    }

    const canvas = draggedElement.parentElement;
    if (!canvas) {
        return noMatch;
    }

    let closestTarget: HTMLElement | null = null;
    let closestDistance = Infinity;
    let closestDirection: EdgeDirection = 'right';
    let closestReversed = false;

    const draggedRect = draggedElement.getBoundingClientRect();

    // Forward: dragged element initiates toward nearby targets
    if (canInitiateMeld(draggedElement)) {
        for (const targetClass of getTargetClasses()) {
            canvas.querySelectorAll(`.${targetClass}`).forEach(el => {
                const targetElement = el as HTMLElement;
                if (targetElement === draggedElement) return;
                // Skip if already in a meld (Phase 3: allow melding into existing compositions)
                if (targetElement.parentElement?.classList.contains('melded-composition')) return;

                const direction = areCompatible(draggedElement, targetElement);
                if (!direction) return;

                const targetRect = targetElement.getBoundingClientRect();
                const distance = checkDirectionalProximity(draggedRect, targetRect, direction);

                if (distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                    closestDistance = distance;
                    closestTarget = targetElement;
                    closestDirection = direction;
                    closestReversed = false;
                }
            });
        }
    }

    // Reverse: nearby elements initiate toward dragged element
    if (canReceiveMeld(draggedElement)) {
        for (const initiatorClass of getInitiatorClasses()) {
            canvas.querySelectorAll(`.${initiatorClass}`).forEach(el => {
                const nearbyElement = el as HTMLElement;
                if (nearbyElement === draggedElement) return;
                if (nearbyElement.parentElement?.classList.contains('melded-composition')) return;

                // Check if the nearby element can initiate toward the dragged element
                const direction = areCompatible(nearbyElement, draggedElement);
                if (!direction) return;

                // Proximity: nearby is the meld initiator, dragged is the target
                const nearbyRect = nearbyElement.getBoundingClientRect();
                const distance = checkDirectionalProximity(nearbyRect, draggedRect, direction);

                if (distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                    closestDistance = distance;
                    closestTarget = nearbyElement;
                    closestDirection = direction;
                    closestReversed = true;
                }
            });
        }
    }

    return { target: closestTarget, distance: closestDistance, direction: closestDirection, reversed: closestReversed };
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
 * Supports multi-directional melding: right (horizontal) and bottom (vertical).
 */
export function performMeld(
    initiatorElement: HTMLElement,
    targetElement: HTMLElement,
    initiatorGlyph: Glyph,
    targetGlyph: Glyph,
    direction: EdgeDirection = 'right'
): HTMLElement {
    const canvas = initiatorElement.parentElement;
    if (!canvas) {
        throw new Error('Cannot meld: no canvas parent');
    }

    log.info(SEG.GLYPH, '[MeldSystem] Performing meld - reparenting elements', { direction });

    // Generate composition ID
    const compositionId = `melded-${initiatorGlyph.id}-${targetGlyph.id}`;

    // Create edge with the actual direction from proximity detection
    const edges: CompositionEdge[] = [{
        from: initiatorGlyph.id,
        to: targetGlyph.id,
        direction,
        position: 0
    }];

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-glyph-id', compositionId);

    // Position at initiator location
    composition.style.position = 'absolute';
    composition.style.left = initiatorElement.style.left;
    composition.style.top = initiatorElement.style.top;

    // Layout direction based on edge direction
    composition.style.display = 'flex';
    if (direction === 'bottom' || direction === 'top') {
        composition.style.flexDirection = 'column';
        composition.style.alignItems = 'flex-start';
    } else {
        composition.style.flexDirection = 'row';
        composition.style.alignItems = 'center';
    }

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
    edges: CompositionEdge[],
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
        glyphCount: glyphElements.length,
        edgeCount: edges.length
    });

    // Determine layout from edge directions
    const hasVertical = edges.some(e => e.direction === 'bottom' || e.direction === 'top');

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

    if (hasVertical && !edges.some(e => e.direction === 'right')) {
        // Pure vertical layout
        composition.style.flexDirection = 'column';
        composition.style.alignItems = 'flex-start';
    } else {
        // Horizontal (or mixed — for now, use row with wrapping)
        composition.style.flexDirection = 'row';
        composition.style.alignItems = 'center';
    }

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

    // Determine unmeld direction from stored edges
    const firstChildId = glyphElements[0].getAttribute('data-glyph-id') || '';
    const storedComp = findCompositionByGlyph(firstChildId);
    const isVertical = storedComp?.edges.some(e => e.direction === 'bottom' || e.direction === 'top')
        && !storedComp?.edges.some(e => e.direction === 'right');

    // Restore absolute positioning for each glyph, spacing along the original axis
    let currentX = left;
    let currentY = top;
    glyphElements.forEach((element) => {
        element.style.position = 'absolute';
        element.style.left = `${currentX}px`;
        element.style.top = `${currentY}px`;

        // Reparent back to canvas
        canvas.insertBefore(element, composition);

        // Accumulate position for next glyph along the original axis
        const rect = element.getBoundingClientRect();
        if (isVertical) {
            currentY += rect.height + UNMELD_OFFSET;
        } else {
            currentX += rect.width + UNMELD_OFFSET;
        }
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