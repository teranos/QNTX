/**
 * ⍟ — who is looking.
 *
 * `sym/symbols.go` calls `i` "Self — Your vantage point into QNTX". The
 * vantage is a person's, and this draws that person and nothing else: what the
 * node calls them, how they got in, where they act, and what is joined to
 * them. What the node itself is went to ≡, which is the sibling `am` names.
 *
 * The person is answered to everyone admitted (`/i/` of ROOT SUPER TOKEN
 * ATTESTOR PUBLIC_REGISTRATION), which is a wider reach than anything ≡ draws.
 */

import { apiFetch } from './client';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { createGhostButton } from './components/button.ts';
import { person, personSection, personSwitch, type Person } from './self-person.ts';
import { openTokensGlyph } from './tokens-glyph.ts';
import { openUsersGlyph } from './users-glyph.ts';
import { openMarketGlyph } from './market-glyph.ts';

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
        iElement.innerHTML = '<div class="glyph-loading">Asking the node who is looking...</div>';
        return;
    }

    const sections: string[] = [];

    sections.push(personSection(iPerson, iPersonRefusal));

    // The person's own key, beside the person rather than beside the node's.
    if (iOwnerDID !== null) {
        const value = iOwnerDID
            ? `<span class="glyph-did">${escapeHtml(iOwnerDID)}</span>`
            : `<span class="glyph-unwell">${iRegistered ? '⚠ passkey registered, no identity established' : 'no passkey registered'}</span>`;
        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">Identity</h3>
                <div class="glyph-row">
                    <span class="glyph-label">You:</span>
                    <span class="glyph-value">${value}</span>
                </div>
            </div>
        `);
    }

    iElement.innerHTML = `
        <div class="glyph-content">
            ${sections.join('\n')}
        </div>
    `;

    const actions = document.createElement('div');
    actions.className = 'glyph-actions';

    // Entry point to the Access Tokens glyph (ADR-025).
    const tokensBtn = createGhostButton('⚿ Access Tokens', async () => {
        openTokensGlyph();
    });
    actions.appendChild(tokensBtn.element);

    // Every User is ROOT's to see and to switch (ADR-031). The table refuses
    // anyone else at /auth/users, so nobody else is offered the way there.
    if (iPerson?.level === 'ROOT') {
        const usersBtn = createGhostButton('⚇ Users', async () => {
            openUsersGlyph();
        });
        actions.appendChild(usersBtn.element);
        // Stands are ROOT's to create and delete (ADR-035).
        const marketBtn = createGhostButton('⛬ Stands', async () => {
            openMarketGlyph();
        });
        actions.appendChild(marketBtn.element);
    }

    // The switch on the person (ADR-031), once the node has said who is looking.
    if (iPersonAsked) {
        const flip = personSwitch(iPerson, iPersonRefusal, loadPerson);
        if (flip) actions.appendChild(flip);
    }

    iElement.appendChild(actions);
}

/** ⍟ in the tray. The id is the word, and migration 061 renamed what was written down. */
export function createIGlyph() {
    return {
        id: 'i-glyph',
        title: 'i',
        symbol: '⍟',
        renderContent: () => {
            const content = document.createElement('div');
            iElement = content;
            renderI();
            if (iOwnerDID === null) void loadOwnerDID();
            if (!iPersonAsked) void loadPerson();
            return content;
        },
        initialWidth: '450px',
        initialHeight: '320px',
    };
}
