/**
 * Users Element — ROOT over every User (ADR-031).
 */

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// Plain window, reached from the Self element. Lists every User as the record
// holds them; pressing a name opens that User, where the switch on them is.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createGhostButton } from './components/button';
import { log, SEG } from './logger';
import { person } from './self-person';
import { openUserElement } from './user-element';
import { openInvitationCancel, openUserInviteElement, type InvitationRecord } from './user-invite-element';

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

const ELEMENT_ID = 'users-element';

/** What the ROOT User is called before they say otherwise (ADR-031). */
const ROOT_NAME = 'root';

/** A User who has said nothing, and is not ROOT. */
const UNNAMED = '—';

async function fetchUsers(): Promise<UserRecord[]> {
    return await apiJson<UserRecord[]>('/auth/users');
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
        pill.className = 'element-pill element-pill-off';
        td.appendChild(pill);
        const by = document.createElement('span');
        by.className = 'element-pill-when';
        by.textContent = `by ${u.disabled_by}`;
        td.appendChild(by);
        return td;
    }
    pill.textContent = 'on';
    pill.className = 'element-pill element-pill-on';
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

/** Exported for tests: every User, a row each. A row is a way in to the
 *  User and nothing else. */
export function renderList(container: HTMLElement, users: UserRecord[], invitations: InvitationRecord[] = []): void {
    container.innerHTML = '';

    if (users.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'element-loading';
        empty.textContent = 'No Users. This node belongs to nobody yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table users-table';

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
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    // "but why dont i see my outgoing invitations in the same list, and a way for me to open the would-be-user"
    for (const inv of invitations) {
        if (inv.cancelled_at || inv.accepted_by) continue;
        tbody.appendChild(invitationRow(inv));
    }
    for (const u of users) {
        const tr = document.createElement('tr');

        const name = cell(nameOf(u));
        // The id is the one thing about a User that never changes, and it is
        // on the row rather than in a hover.
        name.title = u.id;
        // Pressing a name opens that User whole.
        name.style.cursor = 'pointer';
        name.addEventListener('click', () => { openUserElement(u); });
        tr.appendChild(name);
        tr.appendChild(cell(u.level));
        tr.appendChild(cell(u.namespace || '—'));
        const reached = reachedBy(u);
        const routes = cell(reached.shown, 'element-time');
        routes.title = reached.whole;
        tr.appendChild(routes);
        tr.appendChild(cell(u.email_addresses?.length ? u.email_addresses.join(', ') : '—'));
        tr.appendChild(cell(u.phone_numbers?.length ? u.phone_numbers.join(', ') : '—'));
        tr.appendChild(cell(fmt(u.created_at), 'element-time'));
        tr.appendChild(statusPill(u));

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

/** An open invitation, as the would-be User it is. Pressing the name opens
 *  it, with its cancel. */
function invitationRow(inv: InvitationRecord): HTMLTableRowElement {
    const tr = document.createElement('tr');
    const name = cell(inv.display_name || UNNAMED);
    name.title = inv.id;
    name.style.cursor = 'pointer';
    name.addEventListener('click', () => { openInvitationCancel(inv.id); });
    tr.appendChild(name);
    tr.appendChild(cell('invited'));
    tr.appendChild(cell('—'));
    const routes = cell(inv.accounts.map(a => a.provider).join(', '), 'element-time');
    routes.title = inv.accounts.map(a => `${a.provider}: ${a.account}`).join('\n');
    tr.appendChild(routes);
    tr.appendChild(cell(inv.email));
    tr.appendChild(cell('—'));
    tr.appendChild(cell(fmt(inv.created_at), 'element-time'));
    const status = document.createElement('td');
    const pill = document.createElement('span');
    pill.className = 'element-pill';
    pill.textContent = 'invited';
    status.appendChild(pill);
    tr.appendChild(status);
    return tr;
}

/** The invitations ROOT sent. A node that will not list them still lists its
 *  Users. */
async function fetchInvitations(): Promise<InvitationRecord[]> {
    try {
        return await apiJson<InvitationRecord[]>('/auth/invitations');
    } catch (err: unknown) {
        log.error(SEG.UI, '[UsersElement] the node did not list its invitations', err);
        return [];
    }
}

async function refreshList(container: HTMLElement): Promise<void> {
    const [users, invitations] = await Promise.all([fetchUsers(), fetchInvitations()]);
    renderList(container, users, invitations);
}

// "as root, i press the + and i can create a new user, like how i would create a new oauth token"

/** Exported for tests: the way to the invite element, the + Access Tokens has. */
export function renderInviteLink(container: HTMLElement, invited?: () => void): void {
    container.innerHTML = '';
    container.style.padding = '8px 0';

    const invite = createGhostButton('+', async () => {
        openUserInviteElement(invited);
    });
    invite.element.title = 'invite a user';
    invite.element.setAttribute('aria-label', 'Invite a user');
    invite.element.style.fontSize = '16px';
    invite.element.style.lineHeight = '1';
    invite.element.style.padding = '4px 10px';
    container.appendChild(invite.element);

    // Inviting is a session's act, the way switching a person is.
    person().then(who => {
        if (who.via !== 'token') return;
        const why = 'only a session invites a user, and this page reaches the node as a token';
        invite.setDisabled(true, why);
        container.title = why;
    }).catch((err: unknown) => {
        log.error(SEG.UI, '[UsersElement] the node did not say who is looking', err);
    });
}

export function createUsersElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Users',
        symbol: '⚇',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'users-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'users-list';
            listContainer.innerHTML = '<div class="element-loading">Loading Users…</div>';

            const inviteContainer = document.createElement('div');
            inviteContainer.className = 'users-invite-link';
            renderInviteLink(inviteContainer, () => { void refreshList(listContainer); });

            content.appendChild(inviteContainer);
            content.appendChild(listContainer);

            refreshList(listContainer).catch((err: unknown) => {
                log.error(SEG.UI, '[UsersElement] the node did not list its Users', err);
                listContainer.innerHTML = '';
                const message = `the node did not list its Users: ${err instanceof Error ? err.message : String(err)}`;
                const errBox = document.createElement('div');
                errBox.className = 'element-error';
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

/** Opens the Users element. Called from the Self element. */
export function openUsersElement(): void {
    tray.open(ELEMENT_ID);
}
