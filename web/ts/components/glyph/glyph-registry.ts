/**
 * Glyph Type Registry — single source of truth for canvas glyph types.
 *
 * Maps symbol ↔ className ↔ title ↔ label ↔ factory, eliminating
 * parallel if/else chains in canvas-glyph.ts.
 *
 * Add a new glyph type → add one entry here.
 */

import type { Glyph } from '@qntx/glyphs';
import { AX, SO, SE, AS, Attestation, Sigma, Type, Triplet, Prose, Subcanvas } from '../../sym';
import { createAxGlyph } from './ax-glyph';
import { createSemanticGlyph } from './semantic-glyph';
import { createPyGlyph, PY_DEFAULT_CODE } from './py-glyph';
import { createPromptGlyph, PROMPT_DEFAULT_TEMPLATE } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';
import { createTsGlyph, TS_DEFAULT_CODE } from './ts-glyph';
import { createSubcanvasGlyph } from './subcanvas-glyph';
import { createAttestationGlyph } from './attestation-glyph';
import { createSigmaGlyph } from './sigma-glyph';
import { createTypeGlyph } from './type-glyph';
import { createTripletGlyph } from './triplet-glyph';
import { createResultGlyph } from './result-glyph';
import { createThreadGlyph } from './thread-glyph';

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
    /**
     * Glyph name for a glyph published as an attestation and served from /g/.
     *
     * Separate from pluginName because it is a different kind of thing: there
     * is no plugin, no process and no am.toml line. Calling one a plugin is
     * what made the canvas tell somebody to enable a plugin that cannot exist.
     */
    publishedName?: string;
    /** Initial content persisted with the glyph (e.g., default code template) */
    defaultContent?: string;
    /** Position in spawn menu. If undefined, not shown in spawn menu. Lower = earlier. */
    spawnMenuOrder?: number;
    /** Command aliases for search-bar spawning (e.g., 'so' → Prompt, 'prose' → Note) */
    commandAliases?: string[];
}

const GLYPH_TYPES: GlyphTypeEntry[] = [
    { symbol: AX,       className: 'canvas-ax-glyph',      title: 'AX Query',        label: 'AX',        render: createAxGlyph,        spawnMenuOrder: 0 },
    { symbol: SE,       className: 'canvas-se-glyph',      title: 'Semantic Search', label: 'SE',        render: createSemanticGlyph,  spawnMenuOrder: 1 },
    { symbol: 'py',     className: 'canvas-py-glyph',      title: 'Python',          label: 'Py',        render: createPyGlyph,        spawnMenuOrder: 2, defaultContent: PY_DEFAULT_CODE },
    { symbol: 'ts',     className: 'canvas-ts-glyph',      title: 'TypeScript',      label: 'TS',        render: createTsGlyph,        spawnMenuOrder: 3, defaultContent: TS_DEFAULT_CODE },
    { symbol: SO,       className: 'canvas-prompt-glyph',  title: 'Prompt',          label: 'Prompt',    render: createPromptGlyph,    spawnMenuOrder: 4, defaultContent: PROMPT_DEFAULT_TEMPLATE, commandAliases: ['so'] },
    { symbol: Prose,    className: 'canvas-note-glyph',    title: 'Note',            label: 'Note',      render: createNoteGlyph,      spawnMenuOrder: 5, defaultContent: 'Write here — select and click ⟶ to convert to a prompt glyph.', commandAliases: ['prose'] },
    { symbol: Subcanvas, className: 'canvas-subcanvas-glyph', title: 'Subcanvas',    label: 'Subcanvas', render: createSubcanvasGlyph, spawnMenuOrder: 6 },
    { symbol: Attestation, className: 'canvas-attestation-glyph', title: 'Attestation', label: 'Attestation', render: createAttestationGlyph },
    { symbol: Triplet,  className: 'canvas-triplet-glyph',     title: 'Triplet',     label: 'Triplet',   render: createTripletGlyph },
    { symbol: Sigma,    className: 'canvas-sigma-glyph',       title: 'Sigma',       label: 'Sigma',     render: createSigmaGlyph },
    { symbol: Type,     className: 'canvas-type-glyph',        title: 'Type',        label: 'Type',      render: createTypeGlyph },
    { symbol: 'stream', className: 'canvas-stream-glyph',      title: 'Stream',      label: 'Stream',    render: (g) => createResultGlyph(g) },
    { symbol: '\u303D', className: 'canvas-thread-glyph',     title: 'Thread',      label: 'Thread',    render: createThreadGlyph },
];

