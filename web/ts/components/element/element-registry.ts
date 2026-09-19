/**
 * Element Type Registry — single source of truth for canvas element types.
 *
 * Maps symbol ↔ className ↔ title ↔ label ↔ factory, eliminating
 * parallel if/else chains in canvas-element.ts.
 *
 * Add a new element type → add one entry here.
 */

import type { Element } from '@teranos/elements';
import { AX, SO, SE, AS, Attestation, Sigma, Type, Triplet, Prose, Subcanvas } from '../../sym';
import { createAxElement } from './ax-element';
import { createSemanticElement } from './semantic-element';
import { createPyElement, PY_DEFAULT_CODE } from './py-element';
import { createPromptElement, PROMPT_DEFAULT_TEMPLATE } from './prompt-element';
import { createNoteElement } from './note-element';
import { createTsElement, TS_DEFAULT_CODE } from './ts-element';
import { createSubcanvasElement } from './subcanvas-element';
import { createAttestationElement } from './attestation-element';
import { createSigmaElement } from './sigma-element';
import { createTypeElement } from './type-element';
import { createTripletElement } from './triplet-element';
import { createResultElement } from './result-element';
import { createThreadElement } from './thread-element';

export interface ElementTypeEntry {
    /** Symbol identifier (e.g., AX, 'py', SO, Prose) */
    symbol: string;
    /** CSS class on the canvas element (e.g., 'canvas-py-element') */
    className: string;
    /** Human-readable name */
    title: string;
    /** Short label for log messages */
    label: string;
    /** Create the DOM element for this element type */
    render: (item: Element) => Promise<HTMLElement> | HTMLElement;
    /** Plugin name for plugin-provided elements (undefined for built-in elements) */
    pluginName?: string;
    /**
     * Element name for an element published as an attestation and served from /g/.
     *
     * Separate from pluginName because it is a different kind of thing: there
     * is no plugin, no process and no am.toml line. Calling one a plugin is
     * what made the canvas tell somebody to enable a plugin that cannot exist.
     */
    publishedName?: string;
    /** Initial content persisted with the element (e.g., default code template) */
    defaultContent?: string;
    /** Position in spawn menu. If undefined, not shown in spawn menu. Lower = earlier. */
    spawnMenuOrder?: number;
    /** Command aliases for search-bar spawning (e.g., 'so' → Prompt, 'prose' → Note) */
    commandAliases?: string[];
}

const ELEMENT_TYPES: ElementTypeEntry[] = [
    { symbol: AX,       className: 'canvas-ax-element',      title: 'AX Query',        label: 'AX',        render: createAxElement,        spawnMenuOrder: 0 },
    { symbol: SE,       className: 'canvas-se-element',      title: 'Semantic Search', label: 'SE',        render: createSemanticElement,  spawnMenuOrder: 1 },
    { symbol: 'py',     className: 'canvas-py-element',      title: 'Python',          label: 'Py',        render: createPyElement,        spawnMenuOrder: 2, defaultContent: PY_DEFAULT_CODE },
    { symbol: 'ts',     className: 'canvas-ts-element',      title: 'TypeScript',      label: 'TS',        render: createTsElement,        spawnMenuOrder: 3, defaultContent: TS_DEFAULT_CODE },
    { symbol: SO,       className: 'canvas-prompt-element',  title: 'Prompt',          label: 'Prompt',    render: createPromptElement,    spawnMenuOrder: 4, defaultContent: PROMPT_DEFAULT_TEMPLATE, commandAliases: ['so'] },
    { symbol: Prose,    className: 'canvas-note-element',    title: 'Note',            label: 'Note',      render: createNoteElement,      spawnMenuOrder: 5, defaultContent: 'Write here — select and click ⟶ to convert to a prompt element.', commandAliases: ['prose'] },
    { symbol: Subcanvas, className: 'canvas-subcanvas-element', title: 'Subcanvas',    label: 'Subcanvas', render: createSubcanvasElement, spawnMenuOrder: 6 },
    { symbol: Attestation, className: 'canvas-attestation-element', title: 'Attestation', label: 'Attestation', render: createAttestationElement },
    { symbol: Triplet,  className: 'canvas-triplet-element',     title: 'Triplet',     label: 'Triplet',   render: createTripletElement },
    { symbol: Sigma,    className: 'canvas-sigma-element',       title: 'Sigma',       label: 'Sigma',     render: createSigmaElement },
    { symbol: Type,     className: 'canvas-type-element',        title: 'Type',        label: 'Type',      render: createTypeElement },
    { symbol: 'stream', className: 'canvas-stream-element',      title: 'Stream',      label: 'Stream',    render: (g) => createResultElement(g) },
    { symbol: '\u303D', className: 'canvas-thread-element',     title: 'Thread',      label: 'Thread',    render: createThreadElement },
];

