/**
 * Roles Element — every line the gate reads about roles (ADR-034), as written.
 *
 * "its a fucking audit trail". Each row is one attestation, read out loud as
 * X is Y of Z by W, with who wrote it and when. Newest first, and a line a
 * later one superseded is kept: the store is the record, and what holds is
 * the gate's business at the moment it decides. Nothing here is settled or
 * folded.
 */

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createGhostButton, createPrimaryButton } from './components/button';
import { el } from './html-utils';
import { jsonBody } from './http-utils';
import { log, SEG } from './logger';
import { openTokenElement } from './token-element';
import { openUsersElement } from './users-element';
import { compose, composeAll, holdsIn, impliedFor, KINDS, knownFrom, preview, split, type Implied, type Kind, type Known, type PersonNamed, type Slots, type TokenNamed } from './roles-compose';
import openapi from '../../server/openapi/openapi.json';

/** One line as the node answers it: the five slots, when, and who. */
export interface Line {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    by: string;
    // The writer's token id when the writer is a token: the way to its element.
    by_token?: string;
    at: string;
}

const ELEMENT_ID = 'roles-element';

/** The same sentinel datapunt reads by, `SINCE` in clean/tools/datapunt/src/records.d.
 *  Attestations before it were written in a shape nobody reads any more. */
export const SINCE = '2026-09-12T16:00:00Z';

// A node answering a shape this element does not know is said as that, with the
// whole of what it answered, rather than as a property error further down.
async function fetchLines(): Promise<Line[]> {
    const answer = await apiJson<{ lines?: Line[]; count?: number }>('/api/roles');
    if (!Array.isArray(answer.lines)) {
        throw new Error(`/api/roles answered no lines: ${JSON.stringify(answer)}`);
    }
    return answer.lines;
}

/** A DID by its last eight; anything else as it is. */
export function short(s: string): string {
    return s.startsWith('did:key:') ? s.slice(-8) : s;
}

/** The line read out loud. The first actor is the writer and has its own
 *  column; what follows it, `all` or the granters, is said after `by`. */
export function said(line: Line): string {
    const subjects = line.subjects.map(short).join(' ');
    const predicates = line.predicates.join(' ');
    const contexts = line.contexts.join(' ');
    const by = line.actors.slice(1).map(short).join(' ');
    let out = `${subjects} is ${predicates}`;
    if (contexts) out += ` of ${contexts}`;
    if (by) out += ` by ${by}`;
    return out;
}

function fmt(at: string): string {
    const d = new Date(at);
    return isNaN(d.getTime()) ? at : d.toISOString().slice(0, 19).replace('T', ' ');
}

function errorBox(message: string): HTMLDivElement {
    const box = document.createElement('div');
    box.className = 'element-error';
    box.textContent = message;
    box.style.cursor = 'pointer';
    box.title = 'press to copy';
    box.addEventListener('click', () => {
        void navigator.clipboard.writeText(message).then(
            () => { box.textContent = 'copied'; setTimeout(() => { box.textContent = message; }, 1200); },
            () => { box.textContent = 'refused'; setTimeout(() => { box.textContent = message; }, 1200); },
        );
    });
    return box;
}

