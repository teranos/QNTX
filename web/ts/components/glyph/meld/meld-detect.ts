/**
 * Meld detection — proximity checks and target finding for glyph melding.
 *
 * Determines which glyphs are close enough to meld and in which direction.
 * Bidirectional: checks both forward (dragged→nearby) and reverse (nearby→dragged).
 */

import { getInitiatorClasses, getTargetClasses, areClassesCompatible, getGlyphClass, type EdgeDirection } from './meldability';

// Configuration
export const PROXIMITY_THRESHOLD = 100; // px - distance at which proximity feedback starts
export const MELD_THRESHOLD = 30; // px - distance at which glyphs meld
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
                // Skip elements in the same composition (no internal rearrangement)
                // Uses .closest() to handle elements inside sub-containers
                const targetComp = targetElement.closest('.melded-composition');
                if (targetComp) {
                    if (draggedElement.closest('.melded-composition') === targetComp) return;
                }

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
                const nearbyComp = nearbyElement.closest('.melded-composition');
                if (nearbyComp) {
                    if (draggedElement.closest('.melded-composition') === nearbyComp) return;
                }

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
