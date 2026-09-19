/**
 * Access Tokens Element — machine-access token management (ADR-025).
 *
 * Plain window (no panel manifestation). Reached from the Self element. Lists
 * tokens without raw values, revokes and enables them. Minting one is its own
 * element: surveying and creating are different acts.
 */

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createDangerButton, createGhostButton, createPrimaryButton } from './components/button';
import { openTokenMintElement } from './token-mint-element';
import { openTokenElement } from './token-element';
import { log, SEG } from './logger';
import { person } from './self-person';

interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    level?: string;
    namespaces: string[];
    return_address?: string;
    created_at: string;
    expires_at?: string;
    last_used_at?: string;
    revoked_at?: string;
}

const ELEMENT_ID = 'tokens-element';

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
    td.textContent = shortDID(did);
    if (!did) return td;

    // Nothing is hidden: the value is on the element and one press takes it.
    td.className = 'element-did';
    td.title = did;
    td.addEventListener('click', (e) => {
        e.stopPropagation();
        void navigator.clipboard.writeText(did).then(
            () => { td.textContent = 'copied'; setTimeout(() => { td.textContent = shortDID(did); }, 1200); },
            () => { td.textContent = 'refused'; setTimeout(() => { td.textContent = shortDID(did); }, 1200); },
        );
    });
    return td;
}

export type Age = 'now' | 'minutes' | 'hours' | 'days' | 'weeks' | 'months';

function counted(n: number, unit: string): string {
    return `${n} ${unit}${n === 1 ? '' : 's'} ago`;
}

/** How long ago, in the one unit it reads in, rounded down. Under a minute,
 *  and a moment the clock has not reached, is now. */
export function ago(then: string, now: Date): { text: string; age: Age } {
    const elapsed = now.getTime() - new Date(then).getTime();
    if (Number.isNaN(elapsed)) return { text: then, age: 'months' };
    const minutes = Math.floor(elapsed / 60000);
    if (minutes < 1) return { text: 'now', age: 'now' };
    if (minutes < 60) return { text: counted(minutes, 'minute'), age: 'minutes' };
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return { text: counted(hours, 'hour'), age: 'hours' };
    const days = Math.floor(hours / 24);
    if (days < 7) return { text: counted(days, 'day'), age: 'days' };
    if (days < 30) return { text: counted(Math.floor(days / 7), 'week'), age: 'weeks' };
    return { text: counted(Math.floor(days / 30), 'month'), age: 'months' };
}

function segment(text: string, className: string): HTMLSpanElement {
    const span = document.createElement('span');
    span.className = className;
    span.textContent = text;
    return span;
}

// "last used and Status should honestly just be collapsed into one col"
function statusPill(t: TokenInfo, now: Date): HTMLTableCellElement {
    const td = document.createElement('td');
    const used = t.last_used_at ? ago(t.last_used_at, now) : null;

    // When it was revoked is on the pill; when it was last used is on the hover.
    if (t.revoked_at) {
        const pill = segment(`revoked:${ago(t.revoked_at, now).text}`, 'element-pill element-pill-off');
        pill.title = used ? `last used ${used.text}` : 'never used';
        td.appendChild(pill);
        return td;
    }

    if (t.expires_at && new Date(t.expires_at) < now) {
        td.append(segment('expired', 'element-pill element-pill-past'), segment(fmt(t.expires_at), 'element-pill-when'));
        return td;
    }

    const pill = document.createElement('span');
    pill.className = 'token-pill';
    pill.append(
        segment('active', 'token-pill-active'),
        used ? segment(used.text, `token-age-${used.age}`) : segment('never used', 'token-pill-never'),
    );
    td.appendChild(pill);
    return td;
}

/** Exported for tests: which control a row offers is the whole point of the
 *  revoked state, and it is not reachable through the async element mount.
 *  `switches` is whether the viewer may revoke or enable: a session may, a
 *  token may not, and a row does not offer a token what a token cannot do. */