const _bySymbol = new Map(ELEMENT_TYPES.map(e => [e.symbol, e]));
const _byClassName = new Map(ELEMENT_TYPES.map(e => [e.className, e]));

/** Who provided an element type, or nothing for a built-in. */
function providerOf(entry: ElementTypeEntry): string | undefined {
    if (entry.pluginName !== undefined) return `plugin:${entry.pluginName}`;
    if (entry.publishedName !== undefined) return `published:${entry.publishedName}`;
    return undefined;
}

/**
 * Replace a provided element type already registered under this symbol.
 *
 * An element's module can be replaced while the page is open — republished as an
 * attestation, or rebuilt by a plugin — and the entry registered from the
 * previous one closes over the module it imported.
 *
 * Refuses anything a built-in registered, and refuses one provider taking over
 * another's symbol: a plugin cannot claim a published element's, or the reverse.
 */
export function replacePluginElementType(entry: ElementTypeEntry): boolean {
    const existing = _bySymbol.get(entry.symbol);
    if (!existing) return false;

    const held = providerOf(existing);
    if (held === undefined || held !== providerOf(entry)) return false;

    const at = ELEMENT_TYPES.indexOf(existing);
    if (at !== -1) ELEMENT_TYPES[at] = entry;
    _bySymbol.set(entry.symbol, entry);
    _byClassName.delete(existing.className);
    _byClassName.set(entry.className, entry);
    return true;
}

/** Register a new element type at runtime (for plugin elements) */
export function registerElementType(entry: ElementTypeEntry): void {
    // Check for symbol collision with built-in elements
    if (_bySymbol.has(entry.symbol)) {
        console.warn(`[ElementRegistry] Symbol ${entry.symbol} already registered, skipping`);
        return;
    }

    // Check for className collision
    if (_byClassName.has(entry.className)) {
        console.warn(`[ElementRegistry] Class ${entry.className} already registered, skipping`);
        return;
    }

    // Add to array and Maps
    ELEMENT_TYPES.push(entry);
    _bySymbol.set(entry.symbol, entry);
    _byClassName.set(entry.className, entry);

    console.debug(`[ElementRegistry] Registered element type: ${entry.symbol} (${entry.label})`);
}

/** Get all registered element types */
export function getAllElementTypes(): readonly ElementTypeEntry[] {
    return ELEMENT_TYPES;
}

/** Look up element type by symbol (e.g., AX, 'py', SO) */
export function getElementTypeBySymbol(symbol: string): ElementTypeEntry | undefined {
    return _bySymbol.get(symbol);
}

/**
 * What a canvas saved before the attestation element moved to ⎔ means by "+".
 *
 * A canvas keeps the symbol and nothing else about which element a record is, so
 * every attestation element placed before the move still says "+". Reading the
 * symbol alone would draw those as whatever "+" means now.
 *
 * The content says what the symbol no longer can: an attestation element carries
 * one attestation, so a "+" record whose content parses to an object with a
 * subject, predicate or context is that attestation element and no other.
 */
export function getElementTypeBySavedSymbol(symbol: string, content?: string): ElementTypeEntry | undefined {
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

/** Get element types that appear in the spawn menu, sorted by spawnMenuOrder */
export function getSpawnableElements(): ElementTypeEntry[] {
    return ELEMENT_TYPES
        .filter(e => e.spawnMenuOrder !== undefined)
        .sort((a, b) => a.spawnMenuOrder! - b.spawnMenuOrder!);
}

/** Look up a spawnable element type by command name (label or alias) */
export function getCommandEntry(command: string): ElementTypeEntry | undefined {
    const cmd = command.toLowerCase().trim();
    for (const entry of ELEMENT_TYPES) {
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
    for (const entry of ELEMENT_TYPES) {
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

/** Look up element type by DOM element's class list */
export function getElementTypeByElement(element: HTMLElement): ElementTypeEntry | undefined {
    for (const [className, entry] of _byClassName) {
        if (element.classList.contains(className)) return entry;
    }
    return undefined;
}
