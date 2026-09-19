import { apiFetch, connectivity } from './client';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { kindOf, tilesHtml, type Namespace, type Open } from './namespaces-view';
import { person } from './self-person';
import { standAtTheDoor } from './signin';

let bar: HTMLElement | null = null;
// The row, whose contents are rewritten, and the rectangle, which is not. One
// element for its whole life (web/CLAUDE.md): it is moved, never remade, so it
// can travel to where you stepped rather than blink out and reappear there.
let row: HTMLElement | null = null;
let rectangle: HTMLElement | null = null;
let namespaces: Namespace[] = [];
let standing = '';
// The level the node answered for this person. The nuke is offered to ROOT.
let level = '';
let adding = false;
// The one button in right-click mode, if any. Never the one being stood in.
let open: Open | null = null;
let failure = '';

// 501 when the node keeps one universe, 403 below SUPER, 401 when nobody is
// signed in yet. None is an error to show — they mean there is no bar.
async function load(): Promise<boolean> {
    const response = await apiFetch('/api/namespaces');
    if (response.status === 501 || response.status === 403 || response.status === 401) return false;
    if (!response.ok) {
        failure = `could not read namespaces: HTTP ${response.status} ${await response.text()}`;
        return true;
    }
    const data = await response.json() as { namespaces: Namespace[] };
    namespaces = data.namespaces || [];
    failure = '';
    return true;
}

function render(): void {
    if (!bar || !row) return;

    const said = failure === '' ? '' : `<div class="namespaces-failure" title="press to copy">${escapeHtml(failure)}</div>`;
    row.innerHTML = tilesHtml(namespaces, standing, adding, open, rootInSystem()) + said;
    // Where the rectangle is, for the stylesheet: default is redder from system.
    row.dataset.standing = standing;

    if (adding) row.querySelector<HTMLInputElement>('#namespace-new')?.focus();
    place();
}

// The rectangle over the namespace being stood in. It is sized and moved, so
// the transition in the stylesheet carries it from where it was; a rectangle
// that is nowhere has nothing to travel from and is simply hidden.
function place(): void {
    if (!row || !rectangle) return;

    const here = row.querySelector<HTMLElement>('.namespace-tile.standing');
    if (!here) {
        rectangle.hidden = true;
        return;
    }
    rectangle.hidden = false;
    rectangle.style.width = `${here.offsetWidth}px`;
    rectangle.style.height = `${here.offsetHeight}px`;
    rectangle.style.transform = `translate(${here.offsetLeft}px, ${here.offsetTop}px)`;
}

async function create(name: string): Promise<void> {
    const response = await apiFetch('/api/namespaces', jsonBody('POST', { name }));
    adding = false;

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, '[Namespaces] Failed to create:', name, response.status, said);
        failure = `could not create ${name}: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    await load();
    render();
}

// Moving. The rectangle goes where the node says it went, never where the click
// was — same order create() asks in, so the row can never draw a namespace the
// writes are not landing in.
async function step(name: string): Promise<void> {
    const response = await apiFetch('/i/standing', jsonBody('POST', { namespace: name }));

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, '[Namespaces] Failed to stand in:', name, response.status, said);
        failure = `could not stand in ${name}: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    const moved = await response.json() as { namespace: string };
    standing = moved.namespace;
    // The node answered a namespace other than the one pressed: this person
    // registered at a door, and a door is where their requests act whatever
    // they step to (ADR-032). Saying nothing here is a rectangle that ignores
    // a press and gives no reason for it.
    failure = moved.namespace === name
        ? ''
        : `you act in ${moved.namespace}, the door you registered at, so the rectangle stays there`;
    render();
}

// The switch on a namespace. The button stays open afterwards, showing the state
// the node now holds, so the [X] that just turned red is right there to press.
async function switchTo(name: string, enabled: boolean): Promise<void> {
    const verb = enabled ? 'enable' : 'disable';
    const response = await apiFetch(`/api/namespaces/${encodeURIComponent(name)}/${verb}`, { method: 'POST' });

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, `[Namespaces] Failed to ${verb}:`, name, response.status, said);
        failure = `could not ${verb} ${name}: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    failure = '';
    await load();
    render();
}

// Emptying default. The node refuses this from anywhere but system and from
// anyone but ROOT; what is offered here is what it would take.
async function nuke(): Promise<void> {
    const response = await apiFetch('/api/namespaces/default/nuke', { method: 'POST' });

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, '[Namespaces] Failed to nuke default:', response.status, said);
        failure = `could not nuke default: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    open = null;
    failure = '';
    await load();
    render();
}

