/**
 * The invitation a friend arrived with (ADR-031): the link ROOT's invitation
 * mailed them, held across the provider's round trip.
 */

// "The friend opens the mail, clicks the link"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

import { apiFetch } from './client';
import type { ProviderDescription } from './ceremony';
import { log, SEG } from './logger';

const PARAM = 'invitation';
const KEY = 'qntx_invitation';

/** What the link signs in with: each provider, and the account ROOT named there. */
export interface Invited {
    token: string;
    accounts: { provider: string; account: string }[];
}

/**
 * The invitation token this tab holds. Taken off the address the first time,
 * and kept for the tab, because a provider sends the browser back without it.
 */
export function heldInvitation(): string {
    const url = new URL(window.location.href);
    const fresh = url.searchParams.get(PARAM);
    if (fresh) {
        try {
            window.sessionStorage.setItem(KEY, fresh);
        } catch (err: unknown) {
            log.warn(SEG.UI, '[Invitation] this tab cannot keep the invitation across the provider:', err);
        }
        url.searchParams.delete(PARAM);
        window.history.replaceState(null, '', url.toString());
        return fresh;
    }
    try {
        return window.sessionStorage.getItem(KEY) ?? '';
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Invitation] this tab cannot read a kept invitation:', err);
        return '';
    }
}

/** The invitation is spent, or refused: the tab holds it no longer. */
export function letGoOfInvitation(): void {
    try {
        window.sessionStorage.removeItem(KEY);
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Invitation] the kept invitation was not dropped:', err);
    }
}

/** What the node says the link signs in with. Throws what the node said when
 *  it is cancelled, used, or not one. */
export async function invited(token: string): Promise<Invited> {
    const response = await apiFetch(`/auth/invitations/${encodeURIComponent(token)}`);
    if (!response.ok) {
        const said = (await response.text()).trim();
        throw new Error(said || `the node did not answer for this invitation (${response.status} ${response.statusText})`);
    }
    const { accounts } = await response.json() as Pick<Invited, 'accounts'>;
    return { token, accounts };
}

/** The providers the invitation signs in with, out of what the node offers. */
export function onlyInvited(providers: ProviderDescription[], inv: Invited): ProviderDescription[] {
    return providers.filter(p => inv.accounts.some(a => a.provider === p.id));
}

/** What the door says the link signs in with. */
export function signsInWith(inv: Invited): string {
    return inv.accounts.map(a => `${a.provider} as ${a.account}`).join(' or ');
}
