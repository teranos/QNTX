/**
 * Roles Glyph — every line the gate reads about roles (ADR-034), as written.
 *
 * "its a fucking audit trail". Each row is one attestation, read out loud as
 * X is Y of Z by W, with who wrote it and when. Newest first, and a line a
 * later one superseded is kept: the store is the record, and what holds is
 * the gate's business at the moment it decides. Nothing here is settled or
 * folded.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { log, SEG } from './logger';

/** One line as the node answers it: the five slots, when, and who. */
export interface Line {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    by: string;
    at: string;
}

const GLYPH_ID = 'roles-glyph';

async function fetchLines(): Promise<Line[]> {
    const answer = await apiJson<{ lines: Line[]; count: number }>('/api/roles');
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
    box.className = 'glyph-error';
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
        empty.className = 'glyph-loading';
        empty.textContent = 'No line has been written yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'glyph-table roles-table';

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

        const by = document.createElement('td');
        by.className = 'glyph-did';
        by.textContent = short(line.by) || '—';
        if (by.textContent !== line.by) by.title = line.by;
        tr.appendChild(by);

        const at = document.createElement('td');
        at.className = 'glyph-time';
        at.textContent = fmt(line.at);
        tr.appendChild(at);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

async function refreshList(container: HTMLElement): Promise<void> {
    renderList(container, await fetchLines());
}

export function createRolesGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Roles',
        symbol: '⚙',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'roles-glyph-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'roles-list';
            listContainer.innerHTML = '<div class="glyph-loading">Reading the lines…</div>';
            content.appendChild(listContainer);

            refreshList(listContainer).catch((err: unknown) => {
                log.error(SEG.UI, '[RolesGlyph] the node did not answer for the lines', err);
                listContainer.innerHTML = '';
                listContainer.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
            });

            return content;
        },
    };
}

/** Opens the roles glyph. Called from ⍟. */
export function openRolesGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
