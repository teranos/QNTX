/**
 * Glyph Type Registry — single source of truth for canvas glyph types.
 *
 * Maps symbol ↔ className ↔ title ↔ label ↔ factory, eliminating
 * parallel if/else chains in canvas-glyph.ts.
 *
 * Add a new glyph type → add one entry here.
 */

import type { Glyph } from './glyph';
import { AX, IX, SO, Prose, Subcanvas } from '@generated/sym.js';
import { createAxGlyph } from './ax-glyph';
import { createPyGlyph } from './py-glyph';
import { createIxGlyph } from './ix-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';
import { createTsGlyph } from './ts-glyph';
import { createSubcanvasGlyph } from './subcanvas-glyph';

export interface GlyphTypeEntry {
    /** Symbol identifier (e.g., AX, 'py', SO, Prose) */
    symbol: string;
    /** CSS class on the canvas element (e.g., 'canvas-py-glyph') */
    className: string;
    /** Human-readable name */
    title: string;
    /** Short label for log messages */
    label: string;
    /** Create the DOM element for this glyph type */
    render: (glyph: Glyph) => Promise<HTMLElement> | HTMLElement;
}

const GLYPH_TYPES: GlyphTypeEntry[] = [
    { symbol: AX,       className: 'canvas-ax-glyph',      title: 'AX Query', label: 'AX',     render: createAxGlyph },
    { symbol: 'py',     className: 'canvas-py-glyph',      title: 'Python',   label: 'Py',     render: createPyGlyph },
    { symbol: IX,       className: 'canvas-ix-glyph',      title: 'Ingest',   label: 'IX',     render: createIxGlyph },
    { symbol: SO,       className: 'canvas-prompt-glyph',  title: 'Prompt',   label: 'Prompt', render: createPromptGlyph },
    { symbol: 'ts',     className: 'canvas-ts-glyph',      title: 'TypeScript', label: 'TS',   render: createTsGlyph },
    { symbol: Prose,    className: 'canvas-note-glyph',    title: 'Note',     label: 'Note',   render: createNoteGlyph },
    { symbol: Subcanvas, className: 'canvas-subcanvas-glyph', title: 'Subcanvas', label: 'Subcanvas', render: createSubcanvasGlyph },
];

const _bySymbol = new Map(GLYPH_TYPES.map(e => [e.symbol, e]));
const _byClassName = new Map(GLYPH_TYPES.map(e => [e.className, e]));

/** Look up glyph type by symbol (e.g., AX, 'py', SO) */
export function getGlyphTypeBySymbol(symbol: string): GlyphTypeEntry | undefined {
    return _bySymbol.get(symbol);
}

/** Look up glyph type by DOM element's class list */
export function getGlyphTypeByElement(element: HTMLElement): GlyphTypeEntry | undefined {
    for (const [className, entry] of _byClassName) {
        if (element.classList.contains(className)) return entry;
    }
    return undefined;
}