async function end(name: string): Promise<void> {
    const response = await apiFetch(`/api/namespaces/${encodeURIComponent(name)}`, { method: 'DELETE' });

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, '[Namespaces] Failed to delete:', name, response.status, said);
        failure = `could not delete ${name}: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    open = null;
    failure = '';
    await load();
    render();
}

// A press on one of the three parts of the open button. Anything else pressed
// in the row is not the open button's.
function pressed(part: HTMLElement, name: string): void {
    const ns = namespaces.find(n => n.name === name);
    if (!ns || !open || open.name !== name) return;

    switch (part.dataset.part) {
        case 'back':
            open = null;
            render();
            return;
        case 'end':
            if (part.dataset.end === 'inert') return;
            if (part.dataset.end === 'active') {
                open = { name, sure: true };
                render();
                return;
            }
            end(name).catch((err: unknown) => log.error(SEG.UI, `Did not delete '${name}':`, err));
            return;
        case 'nuke':
            if (part.dataset.end === 'active') {
                open = { name, sure: true };
                render();
                return;
            }
            nuke().catch((err: unknown) => log.error(SEG.UI, 'Did not nuke default:', err));
            return;
    }
}

// ROOT standing in system: who may empty default, and who may end a namespace.
function rootInSystem(): boolean {
    return standing === 'system' && level === 'ROOT';
}

// Default opens for the nuke alone: from system, and to ROOT. Anything else
// the node keeps for itself does not open.
function opens(name: string): boolean {
    if (name === '' || name === standing) return false;
    if (kindOf(name) === 'project') return true;
    return kindOf(name) === 'default' && rootInSystem();
}

// The knob is dragged, and where it is let go decides. Let go on the side it
// started on, it slides back and nothing is asked of the node; a press that
// never moved is that too.
function drag(knob: HTMLElement, e: PointerEvent): void {
    const track = knob.parentElement;
    const name = knob.closest<HTMLElement>('.namespace-tile')?.dataset.name || '';
    if (!track || name === '') return;
    e.preventDefault();

    const enabled = track.dataset.on === 'true';
    const travel = track.clientWidth - knob.offsetWidth - 2;
    const from = enabled ? travel : 0;
    const startX = e.clientX;
    let at = from;
    let moved = false;

    track.classList.add('dragging');
    knob.setPointerCapture(e.pointerId);

    const move = (ev: PointerEvent) => {
        moved = true;
        at = Math.min(travel, Math.max(0, from + ev.clientX - startX));
        knob.style.transform = `translateX(${at}px)`;
    };
    const up = () => {
        knob.removeEventListener('pointermove', move);
        knob.removeEventListener('pointerup', up);
        knob.removeEventListener('pointercancel', up);
        track.classList.remove('dragging');
        // A press that never moved nudges the knob the way it would go, and it
        // snaps back: the switch is dragged, and this is it saying so.
        if (!moved) {
            knob.style.transform = `translateX(${enabled ? travel - 6 : 6}px)`;
            setTimeout(() => { knob.style.transform = ''; }, 160);
            return;
        }
        const on = at > travel / 2;
        knob.style.transform = '';
        if (on === enabled) return;
        track.dataset.on = String(on);
        switchTo(name, on).catch((err: unknown) => log.error(SEG.UI, `Did not switch '${name}':`, err));
    };
    knob.addEventListener('pointermove', move);
    knob.addEventListener('pointerup', up);
    knob.addEventListener('pointercancel', up);
}

function attach(el: HTMLElement): void {
    el.addEventListener('pointerdown', (e: PointerEvent) => {
        const knob = (e.target as HTMLElement).closest<HTMLElement>('.switch-knob');
        if (knob && e.button === 0) drag(knob, e);
    });

    el.addEventListener('contextmenu', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        const chosen = target.closest<HTMLElement>('.namespace-tile[data-name]');
        if (!chosen) return;
        e.preventDefault();

        const name = chosen.dataset.name || '';
        if (!opens(name)) return;
        open = { name, sure: false };
        render();
    });

    el.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;
        const part = target.closest<HTMLElement>('.namespace-part');
        if (part) {
            pressed(part, part.closest<HTMLElement>('.namespace-tile')?.dataset.name || '');
            return;
        }
        if (target.closest('.door-latch')) {
            standAtTheDoor();
            return;
        }
        if (target.closest('.namespace-add')) {
            adding = true;
            render();
            return;
        }
        // Beside the tiles, not swallowed into a hover: a press copies the
        // failure reason, the same acknowledgement as tokens-element.ts didCell().
        const said = target.closest<HTMLElement>('.namespaces-failure');
        if (said) {
            const message = failure;
            void navigator.clipboard.writeText(message).then(
                () => { said.textContent = 'copied'; setTimeout(() => { said.textContent = message; }, 1200); },
                () => { said.textContent = 'refused'; setTimeout(() => { said.textContent = message; }, 1200); },
            );
            return;
        }

        const chosen = target.closest<HTMLElement>('.namespace-tile[data-name]');
        if (!chosen || chosen.classList.contains('open')) return;
        const name = chosen.dataset.name || '';
        // The rectangle moves to what was pressed. There is nowhere for it to
        // go from the namespace it is already on, and it cannot land on one
        // that is switched off; the node refuses that step too.
        if (name === '' || name === standing || chosen.dataset.state === 'disabled') return;
        step(name).catch((err: unknown) => log.error(SEG.UI, `Did not stand in '${name}':`, err));
    });

    el.addEventListener('keydown', (e: KeyboardEvent) => {
        const input = e.target as HTMLInputElement;
        if (!input.classList?.contains('namespace-new')) return;

        // Space opens this drawer, so a name with a space in it must not reach
        // the global shortcut and collapse what is being typed into.
        e.stopPropagation();

        if (e.key === 'Enter') {
            e.preventDefault();
            const name = input.value.trim();
            if (name === '') return;
            create(name).catch((err: unknown) => log.error(SEG.UI, `Namespace '${name}' was not created:`, err));
        }
        if (e.key === 'Escape') {
            e.preventDefault();
            adding = false;
            render();
        }
    });
}

// Signing in happens after the page loads, so the bar has to be able to arrive
// later. Asking once at startup is how it reported 401 at somebody who was
// signed in — it had asked before they were.
export function initNamespacesBar(): void {
    const header = document.getElementById('system-drawer-header');
    if (!header) return;

    connectivity.subscribeAuth(admitted => {
        // null is nobody having asked yet. Tearing the bar down on that emptied
        // it every time a tab opened, before the node had said anything.
        if (admitted === null) return;
        if (!admitted) {
            teardown();
            return;
        }
        void appear(header);
    });
}

async function appear(header: HTMLElement): Promise<void> {
    let keeps = false;
    try {
        keeps = await load();
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Namespaces] Failed to reach /api/namespaces:', error);
        return;
    }
    if (!keeps) {
        teardown();
        return;
    }

    // Where the rectangle starts is the node's answer about this person, not a
    // guess this row can make. Without it there is no rectangle, and the reason
    // stands in the row rather than the row picking a namespace to look right.
    try {
        const who = await person();
        standing = who.standing;
        level = who.level;
    } catch (error: unknown) {
        standing = '';
        level = '';
        failure = `could not read where you are standing: ${error instanceof Error ? error.message : String(error)}`;
    }

    if (!bar) {
        bar = document.createElement('div');
        bar.className = 'namespaces-bar';

        // Made once and kept. render() rewrites the row beneath it and the
        // rectangle is not in that rewrite, so it survives to be moved.
        rectangle = document.createElement('div');
        rectangle.className = 'namespaces-rectangle';
        rectangle.hidden = true;

        row = document.createElement('div');
        row.className = 'namespaces-row';

        // The row first: a rectangle painted before the buttons is a rectangle
        // behind them, and it is exactly their size, so none of it would show.
        bar.append(row, rectangle);
        header.insertAdjacentElement('afterend', bar);
        attach(bar);
    }
    render();
}

// Losing the session takes the bar with it, rather than leaving a list of
// namespaces nobody is entitled to see any more.
function teardown(): void {
    bar?.remove();
    bar = null;
    row = null;
    rectangle = null;
    standing = '';
    level = '';
    adding = false;
    open = null;
    failure = '';
}
