/**
 * Meld System - Proximity-based glyph melding
 *
 * Provides functions for detecting proximity, applying magnetic attraction,
 * and melding glyphs together when they get close enough.
 *
 * Based on the vision in docs/vision/glyph-melding.md
 */

import { log, SEG } from '../../logger';
import { AX, SO } from '@generated/sym.js';

// Configuration constants
const PROXIMITY_THRESHOLD = 150; // px - distance at which attraction starts
const MELD_THRESHOLD = 40; // px - distance at which glyphs meld
const MAGNETIC_STRENGTH = 0.3; // Strength of magnetic force (0-1)

// Track melding state
interface MeldState {
    target: HTMLElement | null;
    distance: number;
    isMeldable: boolean;
}

/**
 * Find the nearest meldable target for an element
 */
export function findMeldTarget(
    element: HTMLElement,
    mouseX: number,
    mouseY: number
): MeldState {
    // Check if this is an ax-glyph (only they can initiate melding)
    if (!element.classList.contains('ax-glyph')) {
        return { target: null, distance: Infinity, isMeldable: false };
    }

    // Find all prompt glyphs
    const container = element.parentElement;
    if (!container) {
        return { target: null, distance: Infinity, isMeldable: false };
    }

    const promptGlyphs = container.querySelectorAll('.prompt-glyph');
    let closestTarget: HTMLElement | null = null;
    let closestDistance = Infinity;

    promptGlyphs.forEach((target) => {
        if (target === element) return; // Skip self

        const targetEl = target as HTMLElement;
        const targetRect = targetEl.getBoundingClientRect();
        const elementRect = element.getBoundingClientRect();

        // Check vertical alignment (must be roughly on same horizontal plane)
        const verticalOverlap = Math.min(elementRect.bottom, targetRect.bottom) -
                              Math.max(elementRect.top, targetRect.top);
        const minHeight = Math.min(elementRect.height, targetRect.height);

        if (verticalOverlap < minHeight * 0.3) {
            return; // Not aligned enough vertically
        }

        // Check if ax is to the left of prompt (correct orientation)
        if (elementRect.right > targetRect.left) {
            return; // Wrong orientation or overlapping
        }

        // Calculate distance between right edge of ax and left edge of prompt
        const distance = targetRect.left - elementRect.right;

        if (distance < closestDistance && distance < PROXIMITY_THRESHOLD) {
            closestDistance = distance;
            closestTarget = targetEl;
        }
    });

    return {
        target: closestTarget,
        distance: closestDistance,
        isMeldable: closestDistance < MELD_THRESHOLD
    };
}

/**
 * Calculate magnetic offset for attraction
 */
export function calculateMagneticOffset(distance: number): number {
    if (distance >= PROXIMITY_THRESHOLD) return 0;

    // Inverse square law for realistic magnetic feel
    const normalizedDistance = distance / PROXIMITY_THRESHOLD;
    const force = (1 - normalizedDistance) * MAGNETIC_STRENGTH;

    // Return offset in pixels (pulls toward target)
    return force * (PROXIMITY_THRESHOLD - distance) * 0.5;
}

/**
 * Apply visual feedback based on proximity
 */
export function applyMeldFeedback(
    element: HTMLElement,
    target: HTMLElement | null,
    distance: number
): void {
    // Clear any existing feedback
    clearMeldFeedback(element);

    if (!target || distance >= PROXIMITY_THRESHOLD) {
        return;
    }

    const intensity = 1 - (distance / PROXIMITY_THRESHOLD);

    // Calculate glow color based on proximity (heat-based)
    let glowColor: string;
    let glowSize: number;

    if (distance < MELD_THRESHOLD) {
        glowColor = 'rgba(255, 69, 0, 0.8)'; // Red-orange when meldable
        glowSize = 30;
        element.classList.add('meld-ready');
        target.classList.add('meld-target-ready');
    } else if (distance < PROXIMITY_THRESHOLD * 0.5) {
        glowColor = 'rgba(255, 140, 0, 0.6)'; // Orange when close
        glowSize = 20;
        element.classList.add('meld-approaching');
        target.classList.add('meld-target-approaching');
    } else {
        glowColor = 'rgba(255, 255, 0, 0.3)'; // Faint yellow when distant
        glowSize = 10;
    }

    // Apply glow to edges (right edge of ax, left edge of prompt)
    element.style.boxShadow = `${intensity * glowSize}px 0 ${intensity * glowSize}px ${glowColor}`;
    target.style.boxShadow = `-${intensity * glowSize}px 0 ${intensity * glowSize}px ${glowColor}`;

    // Morph borders for connection preview
    const borderRadius = `${intensity * 10}px`;
    element.style.borderTopRightRadius = borderRadius;
    element.style.borderBottomRightRadius = borderRadius;
    target.style.borderTopLeftRadius = borderRadius;
    target.style.borderBottomLeftRadius = borderRadius;
}

