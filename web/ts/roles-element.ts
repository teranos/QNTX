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
import { createTable, tableCell, tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';
import { openTokenElement } from './token-element';
import { openUsersElement } from './users-element';

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

export const ELEMENT_ID = 'roles-element';

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

    const rows = lines.map(line => {
        const sentence = tableCell(said(line));
        // The whole of it on hover, DIDs unshortened.
        sentence.title = `${line.subjects.join(' ')} is ${line.predicates.join(' ')} of ${line.contexts.join(' ')} by ${line.actors.join(' ')}`;

        // The writer is a way in: a token to its own element, a person to Users.
        const by = tableCell(short(line.by) || '—', 'element-did');
        if (by.textContent !== line.by) by.title = line.by;
        if (line.by_token) {
            by.title = `open ${line.by}`;
            by.addEventListener('click', () => { openTokenElement(line.by_token as string, line.by); });
        } else if (line.by) {
            by.title = `open Users: ${line.by}`;
            by.addEventListener('click', () => { openUsersElement(); });
        }

        return { id: line.id, cells: [sentence, by, tableCell(fmt(line.at), 'element-time')] as const };
    });
    const table = createTable('roles', ['Line', 'by', 'at'], rows);
    container.appendChild(table);
}

async function refreshList(container: HTMLElement): Promise<void> {
    renderList(container, await fetchLines());
}

// Who this is for: whoever reaches these (audience.ts).
export const needs = ['/api/roles'] as const;

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

            // "its a true ui failure"
            // A line is read here, never written.
            content.append(listContainer);

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
