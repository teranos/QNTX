/**
 * Placement specimen — a tray with twenty items in it.
 *
 * Open them by hand, one at a time, and watch where each lands. Nothing here
 * passes a position, so every open goes through morphToWindow → findPlacement.
 */

import { glyphRun } from '../run';
import type { Glyph } from '../glyph';

const WINDOW_WIDTH = 260;

// From docs/SYMBOLS.md — the SEG symbols, then the derived and structural
// ones. Twenty distinct marks, so a window is identifiable at a glance.
const SYMBOLS = [
    '⍟', '≡', '⨳', '⋈', '⌬', '✦', '⟶', '⊨', '+', '=',
    '∈', '꩜', '⊔', '▣', '⏿', '◈', '⊕', '⊗', '⌁', '⊙',
];
const TRAY_SIZE = SYMBOLS.length;

function specimenGlyph(index: number): Glyph {
    return {
        id: `placement-${index}`,
        title: `glyph ${index}`,
        symbol: SYMBOLS[index - 1],
        manifestationType: 'window',
        initialWidth: `${WINDOW_WIDTH}px`,
        // No initialHeight — the engine measures the content and commits
        // fit-content, the way sbvh.nl's windows do.
        color: '#000',
        textColor: '#fff',
        renderContent: () => {
            const el = document.createElement('div');
            el.className = 'glyph-content';
            el.style.maxWidth = `${WINDOW_WIDTH - 2 - 16}px`;
            el.textContent = `glyph ${index}`;
            return el;
        },
    } as Glyph;
}

/** Fills the tray. Opening is done by hand from the tray. */
export function renderPlacementSpecimen(): void {
    for (let i = 1; i <= TRAY_SIZE; i++) {
        glyphRun.add(specimenGlyph(i));
    }
}
