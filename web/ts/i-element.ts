/**
 * ⍟ — who is looking.
 *
 * `sym/README.md` calls `i` "Self — Your vantage point into QNTX". The
 * vantage is a person's, and this draws that person and nothing else: what the
 * node calls them, how they got in, where they act, and what is joined to
 * them. What the node itself is went to ≡, which is the sibling `am` names.
 *
 * The person is answered to everyone admitted (`/i/` in server/reach), which
 * is a wider reach than anything ≡ draws.
 */

import { apiFetch } from './client';
import { I } from './sym';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { person, personSection, personSwitch, personUnbecome, type Person } from './self-person.ts';
import { tray, pressFrom } from '@teranos/elements';
import { openTokensElement, ELEMENT_ID as TOKENS_ID } from './tokens-element.ts';
import { openRolesElement, ELEMENT_ID as ROLES_ID } from './roles-element.ts';
import { openUsersElement, ELEMENT_ID as USERS_ID } from './users-element.ts';
import { openMarketElement, ELEMENT_ID as MARKET_ID } from './market-element.ts';
import { openMailElement, ELEMENT_ID as MAIL_ID } from './mail-element.ts';
import { openGitHubElement, ELEMENT_ID as GITHUB_ID } from './github-element.ts';
import { openApprovalsElement, ELEMENT_ID as APPROVALS_ID } from './approvals-element.ts';

// The elements opened from ⍟: Access Tokens (ADR-025), Users (ADR-031), Roles
// (ADR-034), Stands (ADR-035), Mail (ADR-041), GitHub (ADR-043), Approvals (ADR-052).
const WAYS = [
    { id: TOKENS_ID, label: '⚿ Access Tokens', open: openTokensElement },
    { id: USERS_ID, label: '⚇ Users', open: openUsersElement },
    { id: ROLES_ID, label: '⚙ Roles', open: openRolesElement },
    { id: MARKET_ID, label: '⛬ Stands', open: openMarketElement },
    { id: MAIL_ID, label: '✉ Mail', open: openMailElement },
    { id: GITHUB_ID, label: '⎇ GitHub', open: openGitHubElement },
    { id: APPROVALS_ID, label: '✓ Approvals', open: openApprovalsElement },
];

// Who the node thinks is looking, and what it said instead when it would not
// say. Both empty is nothing asked yet, which draws no section at all.
let iElement: HTMLElement | null = null;
let iPerson: Person | null = null;
let iPersonRefusal = '';
let iPersonAsked = false;
// The owner DID, empty until a passkey establishes one (#577). Empty is shown
// as such rather than hidden, so an unestablished identity is visible.
let iOwnerDID: string | null = null;
let iRegistered = false;

async function loadOwnerDID(): Promise<void> {
    try {
        const response = await apiFetch('/auth/status');
        if (!response.ok) return;
        const status = await response.json();
        iOwnerDID = typeof status?.owner_did === 'string' ? status.owner_did : '';
        iRegistered = status?.registered === true;
        if (iElement) renderI();
    } catch (error: unknown) {
        log.warn(SEG.SELF, `[i] auth status fetch failed: ${error instanceof Error ? error.message : String(error)}`);
    }
}

// A refusal is kept as the node worded it, because that is the answer.
async function loadPerson(): Promise<void> {
    try {
        iPerson = await person();
        iPersonRefusal = '';
    } catch (error: unknown) {
        iPerson = null;
        iPersonRefusal = error instanceof Error ? error.message : String(error);
        log.warn(SEG.SELF, `[i] the node did not say who is looking: ${iPersonRefusal}`);
    }
    iPersonAsked = true;
    if (iElement) renderI();
}

function renderI(): void {
    if (!iElement) return;

    if (!iPersonAsked && iOwnerDID === null) {
        iElement.innerHTML = '<div class="element-loading">Asking the node who is looking...</div>';
        return;
    }

    const sections: string[] = [];

    sections.push(personSection(iPerson, iPersonRefusal));

    // The person's own key, beside the person rather than beside the node's.
    // It is the passkey owner the node holds, which is ROOT's, so ROOT being
    // somebody else is not shown it as theirs.
    if (iOwnerDID !== null && !iPerson?.becoming_by) {
        const value = iOwnerDID
            ? `<span class="element-did">${escapeHtml(iOwnerDID)}</span>`
            : `<span class="status-unwell">${iRegistered ? '⚠ passkey registered, no identity established' : 'no passkey registered'}</span>`;
        sections.push(`
            <div class="element-section">
                <h3 class="element-section-title">Identity</h3>
                <div class="element-row">
                    <span class="label">You:</span>
                    <span class="element-value">${value}</span>
                </div>
            </div>
        `);
    }

    iElement.innerHTML = `
        <div class="element-content">
            ${sections.join('\n')}
        </div>
    `;

    const actions = document.createElement('div');
    actions.className = 'element-actions';

    // "the ui needs to stop suggesting things that arent possible, needs to stop handing out dead birds"
    // A way to an element is offered when the tray holds it, which is when the node said the viewer reaches it.
    for (const way of WAYS) {
        if (!tray.has(way.id)) continue;
        actions.appendChild(pressFrom({ id: `i-way-${way.label}`, title: way.label, does: async () => way.open() }).element);
    }

    // ROOT being somebody gets back to itself here (ADR-031).
    const back = personUnbecome(iPerson);
    if (back) actions.appendChild(back);

    // The switch on the person (ADR-031), once the node has said who is looking.
    // ROOT being somebody does not switch them off.
    if (iPersonAsked && !iPerson?.becoming_by) {
        const flip = personSwitch(iPerson, iPersonRefusal, loadPerson);
        if (flip) actions.appendChild(flip);
    }

    iElement.appendChild(actions);
}

// Who this is for: whoever reaches these (audience.ts).
export const needs = ['/i/'] as const;

/** ⍟ in the tray. The id is the word, and migration 061 renamed what was written down. */
export function createIElement() {
    return {
        id: 'i-element',
        title: 'i',
        symbol: I,
        renderContent: () => {
            const content = document.createElement('div');
            iElement = content;
            renderI();
            if (iOwnerDID === null) void loadOwnerDID();
            if (!iPersonAsked) void loadPerson();
            return content;
        },
    };
}
