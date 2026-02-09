/**
 * Meld detection — proximity checks and target finding for glyph melding.
 *
 * Determines which glyphs are close enough to meld and in which direction.
 * Bidirectional: checks both forward (dragged→nearby) and reverse (nearby→dragged).
 */

import { getInitiatorClasses, getTargetClasses, areClassesCompatible, getGlyphClass, getCompositionCompatibility, type EdgeDirection } from './meldability';
import { findCompositionByGlyph } from '../../state/compositions';
import { log, SEG } from '../../logger';

// Configuration
export const PROXIMITY_THRESHOLD = 100; // px - distance at which proximity feedback starts
export const MELD_THRESHOLD = 30; // px - distance at which glyphs meld
const MIN_ALIGNMENT = 0.3; // fraction - minimum overlap required on the alignment axis (30%)

/**
 * Check if element can initiate melding
 */
export function canInitiateMeld(element: HTMLElement): boolean {
    if (element.classList.contains('melded-composition')) return true;
    return getInitiatorClasses().some(cls =>
        element.classList.contains(cls)
    );
}

/**
 * Check if element can receive meld
 */
export function canReceiveMeld(element: HTMLElement): boolean {
    if (element.classList.contains('melded-composition')) return true;
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

    log.debug(SEG.GLYPH, '[findMeldTarget] start', {
        draggedId: draggedElement.getAttribute('data-glyph-id'),
        draggedClasses: draggedElement.className,
        canvasId: canvas.getAttribute('id') || canvas.className?.substring(0, 40),
        canvasIsComp: canvas.classList.contains('melded-composition'),
    });

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

    // Composition-to-composition: dragged composition toward nearby compositions
    const draggedComp = draggedElement.classList.contains('melded-composition')
        ? draggedElement
        : draggedElement.closest('.melded-composition') as HTMLElement | null;

    const allComps = canvas.querySelectorAll('.melded-composition');
    log.debug(SEG.GLYPH, '[findMeldTarget] comp-to-comp search', {
        draggedIsComp: draggedElement.classList.contains('melded-composition'),
        draggedComp: draggedComp?.getAttribute('data-glyph-id'),
        compsOnCanvas: allComps.length,
    });

    allComps.forEach(el => {
        const compElement = el as HTMLElement;
        if (compElement === draggedElement) return;
        if (compElement === draggedComp) return;

        // Get edges for both compositions
        const compFirstChild = compElement.querySelector('[data-glyph-id]');
        const compChildId = compFirstChild?.getAttribute('data-glyph-id') || '';
        const compState = findCompositionByGlyph(compChildId);

        log.debug(SEG.GLYPH, '[findMeldTarget] examining nearby comp', {
            compId: compElement.getAttribute('data-glyph-id'),
            compChildId,
            compStateFound: !!compState,
        });

        if (!compState) return;

        // Forward: dragged element's leaves → nearby composition's roots
        if (draggedComp) {
            const draggedFirstChild = draggedComp.querySelector('[data-glyph-id]');
            const draggedChildId = draggedFirstChild?.getAttribute('data-glyph-id') || '';
            const draggedState = findCompositionByGlyph(draggedChildId);
            if (draggedState) {
                const direction = getCompositionCompatibility(
                    draggedComp, compElement,
                    draggedState.edges, compState.edges
                );
                const compRect = compElement.getBoundingClientRect();
                const distance = direction ? checkDirectionalProximity(draggedRect, compRect, direction) : Infinity;

                log.debug(SEG.GLYPH, '[findMeldTarget] comp-to-comp forward', {
                    draggedChildId,
                    direction,
                    distance: distance === Infinity ? 'Inf' : distance,
                    draggedRight: draggedRect.right,
                    compLeft: compRect.left,
                });

                if (direction && distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                    closestDistance = distance;
                    closestTarget = compElement;
                    closestDirection = direction;
                    closestReversed = false;
                }
            }
        }

        // Reverse: nearby composition's leaves → dragged element's roots
        if (draggedComp) {
            const draggedFirstChild = draggedComp.querySelector('[data-glyph-id]');
            const draggedChildId = draggedFirstChild?.getAttribute('data-glyph-id') || '';
            const draggedState = findCompositionByGlyph(draggedChildId);
            if (draggedState) {
                const direction = getCompositionCompatibility(
                    compElement, draggedComp,
                    compState.edges, draggedState.edges
                );
                const compRect = compElement.getBoundingClientRect();
                const distance = direction ? checkDirectionalProximity(compRect, draggedRect, direction) : Infinity;

                log.debug(SEG.GLYPH, '[findMeldTarget] comp-to-comp reverse', {
                    direction,
                    distance: distance === Infinity ? 'Inf' : distance,
                    compRight: compRect.right,
                    draggedLeft: draggedRect.left,
                });

                if (direction && distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                    closestDistance = distance;
                    closestTarget = compElement;
                    closestDirection = direction;
                    closestReversed = true;
                }
            }
        } else {
            // Dragged is a standalone glyph — check against nearby composition
            // Forward: standalone glyph → composition roots
            const glyphClass = getGlyphClass(draggedElement);
            if (glyphClass) {
                const direction = getCompositionCompatibility(
                    draggedElement, compElement,
                    undefined, compState.edges
                );
                if (direction) {
                    const compRect = compElement.getBoundingClientRect();
                    const distance = checkDirectionalProximity(draggedRect, compRect, direction);
                    if (distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                        closestDistance = distance;
                        closestTarget = compElement;
                        closestDirection = direction;
                        closestReversed = false;
                    }
                }
            }

            // Reverse: composition leaves → standalone glyph
            if (glyphClass) {
                const direction = getCompositionCompatibility(
                    compElement, draggedElement,
                    compState.edges, undefined
                );
                if (direction) {
                    const compRect = compElement.getBoundingClientRect();
                    const distance = checkDirectionalProximity(compRect, draggedRect, direction);
                    if (distance < PROXIMITY_THRESHOLD && distance < closestDistance) {
                        closestDistance = distance;
                        closestTarget = compElement;
                        closestDirection = direction;
                        closestReversed = true;
                    }
                }
            }
        }
    });

    // Composition dragged near standalone glyphs
    if (draggedComp) {
        const draggedFirstChild = draggedComp.querySelector('[data-glyph-id]');
        const draggedChildId = draggedFirstChild?.getAttribute('data-glyph-id') || '';
        const draggedState = findCompositionByGlyph(draggedChildId);

        if (draggedState) {
            // Forward: composition leaves → standalone targets
            for (const targetClass of getTargetClasses()) {
                canvas.querySelectorAll(`.${targetClass}`).forEach(el => {
                    const targetElement = el as HTMLElement;
                    if (targetElement.closest('.melded-composition')) return; // skip glyphs in compositions

                    const direction = getCompositionCompatibility(
                        draggedComp, targetElement,
                        draggedState.edges, undefined
                    );
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

            // Reverse: standalone initiators → composition roots
            for (const initiatorClass of getInitiatorClasses()) {
                canvas.querySelectorAll(`.${initiatorClass}`).forEach(el => {
                    const nearbyElement = el as HTMLElement;
                    if (nearbyElement.closest('.melded-composition')) return;

                    const direction = getCompositionCompatibility(
                        nearbyElement, draggedComp,
                        undefined, draggedState.edges
                    );
                    if (!direction) return;

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
    }

    if (closestTarget) {
        log.debug(SEG.GLYPH, '[findMeldTarget] result', {
            targetId: closestTarget.getAttribute('data-glyph-id'),
            distance: closestDistance,
            direction: closestDirection,
            reversed: closestReversed,
        });
    }

    return { target: closestTarget, distance: closestDistance, direction: closestDirection, reversed: closestReversed };
}
