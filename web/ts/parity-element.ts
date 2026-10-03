/**
 * Parity Element (≍) — a signum held to a reference it follows, as a seam.
 */

// "D+ is what I am settling for"
// "In the tray, like everything else like users"
// "Let's say that I'm also interested in seeing prose from the specs
// themselves where they have it"
//
// One row per column of a model the signum follows: the reference's column and
// what it says of it on one side, the fields that follow it and what each says
// of itself on the other. What says nothing is said to say nothing.

import type { Element } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';
import { Parity } from './sym';

/** One column of a model of the reference, as parity hold gives it. */
export interface HeldItem {
    column: string;
    score: number;
    followed: string[];
    departs: string[];
    says: string;
    /** Where says was read, when not from the spec's schema itself. */
    says_from: string;
    required: boolean;
}

/** One model of the reference. */
export interface HeldClade {
    model: string;
    says: string;
    says_from: string;
    score: number;
    items: HeldItem[];
}

/** What parity hold gives. */
export interface Held {
    signum: string;
    sigil: string;
    reference: string;
    clades: HeldClade[];
    unfollowed: Record<string, string[]>;
    missing: string[];
    required: string[];
    ours: Record<string, string>;
}

/** One signum, and the references it can be held to. */
export interface Follows {
    signum: string;
    declares: string[];
    by_shape: string[];
}

const ELEMENT_ID = 'parity-element';

/** A field of ours as the seam shows it: protocol.Sigil.does is Sigil.does. */
export function short(full: string): string {
    const dot = full.indexOf('.');
    return dot < 0 ? full : full.slice(dot + 1);
}

/** A message of ours is named by package and message alone: protocol.Sigil. */
function isMessage(full: string): boolean {
    const first = full.indexOf('.');
    return first >= 0 && full.indexOf('.', first + 1) < 0;
}

/** The clades anything follows: the models the seam is drawn for. */
export function followedClades(held: Held): HeldClade[] {
    return held.clades.filter(clade => clade.items.some(item => item.followed.length > 0));
}

function el(tag: string, className: string, text?: string): HTMLElement {
    const node = document.createElement(tag);
    node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
}

/** What one side says, or that it says nothing. */
function says(text: string, silence: string): HTMLElement {
    return text ? el('span', 'parity-says', text) : el('span', 'parity-says parity-silent', silence);
}

/** What one piece of a row says, each word of it apart from the next. */
function spoken(node: Node): string {
    if (!(node instanceof HTMLElement) || node.children.length === 0) return (node.textContent ?? '').trim();
    return [...node.childNodes].map(spoken).filter(Boolean).join(' ');
}

/** A row as copied: their side, the mark, our side, then why it departs. */
function rowText(row: HTMLElement): string {
    const parts: string[] = [];
    const departures: string[] = [];
    for (const child of row.children) {
        if (child.classList.contains('parity-departure')) {
            departures.push(spoken(child));
            continue;
        }
        parts.push(child.classList.contains('parity-side')
            ? [...child.children].map(spoken).filter(Boolean).join(' · ')
            : spoken(child));
    }
    return [parts.filter(Boolean).join('  '), ...departures].join('\n');
}

// "clicking a row should make it copy to clickboard"
function toClipboard(text: string): void {
    navigator.clipboard.writeText(text).catch((err: unknown) =>
        log.error(SEG.UI, '[ParityElement] a row did not reach the clipboard:', err));
}

/** The seam of one model: its words and ours, then a row per column, then
 *  what of ours follows nothing. A pressed row is copied. Exported for tests. */
export function renderSeam(container: HTMLElement, held: Held, clade: HeldClade, copy: (text: string) => void = toClipboard): void {
    container.innerHTML = '';
    container.onclick = (e) => {
        const row = (e.target as HTMLElement).closest('.parity-row') as HTMLElement | null;
        if (row && container.contains(row)) copy(rowText(row));
    };

    const head = el('div', 'parity-row parity-row-head');
    const theirs = el('div', 'parity-side');
    theirs.appendChild(el('span', 'parity-where', held.reference));
    theirs.appendChild(el('span', 'parity-name', clade.model));
    theirs.appendChild(says(clade.says, 'the spec says nothing of it'));
    if (clade.says_from) theirs.appendChild(el('span', 'parity-where', `from ${clade.says_from}`));
    head.appendChild(theirs);
    head.appendChild(el('span', 'parity-mark'));
    const ours = el('div', 'parity-side');
    const messages = Object.keys(held.ours).filter(isMessage).sort();
    ours.appendChild(el('span', 'parity-where', held.signum));
    ours.appendChild(el('span', 'parity-name', messages.map(short).join(', ')));
    for (const message of messages) {
        if (held.ours[message]) ours.appendChild(says(held.ours[message], ''));
    }
    head.appendChild(ours);
    container.appendChild(head);

    for (const item of clade.items) {
        const conforms = item.score === 100;
        const departs = item.departs.length > 0;
        const followed = item.followed.length > 0;
        const owed = item.required && !followed;
        const tone = conforms ? 'parity-conforms' : departs ? 'parity-departs' : owed ? 'parity-owed' : 'parity-unfollowed';

        const row = el('div', 'parity-row');

        const column = el('div', 'parity-side');
        const name = el('span', `parity-name ${tone}`, item.column);
        if (item.required) name.appendChild(el('span', owed ? 'parity-required parity-owed' : 'parity-required', 'REQUIRED'));
        column.appendChild(name);
        column.appendChild(says(item.says, 'the spec says nothing of it'));
        if (item.says_from) column.appendChild(el('span', 'parity-where', `from ${item.says_from}`));
        row.appendChild(column);

        row.appendChild(el('span', `parity-mark ${tone}`, conforms ? '=' : departs ? '≠' : ''));

        const fields = el('div', 'parity-side');
        if (!followed) {
            fields.appendChild(el('span', `parity-name parity-nothing ${tone}`, owed ? 'required, and nothing follows' : 'nothing follows'));
        }
        for (const field of item.followed) {
            fields.appendChild(el('span', 'parity-name', short(field)));
            fields.appendChild(says(held.ours[field] ?? '', 'says nothing of itself'));
        }
        row.appendChild(fields);

        for (const reason of item.departs) row.appendChild(el('div', 'parity-departure', reason));
        container.appendChild(row);
    }

    const out: string[] = [];
    for (const message of Object.keys(held.unfollowed).sort()) {
        out.push(`${short(message)}: ${held.unfollowed[message].join(' · ')}`);
    }
    for (const missing of held.missing) out.push(`${missing}, which the spec does not have`);
    if (out.length > 0) {
        const footer = el('div', 'parity-out-of-spec parity-nothing');
        footer.appendChild(el('div', '', 'fields that follow no column'));
        for (const line of out) footer.appendChild(el('div', '', line));
        container.appendChild(footer);
    }
}

