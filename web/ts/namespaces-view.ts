import { escapeHtml } from './html-utils';

// What a namespace's ns.toml says (ADR-026).
export interface NamespaceDefinition {
    owner: string;
    enabled: boolean;
    created_at: string;
}

export interface Namespace {
    name: string;
    definition: NamespaceDefinition | null;
    kinds: string[];
}

// system is the node and default is the default project. Everything else is a
// project SUPER made (ADR-026).
export function kindOf(name: string): 'system' | 'default' | 'project' {
    if (name === 'system') return 'system';
    if (name === 'default') return 'default';
    return 'project';
}

// What a namespace is, for the hover — what its ns.toml says and what it
// holds. Neither belongs on the face: the face is the name, and nothing else.
function describe(ns: Namespace): string {
    const def = ns.definition;
    const own = def
        ? `${def.enabled ? 'enabled' : 'disabled'}, owned by ${def.owner}, since ${def.created_at}`
        : 'no ns.toml defines this';
    const held = ns.kinds.length > 0 ? ns.kinds.join(', ') : 'nothing yet';
    return `${own} — holds ${held}`;
}

function tile(ns: Namespace, selected: string): string {
    const name = escapeHtml(ns.name);
    const chosen = ns.name === selected ? ' selected' : '';
    return `<div class="namespace-tile${chosen}" data-kind="${kindOf(ns.name)}" data-name="${name}"` +
        ` title="${escapeHtml(describe(ns))}">${name}</div>`;
}

// The + becomes the rectangle you type into, so there is one shape in the row
// and adding is not a second place to look.
function addTile(adding: boolean): string {
    if (!adding) return `<div class="namespace-tile namespace-add" data-action="add">+</div>`;
    return `<input class="namespace-tile namespace-new" id="namespace-new" autocomplete="off" spellcheck="false">`;
}

// The way back out, left of everything it stands beside. Who you are comes
// before which namespace you are looking at, so it sits before them.
// TODO: a User glyph sits beside this latch — settled visual identity, keys,
// devices, logout, add-this-device. The door keeps its way out; the glyph is
// the second one, and the place the past is opened from.
function latchTile(): string {
    return `<div class="door-latch" data-action="door" title="Who you are">&lt;</div>`;
}

export function tilesHtml(namespaces: Namespace[], selected: string, adding: boolean): string {
    const tiles = namespaces.map(ns => tile(ns, selected)).join('') + addTile(adding);
    return `<div class="namespaces-tiles">${latchTile()}${tiles}</div>`;
}
