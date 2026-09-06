/**
 * Token Glyph — one access token, on its own (ADR-025, TOKATTEST).
 * Reached by pressing a row in the Access Tokens list, and by finishing a mint.
 * The raw value exists only on the second of those, and only once.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createButton, createDangerButton } from './components/button';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';
import { spawnAttestationAsWindow } from './components/glyph/attestation-glyph';
import { log, SEG } from './logger';

/** What the node says about a token. No hash, ever. */
export interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    namespaces: string[];
    created_at: string;
    expires_at?: string;
    last_used_at?: string;
    revoked_at?: string;
}

async function fetchToken(id: string): Promise<TokenInfo | undefined> {
    const all = await apiJson<TokenInfo[]>('/auth/tokens');
    return all.find(t => t.id === id);
}

async function revokeToken(id: string): Promise<void> {
    await apiJson<{ status: string }>(`/auth/tokens/${encodeURIComponent(id)}`, {
        method: 'DELETE',
    });
}

async function enableToken(id: string): Promise<void> {
    await apiJson<{ status: string }>(`/auth/tokens/${encodeURIComponent(id)}/enable`, {
        method: 'POST',
    });
}

/** What this token wrote. A token is its own actor (TOKATTEST). */
async function whatItWrote(did: string): Promise<Attestation[]> {
    if (!did) return [];
    return await apiJson<Attestation[]>(
        `/api/attestations?actor=${encodeURIComponent(did)}&limit=50`,
    );
}

function fmt(dt: string | undefined): string {
    if (!dt) return '—';
    const d = new Date(dt);
    return isNaN(d.getTime()) ? dt : d.toISOString().slice(0, 19).replace('T', ' ');
}

/** A dim caption above its value, the way the attestation glyph reads. */
function field(name: string, value: string, copyable = false): HTMLElement {
    const wrap = document.createElement('div');
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '2px';

    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.style.fontSize = '11px';
    caption.textContent = name;

    const held = document.createElement('div');
    held.style.wordBreak = 'break-word';
    held.style.overflowWrap = 'break-word';
    held.textContent = value;

    if (copyable) {
        held.style.cursor = 'pointer';
        held.title = 'press to copy';
        held.addEventListener('click', () => {
            void navigator.clipboard.writeText(value).then(
                () => { caption.textContent = `${name} — copied`; },
                () => { caption.textContent = `${name} — the clipboard refused it`; },
            );
        });
    }

    wrap.append(caption, held);
    return wrap;
}

/** The raw value, on the one occasion it exists. */
function reveal(container: HTMLElement, raw: string): void {
    container.style.padding = '8px';
    container.style.border = '1px solid var(--color-warning, #fbbf24)';
    container.style.borderRadius = '4px';

    const heading = document.createElement('div');
    heading.style.fontWeight = 'bold';
    heading.textContent = 'Shown once, and will not be shown again';
    container.appendChild(heading);

    const value = document.createElement('code');
    value.style.display = 'block';
    value.style.margin = '6px 0';
    value.style.padding = '6px 8px';
    value.style.background = 'var(--bg-secondary)';
    value.style.border = '1px solid var(--border-on-dark)';
    value.style.borderRadius = 'var(--border-radius)';
    value.style.cursor = 'pointer';
    value.style.wordBreak = 'break-all';
    value.title = 'press to copy';
    value.textContent = raw;
    value.addEventListener('click', () => {
        void navigator.clipboard.writeText(raw).then(
            () => { heading.textContent = 'copied'; },
            () => { heading.textContent = 'the clipboard refused it'; },
        );
    });
    container.appendChild(value);
}

/** A glyph-error box whose text is a press away from the clipboard — the same
 *  copy-on-click acknowledgement as tokens-glyph.ts didCell(). */
function errorBox(message: string): HTMLDivElement {
    const box = document.createElement('div');
    box.className = 'glyph-error';
    box.textContent = message;
    box.style.cursor = 'pointer';
    box.title = 'press to copy';
    box.addEventListener('click', () => {
        void navigator.clipboard.writeText(message).then(
            () => { box.textContent = 'copied'; setTimeout(() => { box.textContent = message; }, 1200); },
            () => { box.textContent = 'refused'; setTimeout(() => { box.textContent = message; }, 1200); },
        );
    });
    return box;
}

function status(t: TokenInfo): string {
    if (t.revoked_at) return `revoked ${fmt(t.revoked_at)}`;
    if (t.expires_at && new Date(t.expires_at) < new Date()) return `expired ${fmt(t.expires_at)}`;
    return 'active';
}

