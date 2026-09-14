/**
 * Roles Glyph — every role the lines name (ADR-034).
 *
 * A role is what lines in system say about an upper-case word: what it may
 * write, what it may read and of whose rows, which doors it reaches and who
 * may grant it, and who holds it where. This glyph is where those lines are
 * read and written. Handing a role to a token or a person is the token's or
 * the person's own glyph: that line is about them.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createGhostButton } from './components/button';
import { jsonBody } from './http-utils';
import { log, SEG } from './logger';

/** One role as the node says it, read out loud: the lines settled. */
export interface Role {
    name: string;
    write: string[];
    read: string[];
    all: boolean;
    reach: string[];
    granters: string[];
    holders: Record<string, string[]>;
}

const GLYPH_ID = 'roles-glyph';

async function fetchRoles(): Promise<Role[]> {
    const answer = await apiJson<{ roles: Role[]; count: number }>('/api/roles');
    return answer.roles;
}

/** The three lines about a role, as a line's subject names them. */
export type Kind = 'WRITE' | 'READ' | 'REACH';

/** What a cell shows for one kind of line, read out loud. */
export function said(role: Role, kind: Kind): string {
    switch (kind) {
        case 'WRITE':
            return role.write.join(' ');
        case 'READ':
            return role.read.join(' ') + (role.all ? ' by all' : '');
        case 'REACH':
            return role.reach.join(' ') + (role.granters.length ? ' by ' + role.granters.join(' ') : '');
    }
}

/** A typed line, on the wire: the words or paths before `by`, and after it
 *  who — `all` on a READ line, the granters on a REACH line. An empty line
 *  is nothing to write, since no line is ever taken back by a word. */
export function lineFor(kind: Kind, role: string, typed: string): Record<string, unknown> | null {
    const [before, after] = typed.split(' by ');
    const words = before.trim().split(' ').filter(w => w !== '');
    if (words.length === 0) return null;
    const line: Record<string, unknown> = { subjects: [kind], predicates: words, contexts: [role] };
    const by = (after || '').trim().split(' ').filter(w => w !== '');
    if (by.length > 0) line.actors = by;
    return line;
}

async function write(kind: Kind, role: string, typed: string): Promise<boolean> {
    const line = lineFor(kind, role, typed);
    if (!line) return false;
    await apiJson('/api/attestations', jsonBody('POST', line));
    return true;
}

