/**
 * Users Glyph — ROOT over every User (ADR-031).
 */

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// Plain window, reached from the Self glyph. Lists every User as the record
// holds them, and the switch on each: off carries ROOT's name so the person
// cannot switch it back; on is on whoever switched it off. The minimal
// management is seeing everyone and the one act on a person.

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createDangerButton, createPrimaryButton } from './components/button';
import { log, SEG } from './logger';
import { person } from './self-person';

/** One User, as the record holds them. Nothing here is a secret: a key is a
 *  DID and an account is what a provider calls it. */
export interface UserRecord {
    id: string;
    display_name?: string;
    email_addresses?: string[];
    phone_numbers?: string[];
    level: string;
    namespace?: string;
    created_by?: string;
    disabled_by?: string;
    keys?: { did: string; origin: string }[];
    accounts?: { provider: string; canonical_id: string; handle?: string }[];
    created_at: number;
}

const GLYPH_ID = 'users-glyph';

/** What the ROOT User is called before they say otherwise (ADR-031). */
const ROOT_NAME = 'root';

/** A User who has said nothing, and is not ROOT. */
const UNNAMED = '—';

async function fetchUsers(): Promise<UserRecord[]> {
    return await apiJson<UserRecord[]>('/auth/users');
}

/** Flips the switch on one User. The node's refusal is the error. */
async function flip(id: string, verb: 'disable' | 'enable'): Promise<void> {
    await apiJson<{ status: string }>(`/auth/users/${encodeURIComponent(id)}/${verb}`, { method: 'POST' });
}

/** What to call a User: their name, root for the ROOT User, and a dash for
 *  a person who has not said. */
export function nameOf(u: UserRecord): string {
    if (u.display_name) return u.display_name;
    if (u.level === 'ROOT') return ROOT_NAME;
    return UNNAMED;
}

/** A User's created_at is milliseconds, the way the node writes it. */
export function fmt(ms: number): string {
    if (!ms) return '—';
    return new Date(ms).toISOString().slice(0, 19).replace('T', ' ');
}

function cell(text: string, className = ''): HTMLTableCellElement {
    const td = document.createElement('td');
    td.className = className;
    td.textContent = text;
    return td;
}

/** On or off, and who switched it. */
function statusPill(u: UserRecord): HTMLTableCellElement {
    const td = document.createElement('td');
    const pill = document.createElement('span');

    if (u.disabled_by) {
        pill.textContent = 'off';
        pill.className = 'glyph-pill glyph-pill-off';
        td.appendChild(pill);
        const by = document.createElement('span');
        by.className = 'glyph-pill-when';
        by.textContent = `by ${u.disabled_by}`;
        td.appendChild(by);
        return td;
    }
    pill.textContent = 'on';
    pill.className = 'glyph-pill glyph-pill-on';
    td.appendChild(pill);
    return td;
}

/** How a User is reached. The row carries the count; the hover carries every
 *  route, each account by what it calls itself and each key by the tail of
 *  its DID. */
export function reachedBy(u: UserRecord): { shown: string; whole: string } {
    const accounts = u.accounts ?? [];
    const keys = u.keys ?? [];
    if (accounts.length === 0 && keys.length === 0) return { shown: '—', whole: '' };

    const routes: string[] = [];
    for (const a of accounts) routes.push(a.handle || a.canonical_id);
    for (const k of keys) routes.push(`${k.origin.toLowerCase()} …${k.did.slice(-8)}`);

    const count = (n: number, what: string) => `${n} ${what}${n === 1 ? '' : 's'}`;
    const parts: string[] = [];
    if (accounts.length > 0) parts.push(count(accounts.length, 'account'));
    if (keys.length > 0) parts.push(count(keys.length, 'key'));
    return { shown: parts.join(', '), whole: routes.join('\n') };
}

/** Exported for tests: which control a row offers is the switch itself.
 *  `switches` is whether the viewer may switch anybody: a session may, a token
 *  may not, and a row does not offer a token what a token cannot do. */
export function renderList(container: HTMLElement, users: UserRecord[], switches = true): void {
    container.innerHTML = '';

    if (users.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No Users. This node belongs to nobody yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'glyph-table users-table';

    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th>Name</th>
        <th>Level</th>
        <th>Door</th>
        <th>Reached by</th>
        <th>Email</th>
        <th>Phone</th>
        <th>Created</th>
        <th>Status</th>
        ${switches ? '<th></th>' : ''}
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const u of users) {
        const tr = document.createElement('tr');

        const name = cell(nameOf(u));
        // The id is the one thing about a User that never changes, and it is
        // on the row rather than in a hover.
        name.title = u.id;
        tr.appendChild(name);
        tr.appendChild(cell(u.level));
        tr.appendChild(cell(u.namespace || '—'));
        const reached = reachedBy(u);
        const routes = cell(reached.shown, 'glyph-time');
        routes.title = reached.whole;
        tr.appendChild(routes);
        tr.appendChild(cell(u.email_addresses?.length ? u.email_addresses.join(', ') : '—'));
        tr.appendChild(cell(u.phone_numbers?.length ? u.phone_numbers.join(', ') : '—'));
        tr.appendChild(cell(fmt(u.created_at), 'glyph-time'));
        tr.appendChild(statusPill(u));

        if (switches) {
            const action = document.createElement('td');
            action.className = 'glyph-actions';
            if (u.disabled_by) {
                const on = createPrimaryButton('Switch on', async () => {
                    await flip(u.id, 'enable');
                    await refreshList(container);
                });
                action.appendChild(on.element);
            } else {
                const off = createDangerButton('Switch off', 'Confirm switch off', async () => {
                    await flip(u.id, 'disable');
                    await refreshList(container);
                });
                action.appendChild(off.element);
            }
            tr.appendChild(action);
        }

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

// Who is looking decides what the rows offer: a session switches, a token
// only reads.
async function refreshList(container: HTMLElement): Promise<void> {
    const [users, who] = await Promise.all([fetchUsers(), person()]);
    renderList(container, users, who.via !== 'token');
}

export function createUsersGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Users',
        symbol: '⚇',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'users-glyph-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'users-list';
            listContainer.innerHTML = '<div class="glyph-loading">Loading Users…</div>';
            content.appendChild(listContainer);

            refreshList(listContainer).catch((err: unknown) => {
                log.error(SEG.UI, '[UsersGlyph] the node did not list its Users', err);
                listContainer.innerHTML = '';
                const message = `the node did not list its Users: ${err instanceof Error ? err.message : String(err)}`;
                const errBox = document.createElement('div');
                errBox.className = 'glyph-error';
                errBox.textContent = message;
                errBox.style.cursor = 'pointer';
                errBox.title = 'press to copy';
                errBox.addEventListener('click', () => {
                    void navigator.clipboard.writeText(message).then(
                        () => { errBox.textContent = 'copied'; setTimeout(() => { errBox.textContent = message; }, 1200); },
                        () => { errBox.textContent = 'refused'; setTimeout(() => { errBox.textContent = message; }, 1200); },
                    );
                });
                listContainer.appendChild(errBox);
            });

            return content;
        },
    };
}

/** Opens the Users glyph. Called from the Self glyph. */
export function openUsersGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