/** Exported for tests: the table, one row per line. */
export function renderList(container: HTMLElement, lines: Line[]): void {
    container.innerHTML = '';

    if (lines.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'element-loading';
        empty.textContent = 'No line has been written yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table roles-table';

    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th>Line</th>
        <th>by</th>
        <th>at</th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const line of lines) {
        const tr = document.createElement('tr');
        tr.dataset.id = line.id;

        const sentence = document.createElement('td');
        sentence.textContent = said(line);
        // The whole of it on hover, DIDs unshortened.
        sentence.title = `${line.subjects.join(' ')} is ${line.predicates.join(' ')} of ${line.contexts.join(' ')} by ${line.actors.join(' ')}`;
        tr.appendChild(sentence);

        // The writer is a way in: a token to its own element, a person to Users.
        const by = document.createElement('td');
        by.className = 'element-did';
        by.textContent = short(line.by) || '—';
        if (by.textContent !== line.by) by.title = line.by;
        if (line.by_token) {
            by.title = `open ${line.by}`;
            by.addEventListener('click', () => { openTokenElement(line.by_token as string, line.by); });
        } else if (line.by) {
            by.title = `open Users: ${line.by}`;
            by.addEventListener('click', () => { openUsersElement(); });
        }
        tr.appendChild(by);

        const at = document.createElement('td');
        at.className = 'element-time';
        at.textContent = fmt(line.at);
        tr.appendChild(at);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

// The lines last drawn, so the composer offers what they mention.
let latest: Line[] = [];

async function refreshList(container: HTMLElement): Promise<void> {
    latest = await fetchLines();
    renderList(container, latest);
}

/** What the node lists, for the slots: its tokens, its people, its
 *  namespaces, the predicates held where you stand, and the paths it serves.
 *  A list the node will not give is said, with the reason, and the slot
 *  offers what the lines mention. */
async function known(said: (message: string) => void): Promise<Known> {
    const ask = async <T>(path: string, what: string, read: (answer: T) => void): Promise<void> => {
        try {
            read(await apiJson<T>(path));
        } catch (err: unknown) {
            said(`${what} not offered: ${err instanceof Error ? err.message : String(err)}`);
        }
    };
    let tokens: TokenNamed[] = [];
    let people: PersonNamed[] = [];
    let namespaces: string[] = [];
    let predicates: string[] = [];
    await Promise.all([
        ask<Array<{ label: string; namespaces?: string[] }>>('/auth/tokens', 'the tokens are',
            answer => { tokens = answer.map(t => ({ label: t.label, namespaces: t.namespaces ?? [] })); }),
        ask<Array<{ namespace?: string; accounts?: Array<{ canonical_id: string }> }>>('/auth/users', 'the people are',
            answer => {
                people = answer.flatMap(u => (u.accounts ?? []).map(a => ({ route: a.canonical_id, door: u.namespace ?? '' })));
            }),
        ask<{ namespaces: Array<{ name: string }> }>('/api/namespaces', 'the namespaces are',
            answer => { namespaces = answer.namespaces.map(n => n.name); }),
        // The predicates held where you stand, from SINCE on: datapunt's own
        // line in the sand (records.d), before which the shape is one nobody
        // reads any more. ATS has no delete, so the old ones stay in the store
        // and would be offered as if they were words. The node has no answer
        // yet for every predicate in every namespace.
        ask<Array<{ predicates?: string[] }>>(`/api/attestations?since=${encodeURIComponent(SINCE)}&limit=1000`,
            'the predicates held here are',
            answer => { predicates = [...new Set(answer.flatMap(a => a.predicates ?? []))]; }),
    ]);
    return knownFrom(latest, tokens, people, namespaces, predicates, Object.keys(openapi.paths));
}

// An input that offers what exists and takes what is typed.
function slot(name: string, offered: string[], onInput: (value: string) => void): { wrap: HTMLElement; input: HTMLInputElement } {
    const listId = `roles-compose-${name}-${Math.random().toString(36).slice(2)}`;
    const list = el('datalist');
    list.id = listId;
    for (const value of offered) {
        const option = el('option');
        option.value = value;
        list.appendChild(option);
    }
    const input = el('input', { class: 'input' });
    input.setAttribute('list', listId);
    input.placeholder = name;
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.addEventListener('input', () => { onInput(input.value); });
    // Space separates words here; the drawer must not hear it.
    input.addEventListener('keydown', (e: KeyboardEvent) => { e.stopPropagation(); });
    const wrap = el('span');
    wrap.append(input, list);
    return { wrap, input };
}

/** The slots one kind of line has, and what each offers. `onRole` hears the
 *  role a grant names, so what the role lacks can be offered beside it. */
function slotsFor(kind: Kind, k: Known, s: Slots, redraw: () => void, onRole?: (role: string) => void): HTMLElement[] {
    const parts: HTMLElement[] = [];
    const word = (text: string) => el('span', { text, class: 'roles-compose-word' });
    // The inverse: the line takes its pairs away. Beside the kind, so the
    // sentence above reads with the marker the moment it is on.
    const revoke = () => {
        const box = el('label', { class: 'roles-compose-word' });
        const on = el('input');
        on.type = 'checkbox';
        on.addEventListener('change', () => { s.revoked = on.checked; redraw(); });
        box.append(on, ' revoked');
        return box;
    };
    switch (kind) {
        case 'REACH':
            parts.push(word('is'), slot('paths', k.paths, v => { s.what = split(v); redraw(); }).wrap);
            parts.push(word('of'), slot('role', k.roles, v => { s.of = v; redraw(); }).wrap);
            parts.push(word('by'), slot('granters', [...k.roles, 'SUPER', 'ATTESTOR'], v => { s.by = split(v); redraw(); }).wrap);
            parts.push(revoke());
            break;
        case 'WRITE':
            parts.push(word('is'), slot('predicates', k.words, v => { s.what = split(v); redraw(); }).wrap);
            parts.push(word('of'), slot('role', k.roles, v => { s.of = v; redraw(); }).wrap);
            parts.push(revoke());
            break;
        case 'READ':
            parts.push(word('is'), slot('predicates', k.words, v => { s.what = split(v); redraw(); }).wrap);
            parts.push(word('of'), slot('role', k.roles, v => { s.of = v; redraw(); }).wrap);
            parts.push(word('by'), slot('all', ['all'], v => { s.by = split(v); redraw(); }).wrap);
            parts.push(revoke());
            break;
        case 'GRANT':
        case 'REVOKE': {
            // The namespace is the who's: filled from their record the moment
            // the who is picked, spelled as the record spells it, and locked
            // when the record names exactly one.
            const namespace = slot('namespace', k.namespaces, v => { s.of = v; redraw(); });
            const who = slot('who', k.names, v => {
                s.who = v;
                const holds = holdsIn(k, v);
                namespace.input.readOnly = holds.length === 1 && k.acts[v] !== undefined;
                s.of = holds.length === 1 ? holds[0] : '';
                namespace.input.value = s.of;
                redraw();
            });
            parts.push(who.wrap);
            parts.push(word(kind === 'GRANT' ? 'is role:granted' : 'is role:revoked'), slot('role', k.roles, v => {
                s.what = split(v);
                onRole?.(s.what[0] ?? '');
                redraw();
            }).wrap);
            parts.push(word('of'), namespace.wrap);
            break;
        }
    }
    return parts;
}

// The lines a role lacks, offered beside a grant of it: each on by default,
// with its own slots, and a box to opt out.
function impliedRows(implied: Implied[], k: Known, role: string, redraw: () => void): HTMLElement {
    const box = el('div', { class: 'roles-compose-implied' });
    const word = (text: string) => el('span', { text, class: 'roles-compose-word' });
    for (const each of implied) {
        const row = el('div', { class: 'roles-compose-row' });
        const on = el('input');
        on.type = 'checkbox';
        on.checked = each.on;
        on.addEventListener('change', () => { each.on = on.checked; redraw(); });
        row.append(on, word(`${each.kind} is`));
        if (each.kind === 'REACH') {
            const paths = slot('paths', k.paths, v => { each.what = split(v); redraw(); });
            paths.input.value = each.what.join(' ');
            row.append(paths.wrap);
        } else {
            row.append(slot('predicates', k.words, v => { each.what = split(v); redraw(); }).wrap);
        }
        row.append(word(`of ${role.toUpperCase()}`));
        if (each.kind === 'READ') {
            const all = slot('all', ['all'], v => { each.by = split(v); redraw(); });
            all.input.value = each.by.join(' ');
            row.append(word('by'), all.wrap);
        }
        box.appendChild(row);
    }
    return box;
}

/** The whole of what Attest will write, read out loud, one line each. */
function previewAll(s: Slots, implied: Implied[]): string {
    const role = s.what[0] ?? '';
    const lines = implied.filter(i => i.on).map(i => preview({ kind: i.kind, what: i.what, of: role, who: '', by: i.by }));
    lines.push(preview(s));
    return lines.join('\n');
}

// The composer: the kind of line, then its slots, the sentence as it fills,
// and Attest. A failure is the button's own: it throws, and the button says.
function renderComposer(container: HTMLElement, listContainer: HTMLElement, k: Known): void {
    container.innerHTML = '';
    const s: Slots = { kind: 'WRITE', what: [], of: '', who: '', by: [] };

    const row = el('div', { class: 'roles-compose-row' });
    const sentence = el('div', { class: 'roles-compose-sentence' });
    sentence.style.whiteSpace = 'pre-line';
    // A grant carries the lines its role lacks (composeAll); the rest carry
    // nothing beside them.
    let implied: Implied[] = [];
    const impliedBox = el('div');
    const redraw = () => { sentence.textContent = s.kind === 'GRANT' ? previewAll(s, implied) : preview(s); };

    const kind = el('select', { class: 'input' });
    for (const each of KINDS) {
        const option = el('option', { text: each });
        option.value = each;
        kind.appendChild(option);
    }
    kind.value = s.kind;

    const onRole = (role: string) => {
        implied = s.kind === 'GRANT' ? impliedFor(role, latest) : [];
        impliedBox.innerHTML = '';
        if (implied.length > 0) impliedBox.appendChild(impliedRows(implied, k, role, redraw));
    };
    const slotsBox = el('span', { class: 'roles-compose-row' });
    const drawSlots = () => {
        slotsBox.innerHTML = '';
        s.what = []; s.of = ''; s.who = ''; s.by = []; s.revoked = false;
        onRole('');
        for (const part of slotsFor(s.kind, k, s, redraw, onRole)) slotsBox.appendChild(part);
        redraw();
    };
    kind.addEventListener('change', () => { s.kind = kind.value as Kind; drawSlots(); });

    const attest = createPrimaryButton('Attest', async () => {
        const made = s.kind === 'GRANT' ? composeAll(s, implied) : compose(s);
        if ('missing' in made) throw new Error(`the line is missing ${made.missing}`);
        const lines = 'lines' in made ? made.lines : [made.line];
        for (const line of lines) {
            await apiJson('/api/attestations', jsonBody('POST', line));
        }
        container.hidden = true;
        await refreshList(listContainer);
    });

    row.append(kind, slotsBox, attest.element);
    container.append(sentence, row, impliedBox);
    drawSlots();
}

export function createRolesElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Roles',
        symbol: '⚙',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'roles-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'roles-list';
            listContainer.innerHTML = '<div class="element-loading">Reading the lines…</div>';

            // The + opens the composer. What the slots offer is read when it
            // opens, so it is what the node holds then.
            const composer = el('div', { class: 'roles-compose' });
            composer.hidden = true;
            const notes = el('div');
            const said = (message: string) => { notes.appendChild(errorBox(message)); };
            const open = createGhostButton('+', async () => {
                notes.innerHTML = '';
                if (!composer.hidden) {
                    composer.hidden = true;
                    return;
                }
                renderComposer(composer, listContainer, await known(said));
                composer.hidden = false;
            });
            open.element.title = 'write a line';
            open.element.setAttribute('aria-label', 'Write a line');
            open.element.style.fontSize = '16px';
            open.element.style.lineHeight = '1';
            open.element.style.padding = '4px 10px';

            const top = el('div', { style: { display: 'flex', gap: '8px', alignItems: 'center' } });
            top.appendChild(open.element);
            content.append(top, notes, composer, listContainer);

            refreshList(listContainer).catch((err: unknown) => {
                log.error(SEG.UI, '[RolesElement] the node did not answer for the lines', err);
                listContainer.innerHTML = '';
                listContainer.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
            });

            return content;
        },
    };
}

/** Opens the roles element. Called from ⍟. */
export function openRolesElement(): void {
    tray.open(ELEMENT_ID);
}