export function renderList(container: HTMLElement, tokens: TokenInfo[], switches = true, now = new Date()): void {
    container.innerHTML = '';

    if (tokens.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'element-loading';
        empty.textContent = 'No access tokens.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table tokens-table';

    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th>Label</th>
        <th>For</th>
        <th>DID</th>
        <th>Namespace</th>
        <th>Created</th>
        <th>Status</th>
        ${switches ? '<th></th>' : ''}
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const t of tokens) {
        const tr = document.createElement('tr');

        const label = document.createElement('td');
        label.textContent = t.label;
        // The label is the way in to the token's own element. The row keeps its
        // revoke and enable controls, which are not a way in.
        label.style.cursor = 'pointer';
        label.title = 'press to open this token';
        label.addEventListener('click', () => { openTokenElement(t.id, t.label); });
        tr.appendChild(label);

        function cell(text: string, className = ''): HTMLTableCellElement {
            const td = document.createElement('td');
            td.className = className;
            td.textContent = text;
            return td;
        }

        tr.appendChild(cell(t.minted_by || '—'));

        // The DID is how a token's own attestations are found (?actor=).
        tr.appendChild(didCell(t.did));
        tr.appendChild(cell(t.namespaces?.length ? t.namespaces.join(', ') : '—'));
        // What a token may read and write is not on the token: the roles its
        // name holds say, through their lines (ADR-034).
        tr.appendChild(cell(fmt(t.created_at), 'element-time'));
        tr.appendChild(statusPill(t, now));

        if (switches) {
            const action = document.createElement('td');
            action.className = 'element-actions';
            if (t.revoked_at) {
                // Revoked is a state you can leave. Without this the only way
                // back is minting a new token and redistributing it.
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
        }

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

// Who is looking decides what the rows offer: a session revokes and enables,
// a token only reads.
async function refreshList(container: HTMLElement): Promise<void> {
    const [tokens, who] = await Promise.all([fetchTokens(), person()]);
    renderList(container, tokens, who.via !== 'token');
}

/** The way to the mint element. Creating one token is not surveying them all. */
function renderMintLink(container: HTMLElement, listContainer: HTMLElement): void {
    container.innerHTML = '';
    container.style.padding = '8px 0';

    // A plus, because there is one thing to add here and its name is the row
    // it becomes. The palette says the same with a symbol and no words.
    const mint = createGhostButton('+', async () => {
        // The list hears about the token rather than waiting to be asked.
        openTokenMintElement(() => { void refreshList(listContainer); });
    });
    mint.element.title = 'mint a token';
    mint.element.setAttribute('aria-label', 'Mint a token');
    mint.element.style.fontSize = '16px';
    mint.element.style.lineHeight = '1';
    mint.element.style.padding = '4px 10px';
    container.appendChild(mint.element);

    // "i want to see the + but it needs to be grayed out and on hover it needs to say this"
    person().then(who => {
        if (who.via !== 'token') return;
        const why = 'only a session mints a token, and this page reaches the node as a token';
        mint.setDisabled(true, why);
        // A disabled button can get no hover of its own, so the row around it says it too.
        container.title = why;
    }).catch((err: unknown) => {
        log.error(SEG.UI, '[TokensElement] the node did not say who is looking', err);
    });
}

export function createTokensElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Access Tokens',
        symbol: '⚿',
        // No initialWidth: the window then owns width and clips what does not
        // fit (@teranos/elements window/window.ts). A row carries a
        // profile URL and two timestamps, so what it needs is what it gets.
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'tokens-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const listContainer = document.createElement('div');
            listContainer.className = 'tokens-list';
            listContainer.innerHTML = '<div class="element-loading">Loading tokens…</div>';

            const mintContainer = document.createElement('div');
            mintContainer.className = 'tokens-mint-link';
            renderMintLink(mintContainer, listContainer);

            content.appendChild(mintContainer);
            content.appendChild(listContainer);

            refreshList(listContainer).catch(err => {
                log.error(SEG.UI, '[TokensElement] Failed to load tokens', err);
                listContainer.innerHTML = '';
                const message = `Failed to load tokens: ${err instanceof Error ? err.message : String(err)}`;
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

/** Opens the access tokens element. Called from the Self element. */
export function openTokensElement(): void {
    tray.open(ELEMENT_ID);
}
