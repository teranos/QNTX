/**
 * The page behind the canvas: the namespace's.
 *
 * Who is looking, top-left. The namespace's name. "the list of canvasses in
 * that namespace" — each with its owners and its state; a disabled one
 * desaturated, and still openable. "⌗" to create one: pressed, it reveals
 * the name field with a same-sized + beside it, and the + is the system's
 * two-stage button — once to arm, once to create. And the way out of the
 * door, in the corner.
 */

import type { Person } from './self-person';
import { standAtTheDoor } from './signin';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger';
import { Subcanvas } from './sym';
import { openCanvas, openCanvasKey, openOnceKey, standingNamespace } from './standing';
import { go } from './address';
import { pressFrom, type Press } from '@teranos/elements';
import { pressPlace, placePresses } from './components/press-places';
import {
    createCanvas, disableCanvas, disownCanvas, enableCanvas, inviteOwner, listCanvases,
    nukeCanvas, type CanvasRow,
} from './api/canvases';

let host: HTMLElement | null = null;
let body: HTMLElement | null = null;
let who: Person | null = null;
let canvases: CanvasRow[] = [];
let failure = '';
// The birth in progress: which kind the ⌗ makes.
let birth: 'namespace' | 'user' | null = null;
// How the page is built again for another canvas. A test builds nothing.
let rebuild: (id: string) => void = (id) => go(standingNamespace(), id);

function privileged(): boolean {
    return who?.level === 'ROOT' || who?.level === 'SUPER';
}

function hasNamespaceCanvas(): boolean {
    return canvases.some(c => c.kind === 'namespace');
}

function hasOwnCanvas(): boolean {
    return canvases.some(c => c.kind === 'user' && c.mine);
}

/**
 * Opens a canvas: remembered per namespace, and the page is built for it.
 * Opening it from the list is deliberate, so a disabled one opens this once.
 */
export function enter(id: string): void {
    try {
        localStorage.setItem(openCanvasKey(), id);
        localStorage.setItem(openOnceKey(), '1');
    } catch (err: unknown) {
        log.error(SEG.UI, '[Namespace] The open canvas was not remembered:', err);
    }
    rebuild(id);
}

/** What the page is built for where this person stands. */
export interface Opening {
    /** The canvas open: a User's id, or empty for the namespace's own. */
    open: string;
    /** Whether any canvas opens at all. */
    hasCanvas: boolean;
    /** Whether the one opening is disabled, and so drawn desaturated. */
    disabled: boolean;
}

/**
 * The canvas to open: the one the address names, else the one remembered, if
 * still here, else the namespace's own. "becomes desaturated in the view and
 * can either be seen by opening it" — a disabled one opens only when it was
 * just pressed, and never on its own.
 */
export function opening(rows: CanvasRow[], addressed = ''): Opening {
    let remembered = '';
    let once = false;
    try {
        remembered = addressed !== '' ? addressed : localStorage.getItem(openCanvasKey()) ?? '';
        once = localStorage.getItem(openOnceKey()) === '1';
        localStorage.removeItem(openOnceKey());
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Namespace] The open canvas was not read:', err);
    }
    const row = remembered === ''
        ? rows.find(c => c.kind === 'namespace')
        : rows.find(c => c.id === remembered);
    if (!row) {
        const own = rows.find(c => c.kind === 'namespace');
        if (!own) return { open: '', hasCanvas: false, disabled: false };
        if (own.disabled_by !== '' && !once) return { open: '', hasCanvas: false, disabled: false };
        return { open: '', hasCanvas: true, disabled: own.disabled_by !== '' };
    }
    const disabled = row.disabled_by !== '';
    if (disabled && !once) return { open: '', hasCanvas: false, disabled: false };
    return { open: row.kind === 'namespace' ? '' : row.id, hasCanvas: true, disabled };
}

// One ⌗. "so, yeah, the first need is to create a canvas right": where the
// namespace has none and this person may make it, ⌗ makes the namespace's;
// otherwise it makes their own.
function nextBirth(): { kind: 'namespace' | 'user'; label: string } | null {
    if (privileged() && !hasNamespaceCanvas()) return { kind: 'namespace', label: 'the namespace\'s canvas' };
    if (!hasOwnCanvas()) return { kind: 'user', label: 'your own canvas' };
    return null;
}

