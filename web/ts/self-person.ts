/**
 * The person, on the glyph that draws the node.
 */

// "the Self glyph has a lot about the node itself, but nothing about the User
// that is logged in"

// /auth/status answers whether somebody is admitted and at what level. This
// asks who, and draws what comes back: what the node calls this person, how
// they got in, the door they registered at, where they act, and the accounts
// joined to them. Nobody logged in gets the node's own refusal instead.

import { apiFetch } from './client';
import { escapeHtml } from './html-utils';

/** One provider account joined to a User. No binding, no token, no secret. */
export interface PersonAccount {
    provider?: string;
    canonical_id?: string;
    handle?: string;
}

/** What the node thinks of whoever is asking. GET /auth/user. */
export interface Person {
    user: string;
    display_name?: string;
    name: string;
    level: string;
    namespaces: string[];
    door?: string;
    identity: string;
    via: string;
    accounts: PersonAccount[];
    keys: string[];
}

/** A User that walked up to no door names none. */
const NO_DOOR = 'no door';

/** Naming no namespace is naming all of them, so the blank has to say which. */
const EVERY_NAMESPACE = 'every namespace this node serves';

/** Nobody has been joined to this User yet. */
const NO_ACCOUNTS = 'none';

/**
 * Asks the node who it thinks is looking.
 *
 * Accept says JSON, because the node answers a refusal in the caller's own
 * terms: without it a browser is sent to the login page and the glyph would
 * draw that instead of what the node said.
 */
export async function person(): Promise<Person> {
    const response = await apiFetch('/auth/user', { headers: { Accept: 'application/json' } });
    if (!response.ok) {
        throw new Error(await refusal(response));
    }
    return await response.json() as Person;
}

/** What the node said when it would not answer. Its words, never softened. */
export async function refusal(response: Response): Promise<string> {
    const body = await response.text().catch((err: unknown) => `(unreadable body: ${err})`);
    try {
        const said = JSON.parse(body) as { error?: string };
        if (said?.error) return said.error;
    } catch (err: unknown) {
        // Not JSON, so the body is the whole of what the node said.
        void err;
    }
    return body.trim() || `${response.status} ${response.statusText}`;
}

function row(label: string, value: string): string {
    return `
                <div class="glyph-row">
                    <span class="glyph-label">${escapeHtml(label)}</span>
                    <span class="glyph-value">${escapeHtml(value)}</span>
                </div>`;
}

/** How an account reads: what it calls itself, and what the provider calls it. */
function accountValue(account: PersonAccount): string {
    const handle = account.handle ?? '';
    const canonical = account.canonical_id ?? '';
    if (handle && canonical && handle !== canonical) return `${handle} — ${canonical}`;
    return handle || canonical;
}

/**
 * The section. `who` is what the node answered; `refused` is what it said
 * instead. Neither is nothing asked yet, which draws nothing at all.
 */
export function personSection(who: Person | null, refused: string): string {
    if (!who) {
        if (!refused) return '';
        return `
            <div class="glyph-section">
                <h3 class="glyph-section-title">Who you are</h3>
                <div class="glyph-row">
                    <span class="glyph-value" style="color: #fbbf24;">${escapeHtml(refused)}</span>
                </div>
            </div>
        `;
    }

    const accounts = who.accounts.length === 0
        ? row('Accounts:', NO_ACCOUNTS)
        : who.accounts.map(account => row(`${account.provider ?? ''}:`, accountValue(account))).join('');

    return `
            <div class="glyph-section">
                <h3 class="glyph-section-title">Who you are</h3>
                ${row('Name:', who.name || who.user)}
                ${row('Level:', who.level)}
                ${row('Via:', who.via)}
                ${row('Route:', who.identity)}
                ${row('Door:', who.door || NO_DOOR)}
                ${row('Namespace:', who.namespaces.join(', ') || EVERY_NAMESPACE)}
                ${accounts}
            </div>
        `;
}
