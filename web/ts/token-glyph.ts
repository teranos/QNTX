/**
 * Token Glyph — one access token, on its own (ADR-025, TOKATTEST).
 * Reached by pressing a row in the Access Tokens list, and by finishing a mint.
 * The raw value exists only on the second of those, and only once.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createButton, createDangerButton, createGhostButton } from './components/button';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';
import { spawnAttestationAsWindow } from './components/glyph/attestation-glyph';
import { jsonBody } from './http-utils';
import { log, SEG } from './logger';
import { knownFrom } from './roles-compose';
import type { Line } from './roles-glyph';

/** What the node says about a token. No hash, ever. */
export interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    /** Which kind: SUPER, ATTESTOR or OAUTH. Absent on a token minted before there were kinds. */
    level?: string;
    namespaces: string[];
    /** Where a client's codes go. A client's, and only a client's. */
    return_address?: string;
    created_at: string;
    expires_at?: string;
    last_used_at?: string;
    revoked_at?: string;
    // The roles its DID holds, per namespace, and the words those roles
    // carry: the node's reading, the same one the gate makes (ADR-034).
    roles?: Record<string, string[]>;
    words?: { read: string[]; write: string[]; all: boolean };
    // Every role a WRITE line names, so a grant can tell whether the role it
    // names exists yet.
    known_roles?: Record<string, string[]>;
}

async function fetchToken(id: string): Promise<TokenInfo> {
    return await apiJson<TokenInfo>(`/auth/tokens/${encodeURIComponent(id)}`);
}

/** Every role any line names, read off the lines the gate reads. */
async function rolesTheLinesName(): Promise<string[]> {
    const answer = await apiJson<{ lines?: Line[] }>('/api/roles');
    if (!Array.isArray(answer.lines)) {
        throw new Error(`/api/roles answered no lines: ${JSON.stringify(answer)}`);
    }
    return knownFrom(answer.lines, [], [], [], [], []).roles;
}

/** The grant, on the wire (ADR-034): the role to this token in every namespace
 *  it names. The label is the token's name, and what the grant names; the DID
 *  is its signature, and rides as the actor on what it writes, not here. What
 *  the role may say is the role's own lines, written in the Roles glyph. */
export function linesFor(t: TokenInfo, role: string): Array<Record<string, unknown>> {
    return (t.namespaces || []).map(namespace => ({
        subjects: [t.label], predicates: ['role:granted', role], contexts: [namespace],
    }));
}

