/**
 * The page behind the canvas: the namespace's.
 *
 * "the list of canvasses in that namespace" — each with its owners and its
 * state; a disabled one desaturated, and still openable. "⌗" to create one:
 * pressed, it reveals the name field with a same-sized + beside it, and the
 * + is pressed twice — once to arm it, once to create. And the way out of
 * the door.
 */

import type { Person } from './self-person';
import { standAtTheDoor } from './signin';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger';
import { Subcanvas } from './sym';
import { openCanvas, openCanvasKey } from './standing';
import {
    createCanvas, disableCanvas, disownCanvas, enableCanvas, grantAccess, inviteOwner, listCanvases,
    removeOwner, revokeAccess, type CanvasRow,
} from './api/canvases';

let host: HTMLElement | null = null;
let who: Person | null = null;
// How the page is built again for another canvas. A test builds nothing.
let rebuild: () => void = () => location.reload();
let canvases: CanvasRow[] = [];
let failure = '';
// The birth in progress: which kind, and whether the + is armed.
let birth: { kind: 'namespace' | 'user'; armed: boolean } | null = null;

function privileged(): boolean {
    return who?.level === 'ROOT' || who?.level === 'SUPER';
}

function hasNamespaceCanvas(): boolean {
    return canvases.some(c => c.kind === 'namespace');
}

function hasOwnCanvas(): boolean {
    return canvases.some(c => c.kind === 'user' && c.mine);
}

/** Opens a canvas: remembered per namespace, and the page is built for it. */
export function enter(id: string): void {
    const remembered = id;
    try {
        localStorage.setItem(openCanvasKey(), remembered);
    } catch (err: unknown) {
        log.error(SEG.UI, '[Namespace] The open canvas was not remembered:', err);
    }
    rebuild();
}

/** The canvas remembered as open where this person stands, if it is still theirs. */
export function rememberedCanvas(rows: CanvasRow[]): string {
    let remembered = '';
    try {
        remembered = localStorage.getItem(openCanvasKey()) ?? '';
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Namespace] The open canvas was not read:', err);
    }
    if (remembered === '') return '';
    return rows.some(c => c.id === remembered) ? remembered : '';
}

function ownerHtml(c: CanvasRow): string {
    if (c.owner_views.length === 0) {
        return `<span class="canvas-owner unowned">${c.kind === 'namespace' ? 'the namespace' : 'unowned'}</span>`;
    }
    return c.owner_views.map(o => {
        const picture = o.picture ? `<span class="canvas-owner-dot" title="${escapeHtml(o.name)}"></span>` : '';
        return `<span class="canvas-owner">${picture}${escapeHtml(o.name)}</span>`;
    }).join('');
}

function rowHtml(c: CanvasRow): string {
    const open = openCanvas() === c.id || (openCanvas() === '' && c.kind === 'namespace');
    const state = c.disabled_by ? `disabled by ${escapeHtml(c.disabled_by)}` : '';
    const actions: string[] = [];
    if (c.disabled_by === '') {
        actions.push(`<button class="canvas-act" data-act="disable" data-id="${escapeHtml(c.id)}">disable</button>`);
    } else {
        actions.push(`<button class="canvas-act" data-act="enable" data-id="${escapeHtml(c.id)}">enable</button>`);
        if (privileged()) {
            actions.push(`<button class="canvas-act" data-act="disown" data-id="${escapeHtml(c.id)}">unown</button>`);
            actions.push(`<button class="canvas-act" data-act="give" data-id="${escapeHtml(c.id)}">give to…</button>`);
        }
    }
    if (c.kind === 'user' && (c.mine || privileged())) {
        actions.push(`<button class="canvas-act" data-act="invite" data-id="${escapeHtml(c.id)}">invite…</button>`);
    }
    if (c.kind === 'namespace' && privileged()) {
        actions.push(`<button class="canvas-act" data-act="grant" data-id="${escapeHtml(c.id)}">grant…</button>`);
    }
    return `
        <div class="canvas-row ${c.disabled_by ? 'disabled' : ''} ${open ? 'open' : ''}" data-id="${escapeHtml(c.id)}">
            <span class="canvas-row-symbol">${Subcanvas}</span>
            <span class="canvas-row-name">${escapeHtml(c.name)}</span>
            <span class="canvas-row-kind">${c.kind === 'namespace' ? 'the namespace\'s' : 'a User\'s'}</span>
            <span class="canvas-row-owners">${ownerHtml(c)}</span>
            <span class="canvas-row-state">${state}</span>
            <span class="canvas-row-actions">${actions.join('')}</span>
        </div>`;
}

function birthHtml(): string {
    const may: { kind: 'namespace' | 'user'; label: string }[] = [];
    if (privileged() && !hasNamespaceCanvas()) may.push({ kind: 'namespace', label: 'the namespace\'s canvas' });
    if (!hasOwnCanvas()) may.push({ kind: 'user', label: 'your own canvas' });
    if (may.length === 0) return '';

    if (!birth) {
        return may.map(m => `
            <button class="canvas-birth-symbol" data-kind="${m.kind}" title="create ${m.label}">${Subcanvas}</button>`).join('');
    }
    const label = may.find(m => m.kind === birth?.kind)?.label ?? '';
    return `
        <div class="canvas-birth-form" data-kind="${birth.kind}">
            <span class="canvas-birth-symbol pressed">${Subcanvas}</span>
            <input class="canvas-birth-name" type="text" placeholder="name ${escapeHtml(label)}" autocomplete="off" spellcheck="false">
            <button class="canvas-birth-plus ${birth.armed ? 'armed' : ''}" title="${birth.armed ? 'press again to create' : 'press once to check, again to create'}">+</button>
        </div>`;
}

