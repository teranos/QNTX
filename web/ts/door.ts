/**
 * The door: the metal face inside the system bar that the node keeps shut until
 * it knows who you are.
 */

// Not being recognised is not a step in loading, so it gets no line in the log
// panel. The scrim finishes what it can do without you, lifts, and the system
// bar is standing open with the door in it.

// Opening the door is the bar minimising and the door collapsing inside it. The
// door is not thrown away — it is how you walked in, and it is how you walk back
// out.

import { log, SEG } from './logger';

const DOOR_ID = 'door';
const OPEN_MS = 620;

function drawer(): HTMLElement | null {
    return document.getElementById('system-drawer');
}

/** The loading screen, which the door waits behind until the scrim lifts. */
function scrim(): HTMLElement | null {
    return document.getElementById('loading-screen');
}

/**
 * The plate the door draws into, hung in the system bar on first call. Calling
 * again empties it: the door shows one thing at a time, and a leftover step
 * reads as a choice that is still open.
 */
export function doorHost(): HTMLElement {
    const existing = document.getElementById(DOOR_ID);
    if (existing) {
        const held = existing.querySelector('.door-plate') as HTMLElement;
        held.replaceChildren();
        return held;
    }

    const door = document.createElement('div');
    door.id = DOOR_ID;

    const plate = document.createElement('div');
    plate.className = 'door-plate';

    const line = document.createElement('div');
    line.className = 'door-say';

    door.append(plate, line);
    drawer()?.prepend(door);
    return plate;
}

// What the bar was before the door took it over. The drawer sets its height
// inline, so a door that cleared it would hand back a bar of no fixed size.
let barHeight: string | null = null;

/** Stands the door up in the bar, and lifts the scrim if it is still there. */
export function showDoor(): void {
    const bar = drawer();
    if (bar) {
        if (barHeight === null) barHeight = bar.style.height;
        bar.classList.remove('door-opening');
        bar.classList.add('door-held');
        bar.style.height = 'var(--door-height)';
    }
    document.getElementById(DOOR_ID)?.classList.remove('door-opening');

    const loading = scrim();
    if (!loading) return;
    loading.style.transition = `opacity ${OPEN_MS}ms ease-out`;
    loading.style.opacity = '0';
    setTimeout(() => { loading.style.display = 'none'; }, OPEN_MS);
}

/** Opens it. The bar minimises, the door collapses inside it, and it stays. */
export function stepThrough(): void {
    const bar = drawer();
    if (!bar) return;
    bar.classList.remove('door-held');
    bar.classList.add('door-opening');
    document.getElementById(DOOR_ID)?.classList.add('door-opening');
    bar.style.height = '6px';

    // Handing the height back is what lets the bar behave like a bar again.
    setTimeout(() => {
        bar.classList.remove('door-opening');
        bar.style.height = barHeight ?? '';
        barHeight = null;
    }, OPEN_MS);
}

/** What the door is saying. Falls back to the loader's own line before the
 *  door is hung, so a message is never spoken to nothing. */
export function say(message: string, bad = false): void {
    const line = document.querySelector('.door-say') as HTMLElement | null;
    if (line) {
        line.textContent = message;
        line.classList.toggle('door-bad', bad);
        return;
    }
    const status = document.getElementById('loading-status');
    if (status) status.textContent = message;
}

/** A line in the loader's log panel, for the steps that are steps. */
export function step(message: string, bad = false): void {
    if (window.logLoaderStep) window.logLoaderStep(message, bad);
}

/** A machined slot you press. */
export function pressable(label: string, onPress: () => void): HTMLElement {
    const line = document.createElement('div');
    line.className = 'door-press';
    line.textContent = label;
    line.addEventListener('click', () => { onPress(); });
    return line;
}

/** The way past a step that is not required. Quieter than what it sits under. */
export function skippable(label: string, onPress: () => void): HTMLElement {
    const line = document.createElement('div');
    line.className = 'door-skip';
    line.textContent = label;
    line.addEventListener('click', () => { onPress(); });
    return line;
}

export interface DoorField {
    el: HTMLElement;
    input: HTMLInputElement;
}

/** An etched field. */
export function field(label: string, type: string, placeholder = ''): DoorField {
    const wrap = document.createElement('label');
    wrap.className = 'door-field';

    const caption = document.createElement('span');
    caption.textContent = label;
    wrap.append(caption);

    const input = document.createElement('input');
    input.type = type;
    input.placeholder = placeholder;
    input.autocomplete = 'off';
    input.spellcheck = false;
    wrap.append(input);

    return { el: wrap, input };
}

/** The fingerprint. One press is the whole of signing in when this browser is
 *  already known, so it is the largest thing the door ever draws. */
export function fingerprint(onPress: () => void): HTMLButtonElement {
    const btn = document.createElement('button');
    btn.className = 'door-fingerprint';
    btn.setAttribute('aria-label', 'Sign in');
    btn.innerHTML = `<svg viewBox="0 0 24 24" width="44" height="44" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M13.14 21C10.81 19.54 9.25 16.95 9.25 14c0-1.52 1.23-2.75 2.75-2.75s2.75 1.23 2.75 2.75c0 1.52 1.23 2.75 2.75 2.75s2.75-1.23 2.75-2.75C20.25 9.44 16.55 5.75 12 5.75S3.76 9.44 3.76 14c0 1.02.11 2 .32 2.95M8.49 20.3C7.24 18.51 6.5 16.34 6.5 14c0-3.04 2.46-5.5 5.5-5.5s5.5 2.46 5.5 5.5M17.79 19.48c-.1.01-.2.01-.3.01-3.04 0-5.5-2.46-5.5-5.5M19.67 6.48C17.8 4.35 15.06 3 12 3S6.2 4.35 4.33 6.48"/></svg>`;
    btn.addEventListener('click', () => { onPress(); });
    return btn;
}

/** What went wrong, on the plate and in the browser console. */
export function stumbled(where: string, e: unknown): void {
    const message = e instanceof Error ? e.message : String(e);
    log.warn(SEG.UI, `[Door] ${where}:`, e);
    say(message, true);
}
