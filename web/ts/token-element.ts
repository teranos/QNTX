/**
 * Token Element — one access token, on its own (ADR-025, TOKATTEST).
 * Reached by pressing a row in the Access Tokens list, and by finishing a mint.
 * The raw value exists only on the second of those, and only once.
 */

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { createButton, createDangerButton, createGhostButton } from './components/button';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';
import { spawnAttestationAsWindow } from './components/element/attestation-element';
import { jsonBody } from './http-utils';
import { log, SEG } from './logger';
import { kindOf, ordered, type Namespace } from './namespaces-view';
import { knownFrom } from './roles-compose';
import type { Line } from './roles-element';

/** What the node says about a token. No hash, ever. */
export interface TokenInfo {
    id: string;
    label: string;
    did: string;
    minted_by: string;
    /** The name of the person who minted it, recorded at minting. */
    minted_by_display_name?: string;
    /** Which kind: SUPER, ATTESTOR or OAUTH. Absent on a token minted before there were kinds. */
    level?: string;
    namespaces: string[];
    /** Where a client's codes go. A client's, and only a client's. */
    return_address?: string;
    /** The client this token was issued through, when it was. */
    client_did?: string;
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
 *  the role may say is the role's own lines, written in the Roles element. */
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

/** A dim caption above its value, the way the attestation element reads. */
function field(name: string, value: string, copyable = false, hover = ''): HTMLElement {
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
    if (hover) held.title = hover;

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

/** An element-error box whose text is a press away from the clipboard — the same
 *  copy-on-click acknowledgement as tokens-element.ts didCell(). */
function errorBox(message: string): HTMLDivElement {
    const box = document.createElement('div');
    box.className = 'element-error';
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
// name. Choosing one writes the grant and the element is drawn again from the
// node. A role the token already holds is not offered; a new role is made in
// the Roles element, where its lines are.
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
        pick.className = 'input';
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
                    wrap.querySelector('.element-error')?.remove();
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

/** Makes the client active in a namespace; every connector follows at its next refresh. */
async function makeActive(id: string, namespace: string): Promise<void> {
    await apiJson(`/auth/tokens/${encodeURIComponent(id)}/namespace`, jsonBody('POST', { namespace }));
}

/** Puts the client into a namespace without making it active there. */
async function putIn(id: string, namespace: string): Promise<void> {
    await apiJson(`/auth/tokens/${encodeURIComponent(id)}/namespaces`, jsonBody('POST', { namespace }));
}

/** Takes the client out of a namespace it is not active in. */
async function takeOut(id: string, namespace: string): Promise<void> {
    await apiJson(
        `/auth/tokens/${encodeURIComponent(id)}/namespaces/${encodeURIComponent(namespace)}`,
        { method: 'DELETE' },
    );
}

/** A namespace-bar tile: the same rectangle, size and kind colour. */
function nsTile(name: string): HTMLDivElement {
    const tile = document.createElement('div');
    tile.className = 'namespace-tile';
    tile.dataset.kind = kindOf(name);
    tile.dataset.name = name;
    tile.textContent = name;
    return tile;
}

function part(which: string, text: string): HTMLSpanElement {
    const span = document.createElement('span');
    span.className = 'namespace-part';
    span.dataset.part = which;
    span.textContent = text;
    return span;
}

/** The rectangle over the tile the client is active in, moved rather than
 *  remade, so making another active carries it across the row. */
function place(row: HTMLElement, rectangle: HTMLElement, here: HTMLElement | null): void {
    if (!here) {
        rectangle.hidden = true;
        return;
    }
    rectangle.hidden = false;
    rectangle.style.width = `${here.offsetWidth}px`;
    rectangle.style.height = `${here.offsetHeight}px`;
    rectangle.style.transform = `translate(${here.offsetLeft}px, ${here.offsetTop}px)`;
}

/** The namespaces as the namespace bar draws them, below the title bar: one
 *  tile each, the rectangle over the one a client is active in. For a live
 *  client, ROOT presses a tile to make it active, right-clicks one to split it
 *  into [<] name [X] and take the client out, and [+] puts it into another.
 *  "A TOKEN CAN ONLY BE ACTIVE IN ONE NAMESPACE AT A TIME" */
export function namespacesField(container: HTMLElement, t: TokenInfo): HTMLElement {
    const row = document.createElement('div');
    row.className = 'token-namespaces';

    const tiles = document.createElement('div');
    tiles.className = 'namespaces-tiles';
    row.appendChild(tiles);

    const held = t.namespaces || [];
    const client = t.level === 'OAUTH';
    const editable = client && !t.revoked_at;
    const active = client ? held[0] : undefined;
    const failed = (err: unknown) => {
        row.querySelector('.namespaces-failure')?.remove();
        const said = document.createElement('div');
        said.className = 'namespaces-failure';
        said.textContent = err instanceof Error ? err.message : String(err);
        row.appendChild(said);
    };

    const rectangle = document.createElement('div');
    rectangle.className = 'namespaces-rectangle';
    rectangle.hidden = true;

    const shown = ordered(held.map(name => ({ name, definition: null, kinds: [] })));
    if (shown.length === 0) {
        const none = document.createElement('span');
        none.textContent = '—';
        tiles.appendChild(none);
    }

    for (const ns of shown) {
        const tile = nsTile(ns.name);
        if (ns.name === active) {
            tile.classList.add('standing');
            tile.title = 'active: every connector through this client acts here';
        }
        tiles.appendChild(tile);
        if (!editable || ns.name === active) continue;

        tile.title = 'press to make it active here; right-click to take it out';
        tile.addEventListener('click', () => {
            if (tile.classList.contains('open')) return;
            makeActive(t.id, ns.name)
                .then(() => {
                    // The rectangle goes where the node says it went, and the
                    // element is drawn again once it has arrived.
                    tiles.querySelector('.namespace-tile.standing')?.classList.remove('standing');
                    tile.classList.add('standing');
                    place(row, rectangle, tile);
                    setTimeout(() => { void redraw(container, t.id); }, 200);
                })
                .catch(failed);
        });

        tile.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            if (tile.classList.contains('open')) return;
            tile.classList.add('open');
            tile.textContent = '';
            const back = part('back', '<');
            const name = part('toggle', ns.name);
            const end = part('end', 'X');
            end.dataset.end = 'active';
            tile.append(back, name, end);
            back.addEventListener('click', (ev) => {
                ev.stopPropagation();
                tile.classList.remove('open');
                tile.textContent = ns.name;
            });
            // Once arms it, and a second press takes the client out.
            end.addEventListener('click', (ev) => {
                ev.stopPropagation();
                if (end.dataset.end !== 'sure') {
                    end.dataset.end = 'sure';
                    return;
                }
                takeOut(t.id, ns.name).then(() => redraw(container, t.id)).catch(failed);
            });
        });
    }

    if (editable) {
        const add = document.createElement('div');
        add.className = 'namespace-tile namespace-add';
        add.textContent = '+';
        add.title = 'put this client into another namespace';
        add.addEventListener('click', () => {
            const offered = tiles.querySelectorAll('.token-ns-offer');
            if (offered.length > 0) {
                offered.forEach(o => o.remove());
                return;
            }
            apiJson<{ namespaces: Namespace[] }>('/api/namespaces')
                .then(listed => {
                    for (const ns of ordered(listed.namespaces || []).filter(ns => !held.includes(ns.name))) {
                        const offer = nsTile(ns.name);
                        offer.classList.add('token-ns-offer');
                        offer.style.borderStyle = 'dashed';
                        offer.title = `put this client into ${ns.name}`;
                        offer.addEventListener('click', () => {
                            putIn(t.id, ns.name).then(() => redraw(container, t.id)).catch(failed);
                        });
                        tiles.appendChild(offer);
                    }
                })
                .catch(failed);
        });
        tiles.appendChild(add);
    }

    if (active !== undefined) {
        row.appendChild(rectangle);
        requestAnimationFrame(() => {
            place(row, rectangle, tiles.querySelector<HTMLElement>('.namespace-tile.standing'));
        });
    }
    return row;
}

function status(t: TokenInfo): string {
    if (t.revoked_at) return `revoked ${fmt(t.revoked_at)}`;
    if (t.expires_at && new Date(t.expires_at) < new Date()) return `expired ${fmt(t.expires_at)}`;
    return 'active';
}

/** What a client issued, newest first: an access token and a refresh token
 *  per sign-in and per refresh. */
export function issuedBy(client: TokenInfo, all: TokenInfo[]): TokenInfo[] {
    return all
        .filter(t => t.id !== client.id && !!t.client_did && t.client_did === client.did)
        .sort((a, b) => b.created_at.localeCompare(a.created_at));
}

function stateOf(t: TokenInfo, now: Date): string {
    if (t.revoked_at) return 'revoked';
    if (t.expires_at && new Date(t.expires_at) < now) return 'expired';
    return 'active';
}

/** The issued list, one line each: kind, day and state, the time on the hover,
 *  and a press opens the one line's token. */
export function renderIssued(container: HTMLElement, issued: TokenInfo[], now: Date): void {
    container.innerHTML = '';
    // Smaller than the list it stands in for, not larger.
    container.style.fontSize = '11px';
    container.style.lineHeight = '1.4';
    container.style.gap = '0';
    const caption = document.createElement('span');
    caption.style.color = 'var(--text-on-dark-tertiary)';
    const live = issued.filter(t => stateOf(t, now) === 'active').length;
    caption.textContent = issued.length === 0 ? 'Issued nothing yet' : `Issued ${issued.length}, ${live} live`;
    container.appendChild(caption);

    for (const t of issued) {
        const line = document.createElement('div');
        line.className = 'token-issued-line';
        line.style.cursor = 'pointer';
        line.style.whiteSpace = 'nowrap';
        line.textContent = `${t.level || '—'} ${fmt(t.created_at).slice(0, 10)} ${stateOf(t, now)}`;
        line.title = `created ${fmt(t.created_at)}`;
        if (stateOf(t, now) !== 'active') line.style.color = 'var(--text-on-dark-tertiary)';
        line.addEventListener('click', () => { openTokenElement(t.id, `${t.label} ${t.level ?? ''}`.trim()); });
        container.appendChild(line);
    }
}

/** Exported for tests: what the element draws for one token, given the token. */
export function renderToken(container: HTMLElement, t: TokenInfo, raw?: string): void {
    container.innerHTML = '';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '10px';
    container.style.padding = '12px';
    container.style.fontFamily = 'var(--font-mono)';

    // The namespaces first, directly below the title bar, the way the
    // namespace bar sits below the system bar.
    container.appendChild(namespacesField(container, t));

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
    // Tokens are owned by someone: the name, and on the hover the identity it
    // was minted under.
    container.appendChild(field('Owner', t.minted_by_display_name || t.minted_by || '—', false, t.minted_by || ''));
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

    // "can't we move all the refresh tokens into a compact list in the element
    // of the token it belongs to?"
    if (t.level === 'OAUTH') {
        const issued = document.createElement('div');
        issued.style.display = 'flex';
        issued.style.flexDirection = 'column';
        issued.style.gap = '2px';
        issued.innerHTML = '<div class="element-loading">Reading what it issued…</div>';
        container.appendChild(issued);
        apiJson<TokenInfo[]>('/auth/tokens')
            .then(all => { renderIssued(issued, issuedBy(t, all), new Date()); })
            .catch((err: unknown) => {
                issued.innerHTML = '';
                issued.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
            });
    }

    const wrote = document.createElement('div');
    wrote.style.display = 'flex';
    wrote.style.flexDirection = 'column';
    wrote.style.gap = '4px';
    wrote.style.marginTop = '6px';
    wrote.innerHTML = '<div class="element-loading">Reading what it wrote…</div>';
    container.appendChild(wrote);

    whatItWrote(t.did)
        .then(found => { renderWrote(wrote, found); })
        .catch((err: unknown) => {
            log.warn(SEG.UI, '[TokenElement] could not read what this token wrote', err);
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
    container.classList.add('element-lines');

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

function elementIdFor(id: string): string {
    return `token-element-${id}`;
}

/**
 * Opens one token as its own element. `raw` is present only when the token was
 * just minted, which is the one moment it exists at all.
 */
export function openTokenElement(id: string, label: string, raw?: string): void {
    const elementId = elementIdFor(id);
    if (tray.has(elementId)) {
        tray.open(elementId);
        return;
    }

    tray.add({
        id: elementId,
        title: label || id,
        symbol: '⚿',
        onClose: () => { tray.remove(elementId); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'token-element-content';
            content.innerHTML = '<div class="element-loading">Loading token…</div>';

            fetchToken(id)
                .then(t => { renderToken(content, t, raw); })
                .catch((err: unknown) => {
                    log.error(SEG.UI, '[TokenElement] the node did not answer for this token', err);
                    content.innerHTML = '';
                    content.appendChild(errorBox(err instanceof Error ? err.message : String(err)));
                });

            return content;
        },
    } satisfies Element);

    tray.open(elementId);
}