function ownerHtml(c: CanvasRow): string {
    if (c.owner_views.length === 0) {
        return `<span class="canvas-owner unowned">${c.kind === 'namespace' ? 'the namespace' : 'unowned'}</span>`;
    }
    return c.owner_views.map(o => `<span class="canvas-owner">${escapeHtml(o.name)}</span>`).join('');
}

// The acts a row offers, as presses, put in their places once the row is in.
function rowActs(c: CanvasRow, presses: Record<string, Press>): string {
    const acts: string[] = [];
    const id = c.id;
    const place = (press: Press) => {
        presses[press.id] = press;
        acts.push(pressPlace(press.id, 'canvas-act'));
    };
    if (c.disabled_by === '') {
        place({ id: `disable-${id}`, title: 'disable', again: 'the canvas is disabled', does: () => act(() => disableCanvas(id)) });
    } else {
        place({ id: `enable-${id}`, title: 'enable', does: () => act(() => enableCanvas(id)) });
        if (privileged()) {
            place({ id: `disown-${id}`, title: 'unown', again: 'nobody owns it', does: () => act(() => disownCanvas(id)) });
            // "a canvas can be nuked when disabled, like we do with namespaces themselves."
            place({ id: `nuke-${id}`, title: 'nuke', again: 'gone, for good', irrevocable: true, does: () => act(() => nukeCanvas(id)) });
        }
    }
    if (c.kind === 'user' && (c.mine || privileged())) {
        place({ id: `invite-${id}`, title: 'invite…', does: () => withAsk('the e-mail of the User to invite', email => inviteOwner(id, email)) });
    }
    return acts.join('');
}

// A canvas is a card: what it is called, whose it is, its state, its acts.
function cardHtml(c: CanvasRow, presses: Record<string, Press>): string {
    const open = openCanvas() === c.id || (openCanvas() === '' && c.kind === 'namespace');
    const state = c.disabled_by ? `disabled by ${escapeHtml(c.disabled_by)}` : (open ? 'open' : '');
    return `
        <div class="canvas-card ${c.disabled_by ? 'disabled' : ''} ${open ? 'open' : ''}" data-id="${escapeHtml(c.id)}">
            <div class="canvas-card-head">
                <span class="canvas-card-symbol">${Subcanvas}</span>
                <span class="canvas-card-name">${escapeHtml(c.name)}</span>
            </div>
            <div class="canvas-card-kind">${c.kind === 'namespace' ? 'the namespace\'s' : 'a User\'s'}</div>
            <div class="canvas-card-owners">${ownerHtml(c)}</div>
            <div class="canvas-card-state">${state}</div>
            <div class="canvas-card-actions">${rowActs(c, presses)}</div>
        </div>`;
}

function birthHtml(presses: Record<string, Press>): string {
    const may = nextBirth();
    if (!may) return '';
    if (!birth) {
        presses.birth = { id: 'birth', title: may.label, mark: Subcanvas, does: () => { birth = may.kind; render(); } };
        return pressPlace('birth', 'canvas-birth-symbol');
    }
    // "press once to check, button becomes green and shimmers, 2nd click
    // actually creates it": the press's two stages; a canvas made can be undone, so armed it stays white.
    const kind = birth;
    presses.plus = {
        id: 'plus', title: '+', again: 'the canvas is made', armedForMs: 8000,
        does: async () => {
            const name = body?.querySelector<HTMLInputElement>('.canvas-birth-name')?.value.trim() ?? '';
            if (name === '') throw new Error('a canvas is named');
            const made = await createCanvas(name, kind);
            birth = null;
            enter(kind === 'namespace' ? '' : made.id);
        },
    };
    return `
        <div class="canvas-birth-form" data-kind="${kind}">
            <span class="canvas-birth-symbol pressed">${Subcanvas}</span>
            <input class="canvas-birth-name" type="text" placeholder="name ${escapeHtml(may.label)}" autocomplete="off" spellcheck="false">
            ${pressPlace('plus', 'canvas-birth-plus')}
        </div>`;
}

