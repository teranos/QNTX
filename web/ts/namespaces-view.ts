import { escapeHtml } from './html-utils';

export interface NamespaceOwner {
    owner_did: string;
    minted_by: string;
    created_at: string;
}

export interface Namespace {
    name: string;
    owner: NamespaceOwner | null;
    kinds: string[];
}

// system is the node and default is the default project. Everything else is a
// project SUPER made (ADR-026).
export function kindOf(name: string): 'system' | 'default' | 'project' {
    if (name === 'system') return 'system';
    if (name === 'default') return 'default';
    return 'project';
}

// What a namespace is, for the hover — who owns it and what it holds. Neither
// belongs on the face: the face is the name, equal sized, and nothing else.
function describe(ns: Namespace): string {
    const own = ns.owner
        ? `owned by ${ns.owner.minted_by}, since ${ns.owner.created_at}`
        : 'nobody recorded who owns this';
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

export function tilesHtml(namespaces: Namespace[], selected: string, adding: boolean): string {
    const tiles = namespaces.map(ns => tile(ns, selected)).join('') + addTile(adding);
    return `<div class="namespaces-tiles">${tiles}</div>`;
}
