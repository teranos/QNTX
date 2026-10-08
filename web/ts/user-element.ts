/**
 * User Element — one User, whole (ADR-031), opened from Users.
 */

// "can i set this in qntx ? how about a user element accesible from users element"

// The record as the node holds it. A person adds to their own record here the
// way they do at arrival — an email address, a phone number — through the same
// POST /auth/user/arrive, which writes to the User the session belongs to and
// nobody else's. The first address is the one mail goes to (ADR-041).

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { Button, createDangerButton, createPrimaryButton } from './components/button';
import { log, SEG } from './logger';
import { person } from './self-person';
import type { UserRecord } from './users-element';

/** The ROOT User is root until they say otherwise (ADR-031). */
function nameOf(u: UserRecord): string {
    if (u.display_name) return u.display_name;
    if (u.level === 'ROOT') return 'root';
    return '—';
}

/** created_at is milliseconds, the way the node writes it. */
function when(ms: number): string {
    if (!ms) return '—';
    return new Date(ms).toISOString().slice(0, 19).replace('T', ' ');
}

function row(label: string, value: string): HTMLTableRowElement {
    const tr = document.createElement('tr');
    const th = document.createElement('th');
    th.textContent = label;
    th.style.textAlign = 'left';
    th.style.paddingRight = '12px';
    th.style.verticalAlign = 'top';
    const td = document.createElement('td');
    td.textContent = value;
    td.style.overflowWrap = 'break-word';
    tr.appendChild(th);
    tr.appendChild(td);
    return tr;
}

/** Exported for tests: the addresses, the primary one named as such. */
export function addressesOf(u: UserRecord): string {
    const addresses = u.email_addresses ?? [];
    if (addresses.length === 0) return '— (no mail reaches this User)';
    return addresses.map((a, i) => (i === 0 ? `${a} (primary)` : a)).join('\n');
}

/** Adds to one's own record, as arrival does. The node's refusal is the error. */
async function arrive(email: string, phone: string): Promise<void> {
    await apiJson('/auth/user/arrive', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, phone }),
    });
}

/** What adding looks like on one's own record. */
function addForm(onAdded: () => void): HTMLDivElement {
    const form = document.createElement('div');
    form.className = 'element-actions';
    form.style.flexWrap = 'wrap';
    const email = document.createElement('input');
    email.type = 'email';
    email.placeholder = 'email address';
    email.className = 'user-add-email';
    const phone = document.createElement('input');
    phone.type = 'tel';
    phone.placeholder = 'phone number';
    phone.className = 'user-add-phone';
    const add = createPrimaryButton('Add to my record', async () => {
        const typedEmail = email.value.trim();
        const typedPhone = phone.value.trim();
        if (!typedEmail && !typedPhone) {
            throw new Error('nothing typed, so nothing is added');
        }
        await arrive(typedEmail, typedPhone);
        email.value = '';
        phone.value = '';
        onAdded();
    });
    form.appendChild(email);
    form.appendChild(phone);
    form.appendChild(add.element);
    return form;
}

/** Flips the switch on one User. The node's refusal is the error. */
async function flip(id: string, verb: 'disable' | 'enable'): Promise<void> {
    await apiJson<{ status: string }>(`/auth/users/${encodeURIComponent(id)}/${verb}`, { method: 'POST' });
}

// "the infinite row of switch of is pissing me off as well, should be in the User themselves"

/** The switch on this User: off carries the switcher's name, on undoes it. */
function switchOn(u: UserRecord, onSwitched: () => void): HTMLElement {
    const actions = document.createElement('div');
    actions.className = 'element-actions';
    if (u.disabled_by) {
        const on = createPrimaryButton('Switch on', async () => {
            await flip(u.id, 'enable');
            onSwitched();
        });
        actions.appendChild(on.element);
        return actions;
    }
    const off = createDangerButton('Switch off', 'Confirm switch off', async () => {
        await flip(u.id, 'disable');
        onSwitched();
    });
    actions.appendChild(off.element);
    return actions;
}

// "and to become or unbecome, is actually in the specific User in the Users Element, the i element is to get back to ROOT"