function render(): void {
    if (!body) return;
    const presses: Record<string, Press> = {};
    const said = failure === '' ? '' : `<div class="namespace-page-failure">${escapeHtml(failure)}</div>`;
    const rows = canvases.length === 0
        ? '<div class="canvas-none">no canvas here yet</div>'
        : canvases.map(c => cardHtml(c, presses)).join('');
    body.innerHTML = `
        <h2 class="namespace-page-name">${escapeHtml(who && who.standing !== '' ? who.standing : 'you stand in no namespace')}</h2>
        <div class="canvas-list">${rows}</div>
        <input class="canvas-ask" type="text" placeholder="an e-mail, for invite…" autocomplete="off" spellcheck="false">
        <div class="canvas-birth">${birthHtml(presses)}</div>
        ${said}`;
    placePresses(body, presses);
    if (birth) body.querySelector<HTMLInputElement>('.canvas-birth-name')?.focus();
}

async function reload(): Promise<void> {
    try {
        canvases = await listCanvases();
        failure = '';
    } catch (err: unknown) {
        failure = err instanceof Error ? err.message : String(err);
    }
    render();
}

// An act on a canvas, then the list asked again. A refusal is the button's
// own to show, so it is thrown to it.
async function act(doing: () => Promise<unknown>): Promise<void> {
    await doing();
    await reload();
}

// An act that needs a User id or an e-mail takes it from the field beside
// the list, and says so when it is empty.
async function withAsk(what: string, doing: (typed: string) => Promise<unknown>): Promise<void> {
    const field = body?.querySelector<HTMLInputElement>('.canvas-ask');
    const typed = field?.value.trim() ?? '';
    if (typed === '') {
        field?.focus();
        throw new Error(`type ${what} in the field beside the list, then press again`);
    }
    await doing(typed);
    await reload();
}

function attach(el: HTMLElement): void {
    el.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;
        // The system's buttons handle themselves.
        if (target.closest('button')) return;
        const row = target.closest<HTMLElement>('.canvas-card');
        if (!row) return;
        const c = canvases.find(x => x.id === row.dataset.id);
        if (!c) return;
        enter(c.kind === 'namespace' ? '' : c.id);
    });

    el.addEventListener('keydown', (e: KeyboardEvent) => {
        const input = e.target as HTMLInputElement;
        if (!input.classList?.contains('canvas-birth-name') && !input.classList?.contains('canvas-ask')) return;
        // Space opens the system drawer; a name with a space must not reach it.
        e.stopPropagation();
        if (e.key === 'Escape' && input.classList.contains('canvas-birth-name')) {
            birth = null;
            render();
        }
        // Enter does nothing here: the + is what commits, pressed twice.
    });
}

/**
 * Builds the namespace's page into the container, behind whatever canvas is
 * open, and takes the who block from the header to its own top-left. Nobody
 * signed in, or a node with one universe, gets no page.
 */
export function initNamespacePage(person: Person | null, rows: CanvasRow[], again: (id: string) => void = (id) => go(standingNamespace(), id)): void {
    const container = document.getElementById('container');
    if (!container || !person) return;
    who = person;
    canvases = rows;
    rebuild = again;
    birth = null;
    failure = '';

    host = document.createElement('section');
    host.id = 'namespace-page';

    const top = document.createElement('div');
    top.className = 'namespace-page-top';
    const whoBlock = document.querySelector<HTMLElement>('#header .who');
    if (whoBlock) top.append(whoBlock);
    const out = pressFrom({ id: 'namespace-page-out', title: 'the door', mark: '[<]', does: () => { standAtTheDoor(); } });
    out.element.classList.add('namespace-page-out');
    top.append(out.element);

    body = document.createElement('div');
    body.className = 'namespace-page-body';

    host.append(top, body);
    container.classList.add('namespace-page-shown');
    container.append(host);
    attach(host);
    render();
}

// The list is asked once more after an act, and this is what the acts use.
export { reload as reloadNamespacePage };