/**
 * Clear visual feedback
 */
export function clearMeldFeedback(element: HTMLElement): void {
    // Clear classes
    element.classList.remove('meld-ready', 'meld-approaching');

    // Clear styles
    element.style.boxShadow = '';
    element.style.borderTopRightRadius = '';
    element.style.borderBottomRightRadius = '';

    // Clear from all potential targets
    const container = element.parentElement;
    if (container) {
        container.querySelectorAll('.meld-target-ready, .meld-target-approaching').forEach(target => {
            target.classList.remove('meld-target-ready', 'meld-target-approaching');
            (target as HTMLElement).style.boxShadow = '';
            (target as HTMLElement).style.borderTopLeftRadius = '';
            (target as HTMLElement).style.borderBottomLeftRadius = '';
        });
    }
}

/**
 * Perform the meld operation
 */
export function performMeld(
    axElement: HTMLElement,
    promptElement: HTMLElement
): HTMLElement | null {
    log.info(SEG.UI, '[MeldSystem] Performing meld between ax and prompt glyphs');

    const container = axElement.parentElement;
    if (!container) {
        log.error(SEG.UI, '[MeldSystem] No parent container for melding');
        return null;
    }

    // Create melded composition container
    const meldedContainer = document.createElement('div');
    meldedContainer.className = 'melded-glyph-composition';
    meldedContainer.setAttribute('data-melded', 'true');

    // Position at ax location
    meldedContainer.style.position = 'absolute';
    meldedContainer.style.left = axElement.style.left;
    meldedContainer.style.top = axElement.style.top;

    // Clone the glyphs into the composition (preserving functionality)
    const axClone = axElement.cloneNode(true) as HTMLElement;
    const promptClone = promptElement.cloneNode(true) as HTMLElement;

    // Reset positioning for clones (they're now relative to composition)
    axClone.style.position = 'relative';
    axClone.style.left = '0';
    axClone.style.top = '0';
    promptClone.style.position = 'relative';
    promptClone.style.left = '0';
    promptClone.style.top = '0';

    // Clear any meld styles
    axClone.style.boxShadow = '';
    axClone.style.borderRadius = '';
    promptClone.style.boxShadow = '';
    promptClone.style.borderRadius = '';

    // Add to composition
    meldedContainer.appendChild(axClone);
    meldedContainer.appendChild(promptClone);

    // Add to container and remove originals
    container.appendChild(meldedContainer);
    axElement.remove();
    promptElement.remove();

    // Dispatch meld event
    const meldEvent = new CustomEvent('glyph:melded', {
        detail: {
            source: axClone,
            target: promptClone,
            composition: meldedContainer
        }
    });
    document.dispatchEvent(meldEvent);

    log.info(SEG.UI, '[MeldSystem] Meld completed successfully');
    return meldedContainer;
}

/**
 * Check if an element is part of a melded composition
 */
export function isMeldedComposition(element: HTMLElement): boolean {
    return element.classList.contains('melded-glyph-composition') ||
           element.hasAttribute('data-melded');
}

/**
 * Unmeld a composition back into individual glyphs
 */
export function unmeldComposition(composition: HTMLElement): void {
    if (!isMeldedComposition(composition)) {
        log.warn(SEG.UI, '[MeldSystem] Attempted to unmeld non-melded element');
        return;
    }

    const container = composition.parentElement;
    if (!container) return;

    // Extract individual glyphs
    const glyphs = composition.querySelectorAll('.ax-glyph, .prompt-glyph');

    glyphs.forEach((glyph, index) => {
        const glyphEl = glyph as HTMLElement;
        // Restore absolute positioning
        glyphEl.style.position = 'absolute';

        // Calculate position based on composition position
        const compLeft = parseInt(composition.style.left || '0');
        const compTop = parseInt(composition.style.top || '0');

        // Offset each glyph slightly so they don't overlap
        glyphEl.style.left = `${compLeft + (index * 50)}px`;
        glyphEl.style.top = `${compTop}px`;

        // Re-insert as standalone element
        container.insertBefore(glyphEl, composition);
    });

    // Remove the composition container
    composition.remove();

    log.info(SEG.UI, '[MeldSystem] Composition unmelded successfully');
}