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

import { apiFetch } from './client';
import { log, SEG } from './logger';
import { startField, type Field, type Mood } from './door-field';
import { createButton } from './components/button';

const DOOR_ID = 'door';
const OPEN_MS = 620;

// Long enough to read a node's name while it is letting you in.
const NAMED_MS = 2600;

// Lit while the door is up, and only while it is up. The door outlives every
// face it wears, so this is tied to being shown rather than to being built.
let lit: Field | null = null;
let seeded: {
    c: [number, number];
    hue: [number, number, number];
    stand: number;
    circle: number;
} | null = null;
let mooded: Mood = 'rest';
let chose = false;

/** How far the door has come towards letting you in. */
export function mood(next: Mood): void {
    mooded = next;
    if (next === 'rest') chose = false;
    lit?.mood(next);
    // The door wears where it has got to, so what is drawn on it can answer.
    document.getElementById(DOOR_ID)?.setAttribute('data-mood', next);
}

/**
 * Shows the node's name for a moment. Signing in is the one time the door says
 * which node it is without being asked — you are about to be let into it.
 */
export function nameYourself(): void {
    const band = document.getElementById(DOOR_ID)?.querySelector('.door-node');
    if (!band || band.classList.contains('door-node-shown')) return;
    band.classList.add('door-node-shown');
    setTimeout(() => { band.classList.remove('door-node-shown'); }, NAMED_MS);
}

/** The way in that was picked has been put back, so the door stops holding. */
export function unpick(): void {
    chose = false;
    if (mooded === 'hover') mood('rest');
}

/**
 * The fingerprint is white until the node answers, and then it is the answer.
 * Same instant as the field, because they are saying the same thing.
 */
export function verdict(said: 'yes' | 'no' | null): void {
    const print = document.getElementById(DOOR_ID)?.querySelector('.door-fingerprint');
    if (!print) return;
    print.classList.toggle('door-print-yes', said === 'yes');
    print.classList.toggle('door-print-no', said === 'no');
}

// The ways in are drawn and thrown away as the door changes face, so this
// listens on the door itself rather than on anything it happens to be holding.
const WAYS_IN = '.door-fingerprint, .door-provider';

function follow(door: HTMLElement): void {
    door.addEventListener('mouseover', (e) => {
        if (mooded !== 'rest') return;
        const to = e.target as Element | null;
        if (!to?.closest?.(WAYS_IN)) return;
        mood('hover');
    });

    door.addEventListener('mouseout', (e) => {
        if (mooded !== 'hover') return;
        // Having chosen a way in, you are still in the middle of taking it.
        // Only the pointer left; the reaching did not stop.
        if (chose) return;
        const from = e.target as Element | null;
        if (!from?.closest?.(WAYS_IN)) return;
        // Crossing onto a child of the same control is not leaving it.
        const onto = (e as MouseEvent).relatedTarget as Element | null;
        if (onto?.closest?.(WAYS_IN)) return;
        mood('rest');
    });

    door.addEventListener('click', (e) => {
        const hit = e.target as Element | null;

        // Pressing a way in is taking it. Picking a provider is not — that is
        // saying which one, and what it asks for has not been answered yet.
        if (hit?.closest?.('.door-fingerprint, .door-continue')) {
            mood('committed');
            return;
        }
        if (hit?.closest?.('.door-provider')) {
            chose = true;
            mood('hover');
        }
    });
}

// Six hex characters off the DID, as a colour and as text. A door not wearing
// your node's face is not your node, and that is checkable at a glance rather
// than by reading forty characters of base58.
function spread(did: string, seed: number): number {
    let h = seed;
    for (const ch of did) {
        h ^= ch.charCodeAt(0);
        h = Math.imul(h, 0x01000193) >>> 0;
    }
    return h / 0x100000000;
}

// Airplane is the family every door belongs to. Where in it a node sits, and
// how far back it stands, are the node's own.
const AIRPLANE: [number, number] = [-1.7549, 0];
const SHIFT = 0.006;

function faceOf(did: string): {
    tint: string;
    short: string;
    c: [number, number];
    hue: [number, number, number];
    stand: number;
    circle: number;
} {
    let hash = 0;
    for (const ch of did) {
        hash = (hash * 31 + ch.charCodeAt(0)) % 0xffffff;
    }

    return {
        tint: '#' + hash.toString(16).padStart(6, '0'),
        short: did.slice(-8),
        c: [
            AIRPLANE[0] + (spread(did, 0x811c9dc5) - 0.5) * 2 * SHIFT,
            AIRPLANE[1] + (spread(did, 0x9e3779b9) - 0.5) * 2 * SHIFT,
        ],
        hue: [
            ((hash >> 16) & 0xff) / 255,
            ((hash >> 8) & 0xff) / 255,
            (hash & 0xff) / 255,
        ],
        stand: 0.85 + spread(did, 0x85ebca6b) * 0.4,
        circle: 0.85 + spread(did, 0xc2b2ae35) * 0.4,
    };
}

