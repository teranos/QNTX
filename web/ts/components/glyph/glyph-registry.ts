/**
 * Glyph Type Registry — single source of truth for canvas glyph types.
 *
 * Maps symbol ↔ className ↔ title ↔ label ↔ factory, eliminating
 * parallel if/else chains in canvas-glyph.ts.
 *
 * Add a new glyph type → add one entry here.
 */

import type { Glyph } from './glyph';
import { AX, IX, SO, SE, AS, Prose, Doc, Subcanvas } from '@generated/sym.js';
import { createAxGlyph } from './ax-glyph';
import { createSemanticGlyph } from './semantic-glyph';
import { createPyGlyph } from './py-glyph';
import { createIxGlyph } from './ix-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';
import { createTsGlyph } from './ts-glyph';
import { createDocGlyph } from './doc-glyph';
import { createSubcanvasGlyph } from './subcanvas-glyph';
import { createAttestationGlyph } from './attestation-glyph';

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
    /** Plugin name for plugin-provided glyphs (undefined for built-in glyphs) */
    pluginName?: string;
}

const GLYPH_TYPES: GlyphTypeEntry[] = [
    { symbol: AX,       className: 'canvas-ax-glyph',      title: 'AX Query',         label: 'AX',     render: createAxGlyph },
    { symbol: SE,       className: 'canvas-se-glyph',      title: 'Semantic Search',  label: 'SE',     render: createSemanticGlyph },
    { symbol: 'py',     className: 'canvas-py-glyph',      title: 'Python',   label: 'Py',     render: createPyGlyph },
    { symbol: IX,       className: 'canvas-ix-glyph',      title: 'Ingest',   label: 'IX',     render: createIxGlyph },
    { symbol: SO,       className: 'canvas-prompt-glyph',  title: 'Prompt',   label: 'Prompt', render: createPromptGlyph },
    { symbol: 'ts',     className: 'canvas-ts-glyph',      title: 'TypeScript', label: 'TS',   render: createTsGlyph },
    { symbol: Prose,    className: 'canvas-note-glyph',    title: 'Note',     label: 'Note',   render: createNoteGlyph },
    { symbol: Doc,      className: 'canvas-doc-glyph',     title: 'Document', label: 'Doc',    render: createDocGlyph },
    { symbol: Subcanvas, className: 'canvas-subcanvas-glyph', title: 'Subcanvas', label: 'Subcanvas', render: createSubcanvasGlyph },
    { symbol: AS,        className: 'canvas-attestation-glyph', title: 'Attestation', label: 'AS', render: createAttestationGlyph },
];

const _bySymbol = new Map(GLYPH_TYPES.map(e => [e.symbol, e]));
const _byClassName = new Map(GLYPH_TYPES.map(e => [e.className, e]));

/** Register a new glyph type at runtime (for plugin glyphs) */
export function registerGlyphType(entry: GlyphTypeEntry): void {
    // Check for symbol collision with built-in glyphs
    if (_bySymbol.has(entry.symbol)) {
        console.warn(`[GlyphRegistry] Symbol ${entry.symbol} already registered, skipping`);
        return;
    }

    // Check for className collision
    if (_byClassName.has(entry.className)) {
        console.warn(`[GlyphRegistry] Class ${entry.className} already registered, skipping`);
        return;
    }

    // Add to array and Maps
    GLYPH_TYPES.push(entry);
    _bySymbol.set(entry.symbol, entry);
    _byClassName.set(entry.className, entry);

    console.debug(`[GlyphRegistry] Registered glyph type: ${entry.symbol} (${entry.label})`);
}

/** Get all registered glyph types */
export function getAllGlyphTypes(): readonly GlyphTypeEntry[] {
    return GLYPH_TYPES;
}

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
