/**
 * Invite User element — ROOT inviting a friend (ADR-031).
 * Split out of the Users element the way minting is split out of Access Tokens:
 * surveying everyone and inviting one person are different acts.
 */

// "As ROOT i send an invite link to a friend, i enter their e-mail address"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createDangerButton, createPrimaryButton } from './components/button';
import { fetchProviders, type ProviderDescription } from './ceremony';
import { log, SEG } from './logger';

const ELEMENT_ID = 'user-invite-element';

// The Users element that opened this hears about the invitation rather than
// carrying a button that asks you to notice.
let onInvited: (() => void) | undefined;

async function sendInvitation(email: string, provider: string, account: string): Promise<void> {
    await apiJson<unknown>('/auth/invitations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, provider, account }),
    });
}

function styled<T extends HTMLElement>(field: T): T {
    field.style.padding = '6px 8px';
    field.style.fontFamily = 'var(--font-mono)';
    field.style.color = 'var(--text-on-dark)';
    field.style.background = 'var(--bg-dark-light)';
    field.style.border = '1px solid var(--border-on-dark)';
    field.style.borderRadius = 'var(--border-radius)';
    return field;
}

function textField(name: string, type: string, placeholder: string): HTMLInputElement {
    const input = styled(document.createElement('input'));
    input.name = name;
    input.type = type;
    input.placeholder = placeholder;
    input.size = 28;
    return input;
}

function labelled(text: string, field: HTMLElement): HTMLLabelElement {
    const wrap = document.createElement('label');
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '4px';
    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.textContent = text;
    wrap.append(caption, field);
    return wrap;
}

/** Which provider the friend signs in with: the pressed row, none until one is. */
function providerRows(providers: ProviderDescription[], onPress: (p: ProviderDescription) => void): HTMLElement {
    const group = document.createElement('div');
    group.setAttribute('role', 'radiogroup');
    group.style.display = 'flex';
    group.style.flexDirection = 'column';
    group.style.gap = '4px';

    const rows: HTMLButtonElement[] = [];
    for (const p of providers) {
        const row = styled(document.createElement('button'));
        row.type = 'button';
        row.setAttribute('role', 'radio');
        row.setAttribute('aria-checked', 'false');
        row.dataset.provider = p.id;
        row.textContent = p.label;
        row.style.textAlign = 'left';
        row.style.cursor = 'pointer';
        row.addEventListener('click', () => {
            for (const other of rows) {
                const isThis = other === row;
                other.setAttribute('aria-checked', isThis ? 'true' : 'false');
                other.style.borderColor = isThis ? 'var(--text-on-dark)' : 'var(--border-on-dark)';
            }
            onPress(p);
        });
        rows.push(row);
        group.appendChild(row);
    }
    return group;
}

/** Exported for tests: the invite form, drawn into a container. */
export function renderInvite(content: HTMLElement, providers: ProviderDescription[]): void {
    content.className = 'user-invite-content';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '10px';
    content.style.padding = '12px';
    content.style.fontFamily = 'var(--font-mono)';

    let pressed: ProviderDescription | undefined;

    const email = textField('email', 'email', 'friend@example.com');
    const account = textField('account', 'text', 'their account at the provider');

    // "the userstory just shows ROOT the option to alo pick that mail address as the mail of the google identity"
    const same = document.createElement('input');
    same.type = 'checkbox';
    same.name = 'same';
    const sameText = document.createElement('span');
    const sameRow = document.createElement('label');
    sameRow.style.display = 'flex';
    sameRow.style.gap = '6px';
    sameRow.style.alignItems = 'center';
    sameRow.append(same, sameText);

    const showSame = () => {
        const address = email.value.trim();
        sameRow.hidden = !pressed || !address;
        if (pressed) sameText.textContent = `${address} is their ${pressed.label} account`;
        if (same.checked) account.value = address;
        account.disabled = same.checked;
    };
    same.addEventListener('change', showSame);
    email.addEventListener('input', showSame);

    const rows = providerRows(providers, p => { pressed = p; showSame(); });

    // What the node said, beside the button rather than on top of it.
    const said = document.createElement('div');
    said.className = 'invite-said';
    said.style.wordBreak = 'break-word';
    said.style.overflowWrap = 'break-word';

    const send = createPrimaryButton('Send invitation', async () => {
        said.textContent = '';
        const address = email.value.trim();
        if (!address) throw new Error('no e-mail address');
        if (!pressed) throw new Error('no provider');
        const at = account.value.trim();
        if (!at) throw new Error(`no ${pressed.label} account`);
        await sendInvitation(address, pressed.id, at);
        email.value = '';
        account.value = '';
        same.checked = false;
        showSame();
        said.textContent = `invitation sent to ${address}`;
        onInvited?.();
    });

    content.append(
        labelled('E-mail address', email),
        labelled('Signs in with', rows),
        sameRow,
        labelled('Account', account),
        send.element,
        said,
    );
    showSame();
}

function inviteElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Invite User',
        symbol: '⚇',
        onClose: () => { tray.remove(ELEMENT_ID); },
        renderContent: () => {
            const content = document.createElement('div');
            content.innerHTML = '<div class="element-loading">reading providers…</div>';
            fetchProviders().then(providers => {
                content.innerHTML = '';
                renderInvite(content, providers);
            }).catch((err: unknown) => {
                log.error(SEG.UI, '[UserInviteElement] the node did not list its providers', err);
                content.innerHTML = '';
                const errBox = document.createElement('div');
                errBox.className = 'element-error';
                errBox.textContent = err instanceof Error ? err.message : String(err);
                content.appendChild(errBox);
            });
            return content;
        },
    };
}

// "the MAIL ROOT received has a button for cancelling the invitation"

/** One invitation, as the node lists it. */
export interface InvitationRecord {
    id: string;
    email: string;
    provider: string;
    account: string;
    invited_by: string;
    created_at: number;
    cancelled_at?: number;
    accepted_by?: string;
    accepted_at?: number;
}

const CANCEL_ID = 'invitation-cancel-element';

/** Exported for tests: the invitation the mail's button names, and its cancel
 *  while it is open. */
export function renderCancel(content: HTMLElement, inv: InvitationRecord): void {
    content.innerHTML = '';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '10px';
    content.style.padding = '12px';
    content.style.fontFamily = 'var(--font-mono)';

    const what = document.createElement('div');
    what.style.wordBreak = 'break-word';
    what.textContent = `Invitation to ${inv.email}, to sign in with ${inv.provider} as ${inv.account}`;
    content.appendChild(what);

    const state = document.createElement('div');
    if (inv.accepted_by) {
        state.textContent = `already accepted by ${inv.accepted_by}`;
        content.appendChild(state);
        return;
    }
    if (inv.cancelled_at) {
        state.textContent = 'already cancelled';
        content.appendChild(state);
        return;
    }
    const cancel = createDangerButton('Cancel the invitation', 'Confirm cancel', async () => {
        await apiJson<unknown>(`/auth/invitations/${encodeURIComponent(inv.id)}/cancel`, { method: 'POST' });
        renderCancel(content, { ...inv, cancelled_at: Date.now() });
    });
    content.appendChild(cancel.element);
}

/** Opens the cancel for the invitation the mail's button names. */
export function openInvitationCancel(id: string): void {
    if (tray.has(CANCEL_ID)) tray.remove(CANCEL_ID);
    tray.add({
        id: CANCEL_ID,
        title: 'Invitation',
        symbol: '⚇',
        onClose: () => { tray.remove(CANCEL_ID); },
        renderContent: () => {
            const content = document.createElement('div');
            content.innerHTML = '<div class="element-loading">reading the invitation…</div>';
            apiJson<InvitationRecord[]>('/auth/invitations').then(held => {
                const inv = held.find(i => i.id === id);
                if (!inv) throw new Error(`the node holds no invitation ${id}`);
                renderCancel(content, inv);
            }).catch((err: unknown) => {
                log.error(SEG.UI, '[InvitationCancel] the invitation was not read', err);
                content.innerHTML = '';
                const errBox = document.createElement('div');
                errBox.className = 'element-error';
                errBox.textContent = err instanceof Error ? err.message : String(err);
                content.appendChild(errBox);
            });
            return content;
        },
    });
    tray.open(CANCEL_ID);
}

/**
 * Opens the invite element, from the Users element. Built on the way in and
 * removed on close, the way the mint element is.
 */
export function openUserInviteElement(invited?: () => void): void {
    onInvited = invited;
    if (!tray.has(ELEMENT_ID)) {
        tray.add(inviteElement());
    }
    tray.open(ELEMENT_ID);
}