/** ROOT becomes this User (ADR-031), and the page is read again as them. */
async function become(u: UserRecord): Promise<void> {
    await apiJson(`/auth/users/${encodeURIComponent(u.id)}/become`, { method: 'POST' });
    window.location.reload();
}

/** Exported for tests: whether ROOT is offered to become this User. */
export function becomable(u: UserRecord, viewerIsRoot: boolean): boolean {
    return viewerIsRoot && u.level !== 'ROOT' && !u.disabled_by;
}

/**
 * Exported for tests: one User's record. `own` is whether the viewer is this
 * User in a session, the one case the node takes an addition from. `switches`
 * is whether the viewer may switch a User: a session may, a token may not.
 * `viewerIsRoot` is whether ROOT is looking, who may become this User.
 */
export function renderUser(container: HTMLElement, u: UserRecord, own: boolean, onAdded: () => void = () => {}, switches = false, viewerIsRoot = false): void {
    container.innerHTML = '';

    const table = document.createElement('table');
    table.className = 'user-record';
    table.style.borderCollapse = 'collapse';
    table.style.whiteSpace = 'pre-line';
    table.appendChild(row('Name', nameOf(u)));
    table.appendChild(row('Id', u.id));
    table.appendChild(row('Level', u.level));
    table.appendChild(row('Door', u.namespace || '—'));
    table.appendChild(row('Email', addressesOf(u)));
    table.appendChild(row('Phone', u.phone_numbers?.length ? u.phone_numbers.join('\n') : '—'));
    table.appendChild(row('Accounts', (u.accounts ?? []).map(a => `${a.provider || 'account'}: ${a.handle || a.canonical_id}`).join('\n') || '—'));
    table.appendChild(row('Keys', (u.keys ?? []).map(k => `${k.origin.toLowerCase()} ${k.did}`).join('\n') || '—'));
    table.appendChild(row('Created', when(u.created_at)));
    table.appendChild(row('Created by', u.created_by || '—'));
    table.appendChild(row('Status', u.disabled_by ? `off, by ${u.disabled_by}` : 'on'));
    container.appendChild(table);

    if (switches) {
        container.appendChild(switchOn(u, onAdded));
    }

    if (becomable(u, viewerIsRoot)) {
        const actions = document.createElement('div');
        actions.className = 'element-actions';
        actions.appendChild(new Button({
            label: `Become ${nameOf(u)}`,
            variant: 'danger',
            confirmation: { label: `Their sessions end, and their tokens stop until you are ROOT again` },
            onClick: () => become(u),
        }).element);
        container.appendChild(actions);
    }

    if (own) {
        container.appendChild(addForm(onAdded));
        return;
    }
    const note = document.createElement('div');
    note.className = 'element-loading';
    note.textContent = 'Only this User adds to their own record, from their own session.';
    container.appendChild(note);
}

async function redraw(container: HTMLElement, id: string): Promise<void> {
    const [users, who] = await Promise.all([apiJson<UserRecord[]>('/auth/users'), person()]);
    const u = users.find(candidate => candidate.id === id);
    if (!u) {
        throw new Error(`the node holds no User ${id}`);
    }
    renderUser(container, u, who.user === id && who.via === 'session', () => {
        redraw(container, id).catch((err: unknown) => refused(container, err));
    }, who.via !== 'token', who.level === 'ROOT');
}

function refused(container: HTMLElement, err: unknown): void {
    const message = `the node did not show this User: ${err instanceof Error ? err.message : String(err)}`;
    log.error(SEG.UI, `[UserElement] ${message}`, err);
    container.innerHTML = '';
    const box = document.createElement('div');
    box.className = 'element-error';
    box.textContent = message;
    container.appendChild(box);
}

/** Opens one User as its own element. Called from Users. */
export function openUserElement(u: UserRecord): void {
    const elementId = `user-element-${u.id}`;
    if (tray.has(elementId)) {
        tray.open(elementId);
        return;
    }

    tray.add({
        id: elementId,
        title: nameOf(u),
        symbol: '⚇',
        onClose: () => { tray.remove(elementId); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'user-element-content';
            content.style.padding = '12px';
            content.innerHTML = '<div class="element-loading">Loading User…</div>';
            redraw(content, u.id).catch((err: unknown) => refused(content, err));
            return content;
        },
    } satisfies Element);

    tray.open(elementId);
}