/** Puts the node's own identity on the door: its DID, and a colour from it. */
async function wearNodeFace(door: HTMLElement): Promise<void> {
    let did = '';
    try {
        const response = await apiFetch('/.well-known/did.json');
        if (!response.ok) return;
        const doc = await response.json() as { id?: string };
        did = typeof doc.id === 'string' ? doc.id : '';
    } catch (error: unknown) {
        log.warn(SEG.UI, '[Door] the node did not say who it is:', error);
        return;
    }
    if (!did) return;

    const face = faceOf(did);
    const band = document.createElement('div');
    band.className = 'door-node';
    band.style.borderColor = face.tint;
    band.title = did;
    band.textContent = face.short;
    door.style.setProperty('--door-node-tint', face.tint);
    door.prepend(band);

    seeded = { c: face.c, hue: face.hue, stand: face.stand, circle: face.circle };
    lit?.seed(face.c, face.hue, face.stand, face.circle);
}

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
    return build().plate;
}

/**
 * The middle column: the ways in this browser can take right now with what it
 * already holds. The fingerprint lives here, and a QR would.
 */
export function doorStand(): HTMLElement {
    return build().stand;
}

/**
 * Builds the door on first call and empties it on every call after. Both
 * columns are cleared together: the door shows one thing at a time, and a
 * leftover step reads as a choice that is still open.
 */
function build(): { stand: HTMLElement; plate: HTMLElement } {
    const existing = document.getElementById(DOOR_ID);
    if (existing) {
        const stand = existing.querySelector('.door-stand') as HTMLElement;
        const plate = existing.querySelector('.door-plate') as HTMLElement;
        stand.replaceChildren();
        plate.replaceChildren();
        return { stand, plate };
    }

    const door = document.createElement('div');
    door.id = DOOR_ID;

    const mark = document.createElement('img');
    mark.className = 'door-mark';
    mark.src = '/qntx.jpg';
    mark.alt = 'QNTX';

    const stand = document.createElement('div');
    stand.className = 'door-stand';

    const plate = document.createElement('div');
    plate.className = 'door-plate';

    const line = document.createElement('div');
    line.className = 'door-say';

    // Press once and the node says which one it is. Press again and it goes
    // with you, because an id you can read is one you are about to paste.
    mark.addEventListener('click', () => {
        const band = door.querySelector('.door-node') as HTMLElement | null;
        if (!band) return;
        if (band.classList.toggle('door-node-shown')) return;
        void navigator.clipboard.writeText(band.title);
        say('node id copied');
    });

    // <canvas> is the one element that can hand us the WebGL context for the node-unique fractal.
    const field = document.createElement('canvas');
    field.className = 'door-field';

    door.append(field, mark, stand, plate, line);
    follow(door);
    drawer()?.prepend(door);
    void wearNodeFace(door);
    return { stand, plate };
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

    const panel = document.getElementById(DOOR_ID);
    if (panel) panel.style.display = '';

    if (!lit) {
        const field = panel?.querySelector('.door-field') as HTMLCanvasElement | null;
        if (field) lit = startField(field);
        if (seeded) lit?.seed(seeded.c, seeded.hue, seeded.stand, seeded.circle);

        // Only the dev server puts this on the page.
        if (lit && panel && (window as { __DEV__?: boolean }).__DEV__) {
            void import('./door-dev').then(({ mountDials }) => {
                if (lit) mountDials(panel, lit);
            });
        }
    }

    const loading = scrim();
    if (!loading) return;
    loading.style.transition = `opacity ${OPEN_MS}ms ease-out`;
    loading.style.opacity = '0';
    setTimeout(() => { loading.style.display = 'none'; }, OPEN_MS);
}

// One panel at a time. The indicator rail is built before the door now, so a
// 401 from any request can reach the sign-in face while first time setup is
// halfway through a provider ceremony — and draw straight over it.
let engaged = false;

/** Claims the panel, so nothing else draws on it until this is given back. */
export function engageDoor(held: boolean): void {
    engaged = held;
}

/** Whether something already has the panel. */
export function doorEngaged(): boolean {
    return engaged;
}

/** Marks the door as first-time setup, which is the one state it wears tape in. */
export function hazard(taped: boolean): void {
    document.getElementById(DOOR_ID)?.classList.toggle('door-hazard', taped);
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
    // The panel goes out of the layout entirely: a bar minimised to six pixels
    // still shows the top of a hundred-pixel lamp through them.
    setTimeout(() => {
        bar.classList.remove('door-opening');
        bar.style.height = barHeight ?? '';
        barHeight = null;

        const panel = document.getElementById(DOOR_ID);
        if (panel) {
            panel.style.display = 'none';
            panel.classList.remove('door-opening');
        }

        lit?.stop();
        lit = null;
    }, OPEN_MS);
}

/** What the door is saying. Falls back to the loader's own line before the
 *  door is hung, so a message is never spoken to nothing. */
