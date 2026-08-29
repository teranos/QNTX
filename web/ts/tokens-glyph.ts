/**
 * Access Tokens Glyph — machine-access token management (ADR-025).
 *
 * Plain window (no panel manifestation). Reached from the Self glyph. Lists
 * tokens without raw values, revokes and enables them. Minting one is its own
 * glyph: surveying and creating are different acts.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createButton, createDangerButton, createGhostButton, createPrimaryButton } from './components/button';
import { openTokenMintGlyph } from './token-mint-glyph';
import { openTokenGlyph } from './token-glyph';
import { log, SEG } from './logger';

interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    namespaces: string[];
    scope_read: string[];
    scope_write: string[];
    created_at: string;
    expires_at?: string;
    last_used_at?: string;
    revoked_at?: string;
}

const GLYPH_ID = 'tokens-glyph';

async function fetchTokens(): Promise<TokenInfo[]> {
    return await apiJson<TokenInfo[]>('/auth/tokens');
}


async function revokeToken(id: string): Promise<void> {
    await apiJson<{ status: string }>(`/auth/tokens/${encodeURIComponent(id)}`, {
        method: 'DELETE',
    });
}

/**
 * Lift a revocation (ADR-025). Revocation is a switch: kill the token, watch
 * whether anything is still presenting it, turn it back on if that was you.
 */
async function enableToken(id: string): Promise<void> {
    await apiJson<{ status: string }>(`/auth/tokens/${encodeURIComponent(id)}/enable`, {
        method: 'POST',
    });
}

/** What a scope list says it reaches. Empty is none, and '*' is everything. */
function reach(scope: string[] | undefined): string {
    if (!scope || scope.length === 0) return 'nothing';
    if (scope.includes('*')) return 'everything';
    return scope.join(', ');
}

function fmt(dt: string | undefined): string {
    if (!dt) return '—';
    const d = new Date(dt);
    return isNaN(d.getTime()) ? dt : d.toISOString().slice(0, 19).replace('T', ' ');
}

/**
 * The last eight characters of a DID, the way the door wears the node's own.
 * Sixty characters of base58 took the row past both edges and pushed the label
 * and the status out of the window entirely.
 */
export function shortDID(did: string): string {
    if (!did) return '—';
    return did.slice(-8);
}

/** A cell holding a DID short, with the whole of it a press away. */
function didCell(did: string): HTMLTableCellElement {
    const td = document.createElement('td');
    td.style.padding = '4px 8px';
    td.textContent = shortDID(did);
    if (!did) return td;

    // Nothing is hidden: the value is on the element and one press takes it.
    td.title = did;
    td.style.cursor = 'pointer';
    td.addEventListener('click', (e) => {
        e.stopPropagation();
        void navigator.clipboard.writeText(did).then(
            () => { td.textContent = 'copied'; setTimeout(() => { td.textContent = shortDID(did); }, 1200); },
            () => { td.textContent = 'refused'; setTimeout(() => { td.textContent = shortDID(did); }, 1200); },
        );
    });
    return td;
}

/** Active, revoked or expired, said in the colour it is. */
function statusPill(t: TokenInfo): HTMLTableCellElement {
    const td = document.createElement('td');
    td.style.padding = '4px 8px';

    const pill = document.createElement('span');
    pill.style.padding = '2px 8px';
    pill.style.borderRadius = '10px';
    pill.style.fontSize = '11px';
    pill.style.whiteSpace = 'nowrap';

    if (t.revoked_at) {
        pill.textContent = 'revoked';
        pill.style.color = 'var(--color-error)';
        pill.style.background = 'rgba(201, 88, 79, .16)';
        pill.style.border = '1px solid rgba(201, 88, 79, .4)';
        pill.title = `revoked ${fmt(t.revoked_at)}`;
    } else if (t.expires_at && new Date(t.expires_at) < new Date()) {
        pill.textContent = 'expired';
        pill.style.color = 'var(--color-warning, #fbbf24)';
        pill.style.background = 'rgba(251, 191, 36, .14)';
        pill.style.border = '1px solid rgba(251, 191, 36, .4)';
        pill.title = `expired ${fmt(t.expires_at)}`;
    } else {
        pill.textContent = 'active';
        pill.style.color = 'var(--color-success)';
        pill.style.background = 'rgba(29, 122, 76, .2)';
        pill.style.border = '1px solid var(--door-lamp-dim, #1d7a4c)';
    }

    td.appendChild(pill);
    return td;
}

/** Exported for tests: which control a row offers is the whole point of the
 *  revoked state, and it is not reachable through the async glyph mount. */
