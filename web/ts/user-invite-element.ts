/**
 * Invite User element — ROOT inviting a friend (ADR-031).
 * Split out of the Users element the way minting is split out of Access Tokens:
 * surveying everyone and inviting one person are different acts.
 */

// "As ROOT i send an invite link to a friend, i enter their e-mail address"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

// "and dont we actually have logo's for them"

// "and i want to set Name"

// "if both google and apple, then we set both, and  if set then we set the mail address of that provider"

// "or the username"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createDangerButton, createPrimaryButton } from './components/button';
import { fetchProviders, type ProviderDescription } from './ceremony';
import { providerMark } from './provider-marks';
import { log, SEG } from './logger';

const ELEMENT_ID = 'user-invite-element';

// The Users element that opened this hears about the invitation rather than
// carrying a button that asks you to notice.
let onInvited: (() => void) | undefined;

/** One provider the friend may sign in with, and their account there. */
export interface InvitationAccount {
    provider: string;
    account: string;
}

/** What an invitation sends to the node. */
export interface InvitationBody {
    display_name: string;
    email: string;
    accounts: InvitationAccount[];
}

async function sendInvitation(body: InvitationBody): Promise<void> {
    await apiJson<unknown>('/auth/invitations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // The page this is sent from is where the links open, a branch's included.
        body: JSON.stringify({ ...body, page: window.location.origin + window.location.pathname }),
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

// A label activates its first control when anything inside it is pressed, so
// a group of buttons is captioned in a div: in a label, pressing GitHub also
// pressed Google.
function labelled(text: string, field: HTMLElement, tag: 'label' | 'div' = 'label'): HTMLElement {
    const wrap = document.createElement(tag);
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '4px';
    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.textContent = text;
    wrap.append(caption, field);
    return wrap;
}

/** A provider's toggle: its mark and its name, pressed or not. */
function toggle(p: ProviderDescription, onPress: () => void): HTMLButtonElement {
    const button = styled(document.createElement('button'));
    button.type = 'button';
    button.dataset.provider = p.id;
    button.setAttribute('aria-pressed', 'false');
    button.style.display = 'flex';
    button.style.alignItems = 'center';
    button.style.gap = '8px';
    button.style.cursor = 'pointer';
    button.style.textAlign = 'left';
    const mark = providerMark(p.id);
    if (mark) {
        mark.setAttribute('width', '16');
        mark.setAttribute('height', '16');
        button.appendChild(mark);
    }
    const name = document.createElement('span');
    name.textContent = p.label;
    button.appendChild(name);
    button.addEventListener('click', onPress);
    return button;
}

/** One pressed provider's account: the field, and the offer to use the
 *  invitation mail as it. */
interface AccountRow {
    provider: ProviderDescription;
    element: HTMLElement;
    input: HTMLInputElement;
    same: HTMLInputElement;
}

function accountRow(p: ProviderDescription, email: HTMLInputElement): AccountRow {
    const input = textField(`account-${p.id}`, 'text', 'their address or username there');
    const same = document.createElement('input');
    same.type = 'checkbox';
    same.name = `same-${p.id}`;
    const sameRow = document.createElement('label');
    sameRow.style.display = 'flex';
    sameRow.style.gap = '6px';
    sameRow.style.alignItems = 'center';
    const sameText = document.createElement('span');
    sameText.textContent = 'the invitation mail';
    sameRow.append(same, sameText);
    same.addEventListener('change', () => {
        if (same.checked) input.value = email.value.trim();
        input.disabled = same.checked;
    });
    email.addEventListener('input', () => {
        if (same.checked) input.value = email.value.trim();
    });
    // A div: the field and the checkbox are two controls, and in one label
    // pressing the checkbox's text would land in the field.
    const element = labelled(`${p.label} account`, input, 'div');
    element.appendChild(sameRow);
    return { provider: p, element, input, same };
}

/** Exported for tests: the invite form, drawn into a container. `send` is
 *  what an invitation goes out through. */
export function renderInvite(
    content: HTMLElement,
    providers: ProviderDescription[],
    send: (body: InvitationBody) => Promise<void> = sendInvitation,
): void {
    content.className = 'user-invite-content';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '10px';
    content.style.padding = '12px';
    content.style.fontFamily = 'var(--font-mono)';

    const name = textField('display_name', 'text', 'what to call them');
    const email = textField('email', 'email', 'friend@example.com');

    // In the order the node lists the providers, whichever were pressed.
    const accounts = document.createElement('div');
    accounts.style.display = 'flex';
    accounts.style.flexDirection = 'column';
    accounts.style.gap = '10px';
    const pressed = new Map<string, AccountRow>();

    const toggles = document.createElement('div');
    toggles.style.display = 'flex';
    toggles.style.flexWrap = 'wrap';
    toggles.style.gap = '4px';
    for (const p of providers) {
        const button = toggle(p, () => {
            const on = !pressed.has(p.id);
            button.setAttribute('aria-pressed', on ? 'true' : 'false');
            button.style.borderColor = on ? 'var(--text-on-dark)' : 'var(--border-on-dark)';
            if (on) {
                pressed.set(p.id, accountRow(p, email));
            } else {
                pressed.get(p.id)?.element.remove();
                pressed.delete(p.id);
            }
            accounts.replaceChildren(...providers.filter(q => pressed.has(q.id)).map(q => pressed.get(q.id)!.element));
        });
        toggles.appendChild(button);
    }

    // What the node said, beside the button rather than on top of it.
    const said = document.createElement('div');
    said.className = 'invite-said';
    said.style.wordBreak = 'break-word';
    said.style.overflowWrap = 'break-word';

    const sendButton = createPrimaryButton('Send invitation', async () => {
        said.textContent = '';
        const address = email.value.trim();
        if (!address) throw new Error('no invitation mail');
        const rows = providers.filter(p => pressed.has(p.id)).map(p => pressed.get(p.id)!);
        if (rows.length === 0) throw new Error('no provider pressed');
        const named: InvitationAccount[] = [];
        for (const row of rows) {
            const at = row.input.value.trim();
            if (!at) throw new Error(`no ${row.provider.label} account`);
            named.push({ provider: row.provider.id, account: at });
        }
        await send({ display_name: name.value.trim(), email: address, accounts: named });
        said.textContent = `invitation sent to ${address}`;
        onInvited?.();
    });

    content.append(
        labelled('Name', name),
        labelled('Invitation mail', email),
        labelled('Signs in with', toggles, 'div'),
        accounts,
        sendButton.element,
        said,
    );
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
    display_name?: string;
    accounts: InvitationAccount[];
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

    const who = inv.display_name ? `${inv.display_name} (${inv.email})` : inv.email;
    const signsIn = inv.accounts.map(a => `${a.provider} as ${a.account}`).join(' or ');
    const what = document.createElement('div');
    what.style.wordBreak = 'break-word';
    what.textContent = `Invitation to ${who}, to sign in with ${signsIn}`;
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