// TODO: render a StatusItem through a shared primitive instead of a string, so
// the door is a fourth surface beside json, ansi and tmux. No such primitive
// exists in web/ts — StatusItem and renderLine are in server/.
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

/**
 * A step, said out loud and kept.
 */

// Claiming a node happens once and touches a provider, a key, a store and an
// authenticator. When it goes wrong the person needs to see how far it got, so
// every step lands on the plate as well as in the loader's log.
export function step(message: string, bad = false): void {
    if (window.logLoaderStep) window.logLoaderStep(message, bad);
    trace(message, bad);
}

/** Appends to the door's own record of what it has done. */
export function trace(message: string, bad = false): void {
    const door = document.getElementById(DOOR_ID);
    if (!door) return;

    let kept = door.querySelector('.door-trace');
    if (!kept) {
        kept = document.createElement('div');
        kept.className = 'door-trace';
        door.append(kept);
    }

    const line = document.createElement('div');
    line.className = bad ? 'door-trace-line door-trace-bad' : 'door-trace-line';
    line.textContent = message;
    kept.append(line);
    kept.scrollTop = kept.scrollHeight;
}

/** Clears the record, for a door that is starting something new. */
export function untrace(): void {
    document.getElementById(DOOR_ID)?.querySelector('.door-trace')?.remove();
}

/** A machined slot you press, optionally carrying a mark of its own. */
export function pressable(label: string, onPress: () => void, mark?: SVGSVGElement | null): HTMLElement {
    return createButton({
        label,
        onClick: onPress,
        variant: 'ghost',
        className: mark ? 'door-press door-press-marked' : 'door-press',
        mark,
    }).element;
}

/** The way past a step that is not required. Quieter than what it sits under. */
export function skippable(label: string, onPress: () => void): HTMLElement {
    return createButton({
        label,
        onClick: onPress,
        variant: 'ghost',
        className: 'door-skip',
    }).element;
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

/**
 * Whether the dev server is serving this against a node that is not this origin.
 * The relay omits __BACKEND_URL__ exactly then, to keep the node unnamed here.
 */
export function relayed(): boolean {
    const w = window as { __DEV__?: boolean; __BACKEND_URL__?: string };
    return Boolean(w.__DEV__) && !w.__BACKEND_URL__;
}

/** The key: the way in where a passkey cannot reach, because a passkey is bound
 *  to the origin that minted it. Same place, same round, same one press. */
export function tokenMark(onPress: () => void): HTMLButtonElement {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('width', '44');
    svg.setAttribute('height', '44');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '1.6');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');

    const ring = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    ring.setAttribute('cx', '8');
    ring.setAttribute('cy', '8');
    ring.setAttribute('r', '4.25');

    const shaft = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    shaft.setAttribute('d', 'M11 11 L20 20 M17.5 17.5 L15.5 19.5 M20 20 L18 22');
    svg.append(ring, shaft);

    const btn = document.createElement('button');
    btn.className = 'door-fingerprint door-key';
    btn.setAttribute('aria-label', 'Sign in with the token the dev server carries');
    btn.append(svg);
    btn.addEventListener('click', () => { onPress(); });
    return btn;
}

/** The fingerprint. One press is the whole of signing in when this browser is
 *  already known, so it is the largest thing the door ever draws. */
export function fingerprint(onPress: () => void): HTMLButtonElement {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('width', '44');
    svg.setAttribute('height', '44');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '1.6');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');

    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', 'M13.14 21C10.81 19.54 9.25 16.95 9.25 14c0-1.52 1.23-2.75 2.75-2.75s2.75 1.23 2.75 2.75c0 1.52 1.23 2.75 2.75 2.75s2.75-1.23 2.75-2.75C20.25 9.44 16.55 5.75 12 5.75S3.76 9.44 3.76 14c0 1.02.11 2 .32 2.95M8.49 20.3C7.24 18.51 6.5 16.34 6.5 14c0-3.04 2.46-5.5 5.5-5.5s5.5 2.46 5.5 5.5M17.79 19.48c-.1.01-.2.01-.3.01-3.04 0-5.5-2.46-5.5-5.5M19.67 6.48C17.8 4.35 15.06 3 12 3S6.2 4.35 4.33 6.48');
    svg.append(path);

    const btn = document.createElement('button');
    btn.className = 'door-fingerprint';
    btn.setAttribute('aria-label', 'Sign in');
    btn.append(svg);
    btn.addEventListener('click', () => { onPress(); });
    return btn;
}

/**
 * What went wrong: on the plate, kept on the plate, and in the console.
 */

// The status line is overwritten by the next thing the door says, and the
// loader's log panel is hidden by the time the door is up. So a failure that
// only went to those two places was gone before it could be read.
export function stumbled(where: string, e: unknown): void {
    const message = e instanceof Error ? e.message : String(e);
    log.warn(SEG.UI, `[Door] ${where}:`, e);
    say(message, true);

    trace(`${where}: ${message}`, true);
}