export function renderList(container: HTMLElement, tokens: TokenInfo[]): void {
    container.innerHTML = '';

    if (tokens.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No access tokens.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'tokens-table';
    table.style.borderCollapse = 'collapse';
    table.style.fontFamily = 'var(--font-mono)';

    // Dim label above the value, the way the attestation glyph reads.
    const head = 'text-align:left;padding:4px 8px;font-weight:normal;' +
        'color:var(--text-on-dark-tertiary);border-bottom:1px solid var(--border-on-dark);';
    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th style="${head}">Label</th>
        <th style="${head}">For</th>
        <th style="${head}">DID</th>
        <th style="${head}">Namespace</th>
        <th style="${head}">Reads</th>
        <th style="${head}">Writes</th>
        <th style="${head}">Created</th>
        <th style="${head}">Last used</th>
        <th style="${head}">Status</th>
        <th style="${head}"></th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const t of tokens) {
        const tr = document.createElement('tr');

        const label = document.createElement('td');
        label.style.padding = '4px 8px';
        label.textContent = t.label;
        // The label is the way in to the token's own glyph. The row keeps its
        // revoke and enable controls, which are not a way in.
        label.style.cursor = 'pointer';
        label.title = 'press to open this token';
        label.addEventListener('click', () => { openTokenGlyph(t.id, t.label); });
        tr.appendChild(label);

        function cell(text: string): HTMLTableCellElement {
            const td = document.createElement('td');
            td.style.padding = '4px 8px';
            td.style.wordBreak = 'break-word';
            td.style.overflowWrap = 'break-word';
            td.textContent = text;
            return td;
        }

        tr.appendChild(cell(t.minted_by || '—'));

        // The DID is how a token's own attestations are found (?actor=).
        tr.appendChild(didCell(t.did));
        tr.appendChild(cell(t.namespaces?.length ? t.namespaces.join(', ') : '—'));

        // Empty grants nothing, so it reads as "nothing" rather than as blank —
        // a blank cell is what a token with everything would look like too.
        tr.appendChild(cell(reach(t.scope_read)));
        tr.appendChild(cell(reach(t.scope_write)));

        const created = document.createElement('td');
        created.style.padding = '4px 8px';
        created.textContent = fmt(t.created_at);
        tr.appendChild(created);

        const used = document.createElement('td');
        used.style.padding = '4px 8px';
        used.textContent = fmt(t.last_used_at);
        tr.appendChild(used);

        tr.appendChild(statusPill(t));

        const action = document.createElement('td');
        action.style.padding = '4px 8px';
        action.style.textAlign = 'right';
        // The one cell that must not wrap: a button broken across lines is a
        // smaller target than the word it was.
        action.style.whiteSpace = 'nowrap';
        if (t.revoked_at) {
            // Revoked is a state you can leave. Without this the only way back
            // is minting a new token and redistributing it.
            const enable = createPrimaryButton('Enable', async () => {
                await enableToken(t.id);
                await refreshList(container);
            });
            action.appendChild(enable.element);
        } else {
            const revoke = createDangerButton('Revoke', 'Confirm revoke', async () => {
                await revokeToken(t.id);
                await refreshList(container);
            });
            action.appendChild(revoke.element);
        }
        tr.appendChild(action);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

async function refreshList(container: HTMLElement): Promise<void> {
    const tokens = await fetchTokens();
    renderList(container, tokens);
}

/** The way to the mint glyph. Creating one token is not surveying them all. */
function renderMintLink(container: HTMLElement, listContainer: HTMLElement): void {
    container.innerHTML = '';
    container.style.padding = '8px 0';

    // A plus, because there is one thing to add here and its name is the row
    // it becomes. The palette says the same with a symbol and no words.
    const mint = createGhostButton('+', async () => {
        openTokenMintGlyph();
    });
    mint.element.title = 'mint a token';
    mint.element.setAttribute('aria-label', 'Mint a token');
    mint.element.style.fontSize = '16px';
    mint.element.style.lineHeight = '1';
    mint.element.style.padding = '4px 10px';
    container.appendChild(mint.element);

    // A token minted in the other glyph does not reach this list on its own.
    const again = createButton({
        label: 'Refresh',
        variant: 'ghost',
        onClick: async () => { await refreshList(listContainer); },
    });
    again.element.style.marginLeft = '8px';
    container.appendChild(again.element);
}

export function createTokensGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Access Tokens',
        symbol: '⚿',
        // No initialWidth: the window then owns width and clips what does not
        // fit (packages/glyphs/manifestations/window.ts). A row carries a
        // profile URL and two timestamps, so what it needs is what it gets.
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'tokens-glyph-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'tokens-list';
            listContainer.innerHTML = '<div class="glyph-loading">Loading tokens…</div>';

            const mintContainer = document.createElement('div');
            mintContainer.className = 'tokens-mint-link';
            renderMintLink(mintContainer, listContainer);

            content.appendChild(mintContainer);
            content.appendChild(listContainer);

            refreshList(listContainer).catch(err => {
                log.error(SEG.UI, '[TokensGlyph] Failed to load tokens', err);
                listContainer.innerHTML = '';
                const errBox = document.createElement('div');
                errBox.className = 'glyph-error';
                errBox.textContent = `Failed to load tokens: ${err instanceof Error ? err.message : String(err)}`;
                listContainer.appendChild(errBox);
            });

            return content;
        },
    };
}

/** Opens the access tokens glyph. Called from the Self glyph. */
export function openTokensGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
