/**
 * Meld composition — create, extend, reconstruct, and unmeld glyph compositions.
 *
 * CRITICAL: This implementation respects the core glyph axiom:
 * "A Glyph is exactly ONE DOM element for its entire lifetime"
 *
 * NO cloneNode. NO createElement for existing glyphs.
 * Melding is achieved through reparenting, not cloning.
 *
 * Layout: CSS Grid with per-glyph grid-row/grid-column placement
 * derived from the edge DAG via computeGridPositions().
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import type { CompositionEdge } from '../../../state/ui';
import type { EdgeDirection } from './meldability';
import { computeGridPositions } from './meldability';
import { addComposition, removeComposition, extractGlyphIds, findCompositionByGlyph } from '../../../state/compositions';
import { clearMeldFeedback } from './meld-feedback';

const UNMELD_OFFSET = 20; // px - spacing between glyphs when unmelding

/**
 * Apply CSS Grid layout to a composition based on its edge graph.
 * Single source of truth — used by performMeld, extendComposition, and reconstructMeld.
 */
function applyGridLayout(
    composition: HTMLElement,
    elements: HTMLElement[],
    edges: CompositionEdge[]
): void {
    composition.style.display = 'grid';
    composition.style.gridAutoRows = 'auto';
    composition.style.gridAutoColumns = 'auto';
    composition.style.gap = '0';

    const positions = computeGridPositions(edges);

    for (const el of elements) {
        const id = el.getAttribute('data-glyph-id') || '';
        const pos = positions.get(id);
        if (!pos) {
            log.warn(SEG.GLYPH, `[MeldSystem] No grid position computed for glyph ${id}`);
            continue;
        }
        el.style.gridRow = String(pos.row);
        el.style.gridColumn = String(pos.col);
    }
}

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
        throw new Error(`Cannot meld: no canvas parent for initiator ${initiatorGlyph.id}`);
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

    // Apply grid layout from edge graph
    applyGridLayout(composition, [initiatorElement, targetElement], edges);

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

    // Append incoming as direct child — grid handles placement
    compositionElement.appendChild(incomingElement);

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

    // Rebuild grid positions for all children
    const allChildren = Array.from(
        compositionElement.querySelectorAll('[data-glyph-id]')
    ) as HTMLElement[];
    applyGridLayout(compositionElement, allChildren, allEdges);

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
        throw new Error(`Cannot reconstruct meld: no glyph elements provided for composition ${compositionId}`);
    }

    const canvas = glyphElements[0].parentElement;
    if (!canvas) {
        throw new Error(`Cannot reconstruct meld: no canvas parent for composition ${compositionId}`);
    }

    log.info(SEG.GLYPH, '[MeldSystem] Reconstructing meld from storage', {
        glyphCount: glyphElements.length,
        edgeCount: edges.length
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

    // Clear positioning from glyphs and reparent
    glyphElements.forEach(element => {
        element.style.position = 'relative';
        element.style.left = '0';
        element.style.top = '0';
        composition.appendChild(element);
    });

    // Apply grid layout from edge graph
    applyGridLayout(composition, glyphElements, edges);

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

    // TODO(#448): binary heuristic — mixed-direction compositions spread into a flat row
    const firstChildId = glyphElements[0].getAttribute('data-glyph-id') || '';
    const storedComp = findCompositionByGlyph(firstChildId);
    const isVertical = storedComp?.edges.some(e => e.direction === 'bottom' || e.direction === 'top')
        && !storedComp?.edges.some(e => e.direction === 'right');

    // TODO(#450): animate the separation instead of instant repositioning
    let currentX = left;
    let currentY = top;
    glyphElements.forEach((element) => {
        element.style.position = 'absolute';
        element.style.left = `${currentX}px`;
        element.style.top = `${currentY}px`;
        element.style.gridRow = '';
        element.style.gridColumn = '';

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