/** One pill per model a reference has that anything follows, then the seam
 *  of the first. A reference the node refused to hold says why, in its place.
 *  Exported for tests. */
export function renderHolds(content: HTMLElement, holds: Held[], refused: string[] = []): void {
    content.innerHTML = '';
    const pills = el('div', 'parity-pills');
    const seam = el('div', '');
    content.appendChild(pills);
    for (const why of refused) content.appendChild(el('div', 'element-error', why));
    content.appendChild(seam);

    const pressed: HTMLButtonElement[] = [];
    for (const held of holds) {
        for (const clade of followedClades(held)) {
            const pill = document.createElement('button');
            pill.className = 'parity-pill';
            pill.setAttribute('aria-pressed', 'false');
            pill.appendChild(el('span', 'parity-pill-reference', held.reference));
            pill.appendChild(document.createTextNode(` · ${clade.model} ${clade.score}`));
            pill.addEventListener('click', () => {
                for (const other of pressed) other.setAttribute('aria-pressed', 'false');
                pill.setAttribute('aria-pressed', 'true');
                renderSeam(seam, held, clade);
            });
            pressed.push(pill);
            pills.appendChild(pill);
        }
    }
    if (pressed.length === 0) {
        seam.appendChild(el('div', 'element-loading', 'Nothing of this signum follows any reference the node pins.'));
        return;
    }
    pressed[0].click();
}

async function hold(signum: string, reference: string): Promise<Held> {
    const query = new URLSearchParams({ signum, reference });
    return await apiJson<Held>(`/api/parity/hold?${query.toString()}`);
}

/** Every reference a signum can be held to, held: its own first. One the
 *  node refuses does not take the others with it. */
async function holdAll(follows: Follows): Promise<{ held: Held[]; refused: string[] }> {
    const references = [...follows.declares, ...follows.by_shape.filter(r => !follows.declares.includes(r))];
    const asked = await Promise.allSettled(references.map(reference => hold(follows.signum, reference)));
    const held: Held[] = [];
    const refused: string[] = [];
    asked.forEach((answer, i) => {
        if (answer.status === 'fulfilled') {
            held.push(answer.value);
            return;
        }
        const why = answer.reason instanceof Error ? answer.reason.message : String(answer.reason);
        log.error(SEG.UI, `[ParityElement] the node did not hold ${follows.signum} to ${references[i]}`, answer.reason);
        refused.push(`${references[i]}: ${why}`);
    });
    return { held, refused };
}

function failed(container: HTMLElement, what: string, err: unknown): void {
    log.error(SEG.UI, `[ParityElement] ${what}`, err);
    container.innerHTML = '';
    container.appendChild(el('div', 'element-error', `${what}: ${err instanceof Error ? err.message : String(err)}`));
}

export function createParityElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Parity',
        symbol: Parity,
        renderContent: () => {
            const content = el('div', 'parity-element-content');
            const choose = el('div', 'parity-pills');
            const label = document.createElement('label');
            label.className = 'parity-where';
            label.textContent = 'signum';
            const select = document.createElement('select');
            select.id = 'parity-signum';
            label.htmlFor = select.id;
            choose.appendChild(label);
            choose.appendChild(select);
            const holds = el('div', '');
            holds.appendChild(el('div', 'element-loading', 'Asking the node what follows what…'));
            content.appendChild(choose);
            content.appendChild(holds);

            const show = (follows: Follows) => {
                holds.innerHTML = '';
                holds.appendChild(el('div', 'element-loading', `Holding ${follows.signum}…`));
                holdAll(follows).then(
                    ({ held, refused }) => renderHolds(holds, held, refused),
                    (err: unknown) => failed(holds, `the node did not hold ${follows.signum}`, err),
                );
            };

            apiJson<Follows[]>('/api/parity/follows').then(rows => {
                for (const row of rows) {
                    const option = document.createElement('option');
                    option.value = row.signum;
                    option.textContent = row.declares.length > 0 ? `${row.signum} · ${row.declares.join(', ')}` : row.signum;
                    select.appendChild(option);
                }
                // A signum that declares what it follows is the one opened on.
                const first = rows.find(row => row.declares.length > 0) ?? rows[0];
                if (!first) {
                    holds.innerHTML = '';
                    holds.appendChild(el('div', 'element-loading', 'The node serves no signum.'));
                    return;
                }
                select.value = first.signum;
                select.addEventListener('change', () => {
                    const row = rows.find(r => r.signum === select.value);
                    if (row) show(row);
                });
                show(first);
            }, (err: unknown) => failed(holds, 'the node did not say what follows what', err));

            return content;
        },
    };
}