/** Exported for tests: what the glyph draws for one token, given the token. */
export function renderToken(container: HTMLElement, t: TokenInfo, raw?: string): void {
    container.innerHTML = '';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '10px';
    container.style.padding = '12px';
    container.style.fontFamily = 'var(--font-mono)';

    if (raw) {
        const shown = document.createElement('div');
        reveal(shown, raw);
        container.appendChild(shown);
    }

    container.appendChild(field('Label', t.label || '—'));
    // The DID is how this token's own attestations are found: ?actor=<did>.
    container.appendChild(field('DID', t.did || '—', true));
    container.appendChild(field('Speaks for', t.minted_by || '—'));
    container.appendChild(field('Namespaces', t.namespaces?.length ? t.namespaces.join(', ') : '—'));
    container.appendChild(field('Created', fmt(t.created_at)));
    container.appendChild(field('Last used', fmt(t.last_used_at)));
    container.appendChild(field('Status', status(t)));
    // What this token may read and write is not on the token: the roles its
    // DID holds say, through their WRITE and READ lines (ADR-034).

    const actions = document.createElement('div');
    actions.style.display = 'flex';
    actions.style.gap = '8px';
    actions.style.flexWrap = 'wrap';

    if (t.revoked_at) {
        const enable = createButton({
            label: 'Enable',
            variant: 'secondary',
            onClick: async () => {
                await enableToken(t.id);
                await redraw(container, t.id);
            },
        });
        actions.appendChild(enable.element);
    } else {
        const revoke = createDangerButton('Revoke', 'Confirm revoke', async () => {
            await revokeToken(t.id);
            await redraw(container, t.id);
        });
        actions.appendChild(revoke.element);
    }

    container.appendChild(actions);

    const wrote = document.createElement('div');
    wrote.style.display = 'flex';
    wrote.style.flexDirection = 'column';
    wrote.style.gap = '4px';
    wrote.style.marginTop = '6px';
    wrote.innerHTML = '<div class="glyph-loading">Reading what it wrote…</div>';
    container.appendChild(wrote);

    whatItWrote(t.did)
        .then(found => { renderWrote(wrote, found); })
        .catch((err: unknown) => {
            log.warn(SEG.UI, '[TokenGlyph] could not read what this token wrote', err);
            wrote.innerHTML = '';
            wrote.appendChild(field('Wrote', err instanceof Error ? err.message : String(err)));
        });
}

/** The attestations this token made, each one a way into itself. */
export function renderWrote(container: HTMLElement, found: Attestation[]): void {
    container.innerHTML = '';

    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.style.fontSize = '11px';
    caption.textContent = found.length === 0
        ? 'Wrote nothing yet'
        : `Wrote ${found.length}`;
    container.appendChild(caption);

    for (const as of found) {
        const row = document.createElement('div');
        row.style.cursor = 'pointer';
        row.style.padding = '2px 0';
        row.style.wordBreak = 'break-word';
        row.style.overflowWrap = 'break-word';
        row.title = 'press to open';
        const subjects = as.subjects?.join(', ') || '?';
        const predicates = as.predicates?.join(', ') || '?';
        row.textContent = `${subjects} is ${predicates}`;
        row.addEventListener('click', () => { spawnAttestationAsWindow(as); });
        container.appendChild(row);
    }
}

/** Draws it again from the node, so what is on screen is what is stored. */
async function redraw(container: HTMLElement, id: string): Promise<void> {
    const t = await fetchToken(id);
    if (!t) {
        container.innerHTML = '';
        container.appendChild(errorBox(`The node no longer lists token ${id}.`));
        return;
    }
    renderToken(container, t);
}

function glyphIdFor(id: string): string {
    return `token-glyph-${id}`;
}

/**
 * Opens one token as its own glyph. `raw` is present only when the token was
 * just minted, which is the one moment it exists at all.
 */
export function openTokenGlyph(id: string, label: string, raw?: string): void {
    const glyphId = glyphIdFor(id);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: label || id,
        symbol: '⚿',
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'token-glyph-content';
            content.innerHTML = '<div class="glyph-loading">Loading token…</div>';

            fetchToken(id)
                .then(t => {
                    if (!t) {
                        content.innerHTML = '';
                        content.appendChild(errorBox(`The node does not list token ${id}.`));
                        return;
                    }
                    renderToken(content, t, raw);
                })
                .catch((err: unknown) => {
                    log.error(SEG.UI, '[TokenGlyph] the node did not answer for this token', err);
                    content.innerHTML = '';
                    content.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
                });

            return content;
        },
    } satisfies Glyph);

    glyphRun.openGlyph(glyphId);
}
