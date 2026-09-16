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

// A button in right-click mode: which one, and whether its [X] has been
// pressed once already.
export interface Open {
    name: string;
    sure: boolean;
}

// The rectangle is where you are standing, so exactly one tile carries it and
// it is never on nothing. Which one is the node's answer, not this row's.
// What the ns.toml says, or that there is none. A namespace nothing defines is
// not on: the store refuses to switch it and refuses to delete it.
function stateOf(ns: Namespace): 'enabled' | 'disabled' | 'undefined' {
    if (!ns.definition) return 'undefined';
    return ns.definition.enabled ? 'enabled' : 'disabled';
}

function tile(ns: Namespace, standing: string, open: Open | null, mayEnd: boolean): string {
    if (open && open.name === ns.name) return openTile(ns, open.sure, mayEnd);
    const name = escapeHtml(ns.name);
    const here = ns.name === standing ? ' standing' : '';
    return `<div class="namespace-tile${here}" data-kind="${kindOf(ns.name)}" data-state="${stateOf(ns)}"` +
        ` data-name="${name}" title="${escapeHtml(describe(ns))}">${name}</div>`;
}

// The switch, or the word for a namespace that has none to switch.
function middle(state: 'enabled' | 'disabled' | 'undefined'): string {
    if (state === 'undefined') {
        return `<span class="namespace-part" data-part="toggle" data-state="undefined">undefined</span>`;
    }
    return `<span class="namespace-part" data-part="toggle">` +
        `<span class="switch-track" data-on="${state === 'enabled'}">` +
        `<span class="switch-label">ON</span><span class="switch-label">OFF</span>` +
        `<span class="switch-knob"></span></span></span>`;
}

// Default right-clicked from system: a separate flow, the way back and the
// nuke. Red, armed after one press, and the second press empties default.
function nukeTile(sure: boolean): string {
    return `<div class="namespace-tile open" data-kind="default" data-name="default" title="default">` +
        `<span class="namespace-part" data-part="back">&lt;</span>` +
        `<span class="namespace-part" data-part="nuke" data-end="${sure ? 'sure' : 'active'}">nuke</span>` +
        `</div>`;
}

// The right-clicked button, split in three: the way back, the switch, and the
// end. The end is inert while enabled, or unless ROOT stands in system, which is
// what the node demands of a delete; red otherwise, and armed after one press.
function openTile(ns: Namespace, sure: boolean, mayEnd: boolean): string {
    if (kindOf(ns.name) === 'default') return nukeTile(sure);
    const name = escapeHtml(ns.name);
    const state = stateOf(ns);
    const end = state !== 'disabled' || !mayEnd ? 'inert' : sure ? 'sure' : 'active';
    return `<div class="namespace-tile open" data-kind="${kindOf(ns.name)}" data-name="${name}" title="${name}">` +
        `<span class="namespace-part" data-part="back">&lt;</span>` +
        middle(state) +
        `<span class="namespace-part" data-part="end" data-end="${end}">X</span>` +
        `</div>`;
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

// The node's two first, in the node's order: system leftmost, default directly
// after it, then the projects as the node listed them.
export function ordered(namespaces: Namespace[]): Namespace[] {
    const system = namespaces.filter(ns => kindOf(ns.name) === 'system');
    const fallback = namespaces.filter(ns => kindOf(ns.name) === 'default');
    const projects = namespaces.filter(ns => kindOf(ns.name) === 'project');
    return [...system, ...fallback, ...projects];
}

export function tilesHtml(namespaces: Namespace[], standing: string, adding: boolean, open: Open | null = null, mayEnd = false): string {
    const tiles = ordered(namespaces).map(ns => tile(ns, standing, open, mayEnd)).join('') + addTile(adding);
    return `<div class="namespaces-tiles">${latchTile()}${tiles}</div>`;
}