async function grant(t: TokenInfo, role: string): Promise<void> {
    for (const line of linesFor(t, role)) {
        await apiJson('/api/attestations', jsonBody('POST', line));
    }
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

function mayRead(t: TokenInfo): string {
    if (!t.words?.read.length) return '—';
    return t.words.read.join(', ') + (t.words.all ? ' (all)' : '');
}

/** The roles per namespace, one line each, or a dash for none. */
export function rolesText(t: TokenInfo): string {
    const lines: string[] = [];
    for (const namespace of t.namespaces || []) {
        const held = t.roles?.[namespace] || [];
        lines.push(`${namespace}: ${held.length ? held.join(', ') : '—'}`);
    }
    return lines.length ? lines.join('\n') : '—';
}

// The roles, and beside the caption a + that is a pick of the roles the lines
// name. Choosing one writes the grant and the glyph is drawn again from the
// node. A role the token already holds is not offered; a new role is made in
// the Roles glyph, where its lines are.
function rolesField(container: HTMLElement, t: TokenInfo): HTMLElement {
    const wrap = document.createElement('div');
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '2px';

    const caption = document.createElement('span');
    caption.style.display = 'flex';
    caption.style.alignItems = 'center';
    caption.style.gap = '6px';
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.style.fontSize = '11px';
    caption.textContent = 'Roles';

    const held = document.createElement('div');
    held.style.whiteSpace = 'pre-line';
    held.style.wordBreak = 'break-word';
    held.style.overflowWrap = 'break-word';
    held.textContent = rolesText(t);

    // A failure is the button's own: it throws, and the button says.
    const add = createGhostButton('+', async () => {
        if (wrap.querySelector('select')) return;
        const roles = await rolesTheLinesName();
        const holds = new Set((t.namespaces || []).flatMap(ns => t.roles?.[ns] ?? []));
        const offered = roles.filter(r => !holds.has(r));
        if (offered.length === 0) {
            throw new Error(roles.length === 0 ? 'no line names a role yet' : `${t.label} holds every role the lines name`);
        }
        const pick = document.createElement('select');
        pick.className = 'glyph-input';
        const first = document.createElement('option');
        first.value = '';
        first.textContent = 'pick a role';
        pick.appendChild(first);
        for (const role of offered) {
            const option = document.createElement('option');
            option.value = role;
            option.textContent = role;
            pick.appendChild(option);
        }
        pick.addEventListener('change', () => {
            const role = pick.value;
            if (role === '') return;
            pick.disabled = true;
            grant(t, role)
                .then(() => redraw(container, t.id))
                .catch((err: unknown) => {
                    pick.disabled = false;
                    wrap.querySelector('.glyph-error')?.remove();
                    wrap.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
                });
        });
        wrap.appendChild(pick);
        pick.focus();
    });
    add.element.title = 'grant a role';
    add.element.setAttribute('aria-label', 'Grant a role');
    add.element.style.fontSize = '14px';
    add.element.style.lineHeight = '1';
    add.element.style.padding = '2px 8px';
    caption.appendChild(add.element);

    wrap.append(caption, held);
    return wrap;
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
    container.appendChild(field('Kind', t.level || '—'));
    // The DID is how this token's own attestations are found: ?actor=<did>.
    // For a client it is the client id, and the raw value above is the secret.
    container.appendChild(field('DID', t.did || '—', true));
    container.appendChild(field('Speaks for', t.minted_by || '—'));
    container.appendChild(field('Namespaces', t.namespaces?.length ? t.namespaces.join(', ') : '—'));
    if (t.return_address) {
        container.appendChild(field('Return address', t.return_address, true));
    }
    // What this token may read and write is not on the token: the roles its
    // DID holds say, through their WRITE and READ lines (ADR-034).
    container.appendChild(rolesField(container, t));
    container.appendChild(field('May write', t.words?.write.length ? t.words.write.join(', ') : '—'));
    container.appendChild(field('May read', mayRead(t)));
    container.appendChild(field('Created', fmt(t.created_at)));
    container.appendChild(field('Last used', fmt(t.last_used_at)));
    container.appendChild(field('Status', status(t)));

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

/** A DID by its last eight; anything else as it is. */
function short(s: string): string {
    return s.startsWith('did:key:') ? s.slice(-8) : s;
}

/** One attestation read out loud: X is Y of Z. A DID in it is shown by its
 *  last eight, so a line stays a line. */
export function saidLine(as: Pick<Attestation, 'subjects' | 'predicates' | 'contexts'>): string {
    const subjects = (as.subjects ?? []).map(short).join(' ') || '?';
    const predicates = (as.predicates ?? []).map(short).join(' ') || '?';
    const contexts = (as.contexts ?? []).join(' ');
    return contexts ? `${subjects} is ${predicates} of ${contexts}` : `${subjects} is ${predicates}`;
}

/** The attestations this token made, one line each, each a way into itself. */
export function renderWrote(container: HTMLElement, found: Attestation[]): void {
    container.innerHTML = '';
    container.classList.add('glyph-lines');

    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    caption.textContent = found.length === 0
        ? 'Wrote nothing yet'
        : `Wrote ${found.length}`;
    container.appendChild(caption);

    for (const as of found) {
        const row = document.createElement('div');
        // The whole of it on hover, DIDs unshortened; a press opens it.
        row.title = `${(as.subjects ?? []).join(' ')} is ${(as.predicates ?? []).join(' ')} of ${(as.contexts ?? []).join(' ')}`;
        row.textContent = saidLine(as);
        row.addEventListener('click', () => { spawnAttestationAsWindow(as); });
        container.appendChild(row);
    }
}

/** Draws it again from the node, so what is on screen is what is stored. */
async function redraw(container: HTMLElement, id: string): Promise<void> {
    let t: TokenInfo;
    try {
        t = await fetchToken(id);
    } catch (err: unknown) {
        container.innerHTML = '';
        container.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
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
                .then(t => { renderToken(content, t, raw); })
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
