/**
 * Glyph Morphology — shared helpers for manifestation transitions.
 *
 * Extracted from window.ts, panel.ts, canvas.ts to eliminate duplication
 * in the morph lifecycle (axiom verification, tray targeting, element reset).
 */

import type { Glyph } from '../glyph';
import { setWindowState, setProximityText, hasProximityText } from '../dataset';
import { getLogger, getLogSegment } from '../config';

/**
 * Verify the glyph axiom: exactly one DOM element for this glyph.
 * Calls the tracking verifier, then checks for duplicate data-glyph-id attributes.
 */
export function verifyGlyphAxiom(
    id: string,
    element: HTMLElement,
    verifyElement: (id: string, element: HTMLElement) => void
): void {
    verifyElement(id, element);

    const elements = document.querySelectorAll(`[data-glyph-id="${id}"]`);
    if (elements.length !== 1) {
        throw new Error(
            `AXIOM VIOLATION: Expected exactly 1 element for ${id}, found ${elements.length}`
        );
    }
}

/**
 * Morph-to preamble shared by all manifestations.
 * Verifies axiom, captures current rect, detaches, clears proximity text,
 * reparents to body with fixed positioning, and marks window state.
 */
export function prepareMorphTo(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    morphClass: string,
    zIndex: string
): DOMRect {
    verifyGlyphAxiom(glyph.id, glyphElement, verifyElement);

    const glyphRect = glyphElement.getBoundingClientRect();

    // THE GLYPH ITSELF BECOMES THE MANIFESTATION - NO CLONING
    glyphElement.remove();

    if (hasProximityText(glyphElement)) {
        glyphElement.textContent = '';
        setProximityText(glyphElement, false);
    }

    glyphElement.className = morphClass;
    glyphElement.style.position = 'fixed';
    glyphElement.style.zIndex = zIndex;

    document.body.appendChild(glyphElement);
    setWindowState(glyphElement, true);

    return glyphRect;
}

/**
 * Calculate the target position for minimizing to the glyph tray.
 * If glyphId is provided, targets that dot's position.
 * Otherwise targets the end of the tray (where new dots append).
 */
export function calculateTrayTarget(glyphId?: string): { x: number; y: number } {
    const trayElement = document.querySelector('.glyph-run');
    if (!trayElement) {
        return { x: window.innerWidth - 50, y: window.innerHeight / 2 };
    }

    if (glyphId) {
        const dot = trayElement.querySelector(`[data-glyph-id="${glyphId}"]`);
        if (dot) {
            const dotRect = dot.getBoundingClientRect();
            return {
                x: dotRect.left + dotRect.width / 2,
                y: dotRect.top + dotRect.height / 2,
            };
        }
    }

    const indicators = trayElement.querySelector('.glyph-run-indicators');
    const lastDot = indicators?.lastElementChild;
    if (lastDot) {
        const lastRect = lastDot.getBoundingClientRect();
        return {
            x: lastRect.left + lastRect.width / 2,
            y: lastRect.bottom + 6,
        };
    }

    const trayRect = trayElement.getBoundingClientRect();
    return {
        x: trayRect.right - 20,
        y: trayRect.top + trayRect.height / 2,
    };
}

/**
 * Reset a glyph element to its resting state after minimize animation completes.
 * Clears state flags, removes from DOM, wipes inline styles, restores base class,
 * and hands back to the tray via onMorphComplete.
 */
export function resetGlyphElement(
    element: HTMLElement,
    glyph: Glyph,
    label: string,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    log.debug(seg, `[${label}] Animation complete for ${glyph.id}`);
    setWindowState(element, false);
    setProximityText(element, false);
    element.remove();
    element.style.cssText = '';
    element.className = 'glyph-run-glyph';
    onMorphComplete(element, glyph);
}