const _bySymbol = new Map(GLYPH_TYPES.map(e => [e.symbol, e]));
const _byClassName = new Map(GLYPH_TYPES.map(e => [e.className, e]));

/** Who provided a glyph type, or nothing for a built-in. */
function providerOf(entry: GlyphTypeEntry): string | undefined {
    if (entry.pluginName !== undefined) return `plugin:${entry.pluginName}`;
    if (entry.publishedName !== undefined) return `published:${entry.publishedName}`;
    return undefined;
}

/**
 * Replace a provided glyph type already registered under this symbol.
 *
 * A glyph's module can be replaced while the page is open — republished as an
 * attestation, or rebuilt by a plugin — and the entry registered from the
 * previous one closes over the module it imported.
 *
 * Refuses anything a built-in registered, and refuses one provider taking over
 * another's symbol: a plugin cannot claim a published glyph's, or the reverse.
 */
export function replacePluginGlyphType(entry: GlyphTypeEntry): boolean {
    const existing = _bySymbol.get(entry.symbol);
    if (!existing) return false;

    const held = providerOf(existing);
    if (held === undefined || held !== providerOf(entry)) return false;

    const at = GLYPH_TYPES.indexOf(existing);
    if (at !== -1) GLYPH_TYPES[at] = entry;
    _bySymbol.set(entry.symbol, entry);
    _byClassName.delete(existing.className);
    _byClassName.set(entry.className, entry);
    return true;
}

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

/**
 * What a canvas saved before the attestation glyph moved to ⎔ means by "+".
 *
 * A canvas keeps the symbol and nothing else about which glyph a record is, so
 * every attestation glyph placed before the move still says "+". Reading the
 * symbol alone would draw those as whatever "+" means now.
 *
 * The content says what the symbol no longer can: an attestation glyph carries
 * one attestation, so a "+" record whose content parses to an object with a
 * subject, predicate or context is that attestation glyph and no other.
 */
export function getGlyphTypeBySavedSymbol(symbol: string, content?: string): GlyphTypeEntry | undefined {
    if (symbol === AS && holdsAnAttestation(content)) {
        return _bySymbol.get(Attestation);
    }
    return _bySymbol.get(symbol);
}

/** Whether saved content is one attestation rather than a bare value. */
function holdsAnAttestation(content?: string): boolean {
    if (!content) return false;
    try {
        const parsed: unknown = JSON.parse(content);
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return false;
        const as = parsed as Record<string, unknown>;
        return 'subjects' in as || 'predicates' in as || 'contexts' in as;
    } catch (notAnAttestation) {
        // A record whose content is not JSON is not one of these, which is the
        // whole question. The content itself stays untouched either way.
        return false;
    }
}

/** Get glyph types that appear in the spawn menu, sorted by spawnMenuOrder */
export function getSpawnableGlyphs(): GlyphTypeEntry[] {
    return GLYPH_TYPES
        .filter(e => e.spawnMenuOrder !== undefined)
        .sort((a, b) => a.spawnMenuOrder! - b.spawnMenuOrder!);
}

/** Look up a spawnable glyph type by command name (label or alias) */
export function getCommandEntry(command: string): GlyphTypeEntry | undefined {
    const cmd = command.toLowerCase().trim();
    for (const entry of GLYPH_TYPES) {
        if (entry.spawnMenuOrder === undefined) continue;
        if (entry.label.toLowerCase() === cmd) return entry;
        if (entry.commandAliases?.includes(cmd)) return entry;
    }
    return undefined;
}

/** Return command names that prefix-match the query */
export function getMatchingCommandNames(query: string): string[] {
    if (!query) return [];
    const q = query.toLowerCase();
    const names: string[] = [];
    for (const entry of GLYPH_TYPES) {
        if (entry.spawnMenuOrder === undefined) continue;
        const label = entry.label.toLowerCase();
        if (label.startsWith(q)) names.push(label);
        for (const alias of entry.commandAliases ?? []) {
            if (alias.startsWith(q)) names.push(alias);
        }
    }
    return names;
}

/** Human-readable label for a command name */
export function getCommandLabel(command: string): string {
    const entry = getCommandEntry(command);
    if (!entry) return command;
    return `${entry.label} — ${entry.title}`;
}

/** Look up glyph type by DOM element's class list */
export function getGlyphTypeByElement(element: HTMLElement): GlyphTypeEntry | undefined {
    for (const [className, entry] of _byClassName) {
        if (element.classList.contains(className)) return entry;
    }
    return undefined;
}
