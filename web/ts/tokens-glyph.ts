/**
 * Access Tokens Glyph — machine-access token management (ADR-025).
 *
 * Plain window (no panel manifestation). Reached from the Self glyph.
 * Lists tokens without raw values, creates new tokens (raw shown once),
 * revokes existing tokens. All API calls go through /auth/tokens.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createButton, createDangerButton, createPrimaryButton } from './components/button';
import { log, SEG } from './logger';

interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    namespace: string;
    scope_read: string[];
    scope_write: string[];
    created_at: string;
    expires_at?: string;
    last_used_at?: string;
    revoked_at?: string;
}

/** What a token may touch. */
interface TokenScope {
    read: string[];
    write: string[];
}

// Every predicate, read and write. A token minted here carries the reach of the
// session that minted it, and that session is a root identity.
const fullReach: TokenScope = { read: ['*'], write: ['*'] };

interface CreateTokenResponse {
    id: string;
    label: string;
    token: string;
    created_at: string;
    expires_at?: string;
}

const GLYPH_ID = 'tokens-glyph';

async function fetchTokens(): Promise<TokenInfo[]> {
    return await apiJson<TokenInfo[]>('/auth/tokens');
}

async function createToken(
    label: string,
    namespace: string,
    scope: TokenScope,
): Promise<CreateTokenResponse> {
    return await apiJson<CreateTokenResponse>('/auth/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ label, namespace, scope }),
    });
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

function fmt(dt: string | undefined): string {
    if (!dt) return '—';
    const d = new Date(dt);
    return isNaN(d.getTime()) ? dt : d.toISOString().slice(0, 19).replace('T', ' ');
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

    // FIXME: a glyph should size to its content. This scrolls horizontally as
    // a local workaround; the structural fix belongs in the glyphs package.
    const scroller = document.createElement('div');
    scroller.style.width = '100%';
    scroller.style.overflowX = 'auto';

    const table = document.createElement('table');
    table.className = 'tokens-table';
    table.style.width = '100%';
    table.style.borderCollapse = 'collapse';

    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th style="text-align:left;padding:4px 8px;">Label</th>
        <th style="text-align:left;padding:4px 8px;">For</th>
        <th style="text-align:left;padding:4px 8px;">Created</th>
        <th style="text-align:left;padding:4px 8px;">Last used</th>
        <th style="text-align:left;padding:4px 8px;">Status</th>
        <th style="padding:4px 8px;"></th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const t of tokens) {
        const tr = document.createElement('tr');

        const label = document.createElement('td');
        label.style.padding = '4px 8px';
        label.textContent = t.label;
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

        const created = document.createElement('td');
        created.style.padding = '4px 8px';
        created.textContent = fmt(t.created_at);
        tr.appendChild(created);

        const used = document.createElement('td');
        used.style.padding = '4px 8px';
        used.textContent = fmt(t.last_used_at);
        tr.appendChild(used);

        const status = document.createElement('td');
        status.style.padding = '4px 8px';
        if (t.revoked_at) {
            status.textContent = `revoked ${fmt(t.revoked_at)}`;
        } else if (t.expires_at && new Date(t.expires_at) < new Date()) {
            status.textContent = `expired ${fmt(t.expires_at)}`;
        } else {
            status.textContent = 'active';
        }
        tr.appendChild(status);

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
    scroller.appendChild(table);
    container.appendChild(scroller);
}

async function refreshList(container: HTMLElement): Promise<void> {
    const tokens = await fetchTokens();
    renderList(container, tokens);
}

function renderCreateForm(container: HTMLElement, listContainer: HTMLElement, revealContainer: HTMLElement): void {
    container.innerHTML = '';
    container.style.display = 'flex';
    container.style.flexWrap = 'wrap';
    container.style.gap = '8px';
    container.style.alignItems = 'center';
    container.style.padding = '8px 0';

    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'tokens-label-input';
    input.placeholder = 'label';
    input.style.flex = '1';
    input.style.minWidth = '0';

    // The button turns its label into the word Error and puts the reason in a
    // tooltip, which is a refusal nobody reads. Minting a credential is not a
    // place to hide why it did not happen.
    const refusal = document.createElement('div');
    refusal.className = 'tokens-refusal';
    refusal.style.flexBasis = '100%';
    refusal.style.color = 'var(--color-error)';
    refusal.style.wordBreak = 'break-word';
    refusal.style.overflowWrap = 'break-word';

    const create = createPrimaryButton('Create token', async () => {
        refusal.textContent = '';
        try {
            const label = input.value.trim();
            if (!label) {
                throw new Error('no label');
            }
            const resp = await createToken(label, '', fullReach);
            input.value = '';
            showRaw(revealContainer, resp);
            await refreshList(listContainer);
        } catch (e) {
            refusal.textContent = e instanceof Error ? e.message : String(e);
            throw e;
        }
    });

    container.append(input, create.element, refusal);
}

function showRaw(container: HTMLElement, resp: CreateTokenResponse): void {
    container.innerHTML = '';
    container.style.padding = '8px';
    container.style.border = '1px solid var(--color-warning, #fbbf24)';
    container.style.borderRadius = '4px';
    container.style.marginTop = '8px';

    const heading = document.createElement('div');
    heading.style.fontWeight = 'bold';
    heading.textContent = `Token "${resp.label}" — shown once, will not be shown again`;
    container.appendChild(heading);

    // Shown once, so pressing it is the whole interaction. A value you can read
    // and not take is a value you have to retype from a screenshot.
    const value = document.createElement('code');
    value.style.display = 'block';
    value.style.margin = '6px 0';
    value.style.padding = '6px 8px';
    value.style.background = 'var(--bg-secondary)';
    value.style.border = '1px solid var(--border-on-dark)';
    value.style.borderRadius = 'var(--border-radius)';
    value.style.fontFamily = 'var(--font-mono)';
    value.style.cursor = 'pointer';
    value.style.wordBreak = 'break-all';
    value.style.overflowWrap = 'break-word';
    value.title = 'press to copy';
    value.textContent = resp.token;
    value.addEventListener('click', () => {
        void navigator.clipboard.writeText(resp.token).then(
            () => { heading.textContent = 'copied'; },
            () => { heading.textContent = 'the clipboard refused it'; },
        );
    });
    container.appendChild(value);

    const copy = createButton({
        label: 'Copy to clipboard',
        variant: 'secondary',
        onClick: async () => {
            await navigator.clipboard.writeText(resp.token);
        },
    });
    container.appendChild(copy.element);

    const dismiss = createButton({
        label: 'Dismiss',
        variant: 'ghost',
        onClick: async () => {
            container.innerHTML = '';
            container.style.border = 'none';
            container.style.padding = '0';
            container.style.marginTop = '0';
        },
    });
    dismiss.element.style.marginLeft = '8px';
    container.appendChild(dismiss.element);
}

export function createTokensGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: '⚿ Access Tokens',
        initialWidth: '560px',
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

            const revealContainer = document.createElement('div');
            revealContainer.className = 'tokens-reveal';

            const formContainer = document.createElement('div');
            formContainer.className = 'tokens-create-form';

            renderCreateForm(formContainer, listContainer, revealContainer);

            content.appendChild(formContainer);
            content.appendChild(revealContainer);
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
