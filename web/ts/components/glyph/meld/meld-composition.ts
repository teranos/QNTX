/**
 * Meld composition — create, extend, reconstruct, and unmeld glyph compositions.
 *
 * CRITICAL: This implementation respects the core glyph axiom:
 * "A Glyph is exactly ONE DOM element for its entire lifetime"
 *
 * NO cloneNode. NO createElement for existing glyphs.
 * Melding is achieved through reparenting, not cloning.
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import type { CompositionEdge } from '../../../state/ui';
import type { EdgeDirection } from './meldability';
import { addComposition, removeComposition, extractGlyphIds, findCompositionByGlyph } from '../../../state/compositions';
import { clearMeldFeedback } from './meld-feedback';

const UNMELD_OFFSET = 20; // px - spacing between glyphs when unmelding

/**
 * Perform meld operation
 * CRITICAL: This reparents the actual DOM elements, does NOT clone them
 *
 * Compositions are persisted to storage and survive page refresh.
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
 * Extend an existing composition by adding a glyph at a leaf (append) or root (prepend)
 *
 * CRITICAL: Reparents the incoming element into the existing composition container.
 * Regenerates composition ID and updates storage.
 *
 * Cross-axis extensions (e.g., bottom result in a row composition) create nested
 * sub-containers to preserve correct spatial layout.
 */
export function extendComposition(
    compositionElement: HTMLElement,
    incomingElement: HTMLElement,
    incomingGlyphId: string,
    anchorGlyphId: string,
    direction: EdgeDirection,
    incomingRole: 'from' | 'to'
): HTMLElement {
    // Look up existing composition state
    const existingComp = findCompositionByGlyph(anchorGlyphId);
    if (!existingComp) {
        throw new Error(`Cannot extend composition: no composition found for glyph ${anchorGlyphId}`);
    }

    const oldId = existingComp.id;

    // Build new edge based on role
    const newEdge: CompositionEdge = incomingRole === 'to'
        ? { from: anchorGlyphId, to: incomingGlyphId, direction, position: existingComp.edges.length }
        : { from: incomingGlyphId, to: anchorGlyphId, direction, position: existingComp.edges.length };

    // Regenerate composition ID from the new edge
    const newId = `melded-${newEdge.from}-${newEdge.to}`;

    log.info(SEG.GLYPH, '[MeldSystem] Extending composition', {
        oldId,
        newId,
        anchor: anchorGlyphId,
        incoming: incomingGlyphId,
        direction,
        incomingRole
    });

    // Clear positioning and meld feedback on incoming element
    incomingElement.style.position = 'relative';
    incomingElement.style.left = '0';
    incomingElement.style.top = '0';
    clearMeldFeedback(incomingElement);

    // Check if this is a cross-axis extension (e.g., adding 'bottom' result to a 'row' composition)
    const compositionFlexDir = compositionElement.style.flexDirection;
    const isCrossAxis =
        (compositionFlexDir === 'row' && (direction === 'bottom' || direction === 'top')) ||
        (compositionFlexDir === 'column' && direction === 'right');

    if (isCrossAxis) {
        // Find the anchor element in the composition
        const anchorElement = compositionElement.querySelector(`[data-glyph-id="${anchorGlyphId}"]`) as HTMLElement;
        if (!anchorElement) {
            throw new Error(`Cannot extend: anchor glyph ${anchorGlyphId} not found in composition`);
        }

        // Check if anchor is already in a sub-container (e.g., second execution result)
        const existingSub = anchorElement.parentElement;
        if (existingSub?.classList.contains('meld-sub-container')) {
            if (incomingRole === 'to') {
                existingSub.appendChild(incomingElement);
            } else {
                existingSub.insertBefore(incomingElement, existingSub.firstChild);
            }
        } else {
            // Create sub-container with cross-axis direction
            const subContainer = document.createElement('div');
            subContainer.className = 'meld-sub-container';
            subContainer.style.display = 'flex';
            if (direction === 'bottom' || direction === 'top') {
                subContainer.style.flexDirection = 'column';
                subContainer.style.alignItems = 'flex-start';
            } else {
                subContainer.style.flexDirection = 'row';
                subContainer.style.alignItems = 'center';
            }

            // Replace anchor with sub-container, then move anchor into it
            compositionElement.insertBefore(subContainer, anchorElement);
            subContainer.appendChild(anchorElement);

            if (incomingRole === 'to') {
                subContainer.appendChild(incomingElement);
            } else {
                subContainer.insertBefore(incomingElement, subContainer.firstChild);
            }
        }
    } else {
        // Same-axis: simple append/prepend
        if (incomingRole === 'to') {
            compositionElement.appendChild(incomingElement);
        } else {
            compositionElement.insertBefore(incomingElement, compositionElement.firstChild);
        }
    }

    // Update composition ID on DOM
    compositionElement.setAttribute('data-glyph-id', newId);

    // Update storage: remove old, add new with all edges
    const allEdges = [...existingComp.edges, newEdge];
    removeComposition(oldId);
    addComposition({
        id: newId,
        edges: allEdges,
        x: existingComp.x,
        y: existingComp.y
    });

    log.info(SEG.GLYPH, '[MeldSystem] Composition extended', {
        newId,
        edges: allEdges.length,
        glyphs: extractGlyphIds(allEdges)
    });

    return compositionElement;
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
        // Horizontal (or mixed)
        composition.style.flexDirection = 'row';
        composition.style.alignItems = 'center';
    }

    // Clear positioning from glyphs
    glyphElements.forEach(element => {
        element.style.position = 'relative';
        element.style.left = '0';
        element.style.top = '0';
    });

    // For mixed-direction compositions, build sub-containers for cross-axis edges
    const isMixedDirection = hasVertical && edges.some(e => e.direction === 'right');

    if (isMixedDirection) {
        // Identify cross-axis children (targets of non-main-axis edges)
        const crossAxisEdges = new Map<string, HTMLElement[]>();
        const crossAxisChildren = new Set<string>();
        for (const edge of edges) {
            if (edge.direction !== 'right') {
                const toEl = glyphElements.find(el => el.getAttribute('data-glyph-id') === edge.to);
                if (toEl) {
                    const existing = crossAxisEdges.get(edge.from) || [];
                    existing.push(toEl);
                    crossAxisEdges.set(edge.from, existing);
                    crossAxisChildren.add(edge.to);
                }
            }
        }

        // Walk glyph elements: create sub-containers for cross-axis anchors
        for (const element of glyphElements) {
            const id = element.getAttribute('data-glyph-id') || '';
            if (crossAxisChildren.has(id)) continue; // Added as part of sub-container

            const children = crossAxisEdges.get(id);
            if (children && children.length > 0) {
                const subContainer = document.createElement('div');
                subContainer.className = 'meld-sub-container';
                subContainer.style.display = 'flex';
                subContainer.style.flexDirection = 'column';
                subContainer.style.alignItems = 'flex-start';

                subContainer.appendChild(element);
                for (const child of children) {
                    subContainer.appendChild(child);
                }
                composition.appendChild(subContainer);
            } else {
                composition.appendChild(element);
            }
        }
    } else {
        // Pure single-direction: simple append
        glyphElements.forEach(element => {
            composition.appendChild(element);
        });
    }

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

    // Find all child glyphs in composition (including those inside sub-containers)
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

    // Remove composition container (sub-containers cleaned up with it)
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