function render(): void {
    if (!host) return;
    const said = failure === '' ? '' : `<div class="namespace-page-failure">${escapeHtml(failure)}</div>`;
    const rows = canvases.length === 0
        ? '<div class="canvas-none">no canvas here yet</div>'
        : canvases.map(rowHtml).join('');
    host.innerHTML = `
        <div class="namespace-page-head">
            <button class="namespace-page-out" title="the door">[&lt;]</button>
            <h2 class="namespace-page-name">${escapeHtml(who?.standing || 'default')}</h2>
        </div>
        <div class="canvas-list">${rows}</div>
        <div class="canvas-birth">${birthHtml()}</div>
        ${said}`;
    if (birth) host.querySelector<HTMLInputElement>('.canvas-birth-name')?.focus();
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

function said(err: unknown): void {
    failure = err instanceof Error ? err.message : String(err);
    render();
}

async function act(action: string, id: string, host: HTMLElement): Promise<void> {
    switch (action) {
        case 'disable': await disableCanvas(id); break;
        case 'enable': await enableCanvas(id); break;
        case 'disown': await disownCanvas(id); break;
        case 'give': {
            const user = askInline(host, 'the User id to give it to');
            if (!user) return;
            await disownCanvas(id);
            await grantOrOwn(id, user);
            break;
        }
        case 'invite': {
            const email = askInline(host, 'the e-mail of the User to invite');
            if (!email) return;
            await inviteOwner(id, email);
            break;
        }
        case 'grant': {
            const user = askInline(host, 'the User id to grant a look');
            if (!user) return;
            await grantAccess(id, user);
            break;
        }
    }
    await reload();
}

async function grantOrOwn(id: string, user: string): Promise<void> {
    const { addOwner } = await import('./api/canvases');
    await addOwner(id, user);
}

// A one-line ask beside the list, without the banned prompt(): the value is
// typed into the birth field, which is already there for typing names.
function askInline(host: HTMLElement, what: string): string {
    const field = host.querySelector<HTMLInputElement>('.canvas-ask');
    if (field && field.value.trim() !== '') return field.value.trim();
    failure = `type ${what} in the field beside the list, then press the button again`;
    if (!field) {
        const ask = document.createElement('input');
        ask.className = 'canvas-ask';
        ask.type = 'text';
        ask.placeholder = what;
        host.querySelector('.canvas-list')?.after(ask);
    }
    render();
    host.querySelector<HTMLInputElement>('.canvas-ask')?.focus();
    return '';
}

function attach(el: HTMLElement): void {
    el.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;

        if (target.closest('.namespace-page-out')) {
            standAtTheDoor();
            return;
        }
        const symbol = target.closest<HTMLElement>('button.canvas-birth-symbol');
        if (symbol) {
            birth = { kind: symbol.dataset.kind === 'namespace' ? 'namespace' : 'user', armed: false };
            render();
            return;
        }
        const plus = target.closest<HTMLElement>('.canvas-birth-plus');
        if (plus && birth) {
            const name = el.querySelector<HTMLInputElement>('.canvas-birth-name')?.value.trim() ?? '';
            if (name === '') {
                failure = 'a canvas is named';
                render();
                return;
            }
            if (!birth.armed) {
                birth.armed = true;
                failure = '';
                render();
                el.querySelector<HTMLInputElement>('.canvas-birth-name')!.value = name;
                return;
            }
            const kind = birth.kind;
            createCanvas(name, kind)
                .then(made => {
                    birth = null;
                    enter(kind === 'namespace' ? '' : made.id);
                })
                .catch(said);
            return;
        }
        const act_ = target.closest<HTMLElement>('.canvas-act');
        if (act_) {
            e.stopPropagation();
            act(act_.dataset.act ?? '', act_.dataset.id ?? '', el).catch(said);
            return;
        }
        const row = target.closest<HTMLElement>('.canvas-row');
        if (row) {
            const c = canvases.find(x => x.id === row.dataset.id);
            if (!c) return;
            enter(c.kind === 'namespace' ? '' : c.id);
        }
    });

    el.addEventListener('keydown', (e: KeyboardEvent) => {
        const input = e.target as HTMLInputElement;
        if (!input.classList?.contains('canvas-birth-name') && !input.classList?.contains('canvas-ask')) return;
        // Space opens the system drawer; a name with a space must not reach it.
        e.stopPropagation();
        if (e.key === 'Escape') {
            birth = null;
            failure = '';
            render();
        }
        // Enter does nothing here: the + is what commits, pressed twice.
    });
}

/**
 * Builds the namespace's page into the container, behind whatever canvas is
 * open. Nobody signed in, or a node with one universe, gets no page.
 */
export function initNamespacePage(person: Person | null, rows: CanvasRow[], again: () => void = () => location.reload()): void {
    const container = document.getElementById('container');
    if (!container || !person) return;
    who = person;
    canvases = rows;
    rebuild = again;
    host = document.createElement('section');
    host.id = 'namespace-page';
    container.append(host);
    attach(host);
    render();
}

// The list is asked once more after an act, and this is what the acts use.
export { reload as reloadNamespacePage, removeOwner, revokeAccess };