/** Who holds the role, one namespace per line. */
export function holdersText(role: Role): string {
    const namespaces = Object.keys(role.holders).sort();
    if (namespaces.length === 0) return '—';
    return namespaces.map(ns => `${ns}: ${role.holders[ns].join(', ')}`).join('\n');
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

// A cell that is the line, and on a press becomes the input the line is
// typed into. Enter writes it and the table is drawn again from the node;
// Escape puts the cell back as it was.
function lineCell(container: HTMLElement, role: string, kind: Kind, shown: string): HTMLTableCellElement {
    const td = document.createElement('td');
    td.style.padding = '4px 8px';
    td.style.wordBreak = 'break-word';
    td.style.overflowWrap = 'break-word';
    td.style.cursor = 'text';
    td.title = `press to write the ${kind} line`;
    td.textContent = shown || '—';

    td.addEventListener('click', () => {
        if (td.querySelector('input')) return;
        td.textContent = '';
        const input = document.createElement('input');
        input.value = shown;
        input.style.width = '100%';
        input.style.boxSizing = 'border-box';
        input.style.fontFamily = 'var(--font-mono)';
        input.style.background = 'var(--bg-secondary)';
        input.style.color = 'inherit';
        input.style.border = '1px solid var(--border-on-dark)';
        input.style.padding = '2px 4px';
        input.autocomplete = 'off';
        input.spellcheck = false;
        td.appendChild(input);
        input.focus();

        input.addEventListener('keydown', (e: KeyboardEvent) => {
            // Space opens the drawer; here it separates the words.
            e.stopPropagation();
            if (e.key === 'Escape') {
                td.textContent = shown || '—';
                return;
            }
            if (e.key !== 'Enter') return;
            e.preventDefault();
            input.disabled = true;
            write(kind, role, input.value)
                .then(wrote => {
                    if (!wrote) {
                        td.textContent = shown || '—';
                        return;
                    }
                    return refreshList(container);
                })
                .catch((err: unknown) => {
                    input.disabled = false;
                    td.querySelector('.glyph-error')?.remove();
                    td.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
                });
        });
    });
    return td;
}

/** Exported for tests: the table, given the roles. `naming` is a role being
 *  made, whose first line is what makes it. */
export function renderList(container: HTMLElement, roles: Role[], naming = ''): void {
    container.innerHTML = '';

    const drawn = roles.slice();
    if (naming !== '' && !drawn.some(r => r.name === naming)) {
        drawn.push({ name: naming, write: [], read: [], all: false, reach: [], granters: [], holders: {} });
    }

    if (drawn.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No lines name a role yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'roles-table';
    table.style.borderCollapse = 'collapse';
    table.style.fontFamily = 'var(--font-mono)';

    const head = 'text-align:left;padding:4px 8px;font-weight:normal;' +
        'color:var(--text-on-dark-tertiary);border-bottom:1px solid var(--border-on-dark);';
    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th style="${head}">Role</th>
        <th style="${head}">WRITE is</th>
        <th style="${head}">READ is</th>
        <th style="${head}">REACH is</th>
        <th style="${head}">Held by</th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const role of drawn) {
        const tr = document.createElement('tr');
        tr.dataset.role = role.name;

        const name = document.createElement('td');
        name.style.padding = '4px 8px';
        name.textContent = role.name;
        tr.appendChild(name);

        tr.appendChild(lineCell(container, role.name, 'WRITE', said(role, 'WRITE')));
        tr.appendChild(lineCell(container, role.name, 'READ', said(role, 'READ')));
        tr.appendChild(lineCell(container, role.name, 'REACH', said(role, 'REACH')));

        const holders = document.createElement('td');
        holders.style.padding = '4px 8px';
        holders.style.whiteSpace = 'pre-line';
        holders.style.wordBreak = 'break-word';
        holders.style.overflowWrap = 'break-word';
        holders.textContent = holdersText(role);
        tr.appendChild(holders);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

async function refreshList(container: HTMLElement): Promise<void> {
    renderList(container, await fetchRoles());
}

// The +: a role is an upper-case word nothing ships in the binary, and it
// exists once a line names it. Typing the word draws its row; the first line
// written on that row is what makes it.
function renderAddLink(container: HTMLElement, listContainer: HTMLElement): void {
    container.innerHTML = '';
    container.style.padding = '8px 0';
    container.style.display = 'flex';
    container.style.gap = '8px';
    container.style.alignItems = 'center';

    const add = createGhostButton('+', async () => {
        if (container.querySelector('input')) return;
        const input = document.createElement('input');
        input.placeholder = 'ROLE';
        input.style.fontFamily = 'var(--font-mono)';
        input.style.background = 'var(--bg-secondary)';
        input.style.color = 'inherit';
        input.style.border = '1px solid var(--border-on-dark)';
        input.style.padding = '2px 4px';
        input.autocomplete = 'off';
        input.spellcheck = false;
        container.appendChild(input);
        input.focus();
        input.addEventListener('keydown', (e: KeyboardEvent) => {
            e.stopPropagation();
            if (e.key === 'Escape') {
                input.remove();
                return;
            }
            if (e.key !== 'Enter') return;
            e.preventDefault();
            const naming = input.value.trim().toUpperCase();
            input.remove();
            if (naming === '') return;
            fetchRoles()
                .then(roles => { renderList(listContainer, roles, naming); })
                .catch((err: unknown) => {
                    log.error(SEG.UI, '[RolesGlyph] the node did not answer for the roles', err);
                });
        });
    });
    add.element.title = 'name a role';
    add.element.setAttribute('aria-label', 'Name a role');
    add.element.style.fontSize = '16px';
    add.element.style.lineHeight = '1';
    add.element.style.padding = '4px 10px';
    container.appendChild(add.element);
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

            const addContainer = document.createElement('div');
            addContainer.className = 'roles-add-link';
            renderAddLink(addContainer, listContainer);

            content.appendChild(addContainer);
            content.appendChild(listContainer);

            refreshList(listContainer).catch((err: unknown) => {
                log.error(SEG.UI, '[RolesGlyph] the node did not answer for the roles', err);
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
