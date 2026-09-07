/**
 * Stands — a market's public pixels (ADR-035).
 */

// A stand is a public pixel. ROOT creates one into a market (a namespace, never
// system or default); it answers on /s/{market}/{slug} and records one untrusted
// arrival per hit. This glyph lists every stand across all markets as one row
// each; opening a row shows that one stand — its door, who created it, which
// sites report back, whether it is alive, and the snippet to paste — and is the
// only place a stand is deleted. The pixel side names the event; a stand writes
// only under staand:*, so nothing here sets a predicate.

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { backendUrl } from './client/url';
import { createPrimaryButton, createDangerButton, createGhostButton } from './components/button';
import { tooltip } from './components/tooltip';
import { log, SEG } from './logger';

/** One stand as the glyph sees it: what it is, its defining system attestation,
 *  the door it is bound to, the sites reporting back, and its activity. */
export interface StaandInfo {
    slug: string;
    market: string;
    label: string;
    url: string;
    origin: string;
    creator: string;
    defId: string;
    created: string;
    sites: string[];
    arrivals: number;
    dropped: number;
    lastSeen: string;
}

const GLYPH_ID = 'market-glyph';

async function fetchStands(): Promise<StaandInfo[]> {
    const body = await apiJson<{ staands: StaandInfo[] }>('/api/staands');
    return body.staands ?? [];
}

/** Creates a stand in a market. The node's refusal is the error the Button shows.
 *  origin is the door the stand is bound to: empty means open, any site writes. */
async function createStand(market: string, slug: string, label: string, origin: string): Promise<void> {
    await apiJson('/api/staands', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ market, slug, label, origin }),
    });
}

/** Deletes a stand: it stops recording, and the record stays (ADR-026). */
async function deleteStand(market: string, slug: string): Promise<void> {
    await apiJson(`/api/staands?market=${encodeURIComponent(market)}&slug=${encodeURIComponent(slug)}`, {
        method: 'DELETE',
    });
}

/** The full URL of a stand's pixel, host and all — the base every snippet builds on. */
export function fullURL(url: string): string {
    return backendUrl() + url;
}

/** The snippet to paste: a tiny helper that names an event and its params, the
 *  way gtag('event', name, …) does, and fires the pixel as an image. The site
 *  owner sets the event, so the predicate it records is staand:<event>. */
export function standSnippet(url: string): string {
    const base = fullURL(url);
    return [
        '<script>',
        'window.stand = (event, params = {}) => {',
        '  let id = localStorage.getItem("stand_id");',
        '  if (!id) { id = crypto.randomUUID(); localStorage.setItem("stand_id", id); }',
        `  const u = new URL(${JSON.stringify(base)});`,
        '  u.searchParams.set("e", event);',
        '  u.searchParams.set("subject", id);',
        '  for (const k in params) u.searchParams.set(k, params[k]);',
        '  new Image().src = u.toString();',
        '};',
        'stand("pageview");',
        '</script>',
        '',
        '<!-- then, on any interaction: -->',
        '<!-- stand("contact_click", { method: "whatsapp" }); -->',
    ].join('\n');
}

// One font scale, one margin: the glyph sets these once and every part reads
// them, so nothing drifts (UI-wide, ADR-035).
const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** A value that copies to the clipboard on press, saying so as a tooltip. */
function copyable(shown: string, toCopy: string): HTMLElement {
    const el = document.createElement('span');
    el.textContent = shown;
    el.className = 'has-tooltip';
    el.setAttribute('data-tooltip', 'press to copy');
    el.style.cursor = 'pointer';
    el.style.wordBreak = 'break-all';
    el.addEventListener('click', () => {
        void navigator.clipboard.writeText(toCopy).then(
            () => { el.textContent = 'copied'; setTimeout(() => { el.textContent = shown; }, 1200); },
            () => { el.textContent = 'refused'; setTimeout(() => { el.textContent = shown; }, 1200); },
        );
    });
    return el;
}

/** A stand's health in words. Zero arrivals is quiet, not broken. Rate-limited
 *  drops are shown against what was recorded, so a stand hitting its budget is
 *  visible (ADR-035). */
function aliveText(s: StaandInfo): string {
    if (s.arrivals === 0 && s.dropped === 0) {
        return '○ quiet — no arrivals yet';
    }
    const parts = [`● ${s.arrivals} recorded`];
    if (s.dropped > 0) {
        parts.push(`${s.dropped} rate-limited`);
    }
    if (s.lastSeen !== '') {
        parts.push(`last ${s.lastSeen}`);
    }
    return parts.join(' · ');
}

/** Exported for tests: the list, one row per stand across all markets. Each row
 *  names its market and slug and opens the stand on press. */
export function renderStandList(container: HTMLElement, stands: StaandInfo[], onOpen: (s: StaandInfo) => void): void {
    container.replaceChildren();

    if (stands.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No stands yet. Create one below.';
        container.appendChild(empty);
        return;
    }

    for (const s of stands) {
        const row = document.createElement('div');
        row.className = 'stand-row';
        row.style.display = 'flex';
        row.style.justifyContent = 'space-between';
        row.style.alignItems = 'baseline';
        row.style.gap = '8px';
        row.style.padding = '8px ' + EDGE;
        row.style.borderBottom = '1px solid ' + LINE;
        row.style.cursor = 'pointer';
        row.style.fontSize = SIZE;

        const left = document.createElement('span');
        const market = document.createElement('span');
        market.textContent = s.market + ' / ';
        market.style.color = MUTE;
        const slug = document.createElement('span');
        slug.textContent = s.slug;
        left.appendChild(market);
        left.appendChild(slug);
        if (s.label) {
            const label = document.createElement('span');
            label.textContent = ' — ' + s.label;
            label.style.color = MUTE;
            left.appendChild(label);
        }

        const health = document.createElement('span');
        health.textContent = s.arrivals > 0 || s.dropped > 0 ? '●' : '○';
        health.className = 'has-tooltip';
        health.setAttribute('data-tooltip', aliveText(s));
        health.style.color = MUTE;

        row.appendChild(left);
        row.appendChild(health);
        row.addEventListener('click', () => onOpen(s));
        container.appendChild(row);
    }
}

/** One labelled fact line. The value may carry a tooltip for the long form. */
function fact(label: string, value: HTMLElement | string, tip?: string): HTMLElement {
    const row = document.createElement('div');
    row.style.display = 'flex';
    row.style.gap = '10px';
    row.style.padding = '3px 0';
    row.style.fontSize = SIZE;
    const key = document.createElement('span');
    key.textContent = label;
    key.style.color = MUTE;
    key.style.minWidth = '9em';
    key.style.flexShrink = '0';
    const val = typeof value === 'string' ? document.createElement('span') : value;
    if (typeof value === 'string') {
        val.textContent = value;
        val.style.wordBreak = 'break-all';
    }
    if (tip) {
        val.classList.add('has-tooltip');
        val.setAttribute('data-tooltip', tip);
    }
    row.appendChild(key);
    row.appendChild(val);
    return row;
}

/** Exported for tests: one stand opened. Its defining system attestation, the
 *  door, the sites reporting back, its activity, the snippet to paste — and
 *  Delete, the one destructive act, set apart at the foot. */
export function renderStandDetail(
    container: HTMLElement,
    s: StaandInfo,
    onBack: () => void,
    onDelete: () => Promise<void>,
): void {
    container.replaceChildren();

    const back = createGhostButton('← Stands', onBack);
    container.appendChild(back.element);

    const title = document.createElement('div');
    title.textContent = s.market + ' / ' + s.slug;
    title.style.fontSize = '15px';
    title.style.margin = '8px 0';
    container.appendChild(title);

    // The long DID and ASID wrap in full (no truncation) and also carry the
    // value in a tooltip, so a glance reads them and a hover copies (ADR-035).
    container.appendChild(fact('Market', s.market));
    container.appendChild(fact('Door', s.origin.trim() === '' ? 'open — any site may write' : s.origin));
    container.appendChild(fact('Created by', s.creator || '—', s.creator || undefined));
    container.appendChild(fact('Defined by', s.defId || '—', s.defId || undefined));
    container.appendChild(fact('Created', s.created || '—'));
    container.appendChild(fact('Reporting from', s.sites.length > 0 ? s.sites.join(', ') : '—'));
    container.appendChild(fact('Activity', aliveText(s)));
    container.appendChild(fact('URL', copyable(fullURL(s.url), fullURL(s.url))));

    const snippetLabel = document.createElement('div');
    snippetLabel.textContent = 'Snippet — paste on your site; you name the event:';
    snippetLabel.style.color = MUTE;
    snippetLabel.style.fontSize = SIZE;
    snippetLabel.style.margin = '10px 0 4px';
    container.appendChild(snippetLabel);

    const snippet = standSnippet(s.url);
    const pre = document.createElement('pre');
    pre.className = 'has-tooltip stand-snippet';
    pre.setAttribute('data-tooltip', 'press to copy');
    pre.textContent = snippet;
    pre.style.fontSize = '12px';
    pre.style.whiteSpace = 'pre-wrap';
    pre.style.wordBreak = 'break-all';
    pre.style.padding = '8px ' + EDGE;
    pre.style.border = '1px solid ' + LINE;
    pre.style.borderRadius = '6px';
    pre.style.cursor = 'pointer';
    pre.addEventListener('click', () => {
        void navigator.clipboard.writeText(snippet).then(
            () => { pre.setAttribute('data-tooltip', 'copied'); },
            () => { pre.setAttribute('data-tooltip', 'refused'); },
        );
    });
    container.appendChild(pre);

    // Delete: set apart at the foot, above a rule, so it is not among the
    // reading controls (ADR-035).
    const foot = document.createElement('div');
    foot.style.marginTop = '16px';
    foot.style.paddingTop = '10px';
    foot.style.borderTop = '1px solid ' + LINE;
    foot.style.display = 'flex';
    foot.style.justifyContent = 'flex-end';
    const del = createDangerButton('Delete', 'Confirm delete', async () => {
        await onDelete();
    });
    foot.appendChild(del.element);
    container.appendChild(foot);
}

/** Exported for tests: the create form — market, slug, label and a door. No
 *  predicate: the pixel side names the event (ADR-035). */
export function buildCreateForm(onCreate: (market: string, slug: string, label: string, origin: string) => Promise<void>): HTMLElement {
    const form = document.createElement('div');
    form.className = 'stand-create';
    form.style.display = 'flex';
    form.style.gap = '6px';
    form.style.alignItems = 'center';
    form.style.flexWrap = 'wrap';
    form.style.padding = '10px ' + EDGE;

    const input = (placeholder: string, cls: string): HTMLInputElement => {
        const el = document.createElement('input');
        el.type = 'text';
        el.placeholder = placeholder;
        el.className = cls;
        el.style.fontFamily = FONT;
        el.style.fontSize = SIZE;
        el.style.padding = '4px 8px';
        form.appendChild(el);
        return el;
    };

    const market = input('market — a namespace, never system or default', 'stand-market');
    const slug = input('slug', 'stand-slug');
    const label = input('label', 'stand-label');
    const origin = input('door — the site allowed to write, blank = open', 'stand-origin');

    const create = createPrimaryButton('Create', async () => {
        const m = market.value.trim();
        const g = slug.value.trim();
        if (m === '' || g === '') {
            throw new Error('a stand needs a market and a slug');
        }
        await onCreate(m, g, label.value.trim(), origin.value.trim());
        slug.value = '';
        label.value = '';
        origin.value = '';
    });
    form.appendChild(create.element);
    return form;
}

interface View { current: StaandInfo | null }

// render draws the list or, when a stand is open, its detail. Every failure the
// node hands back is shown where it happened: a refused create or delete
// surfaces on its Button, a refused list paints here. Nothing is only logged.
async function render(view: View, root: HTMLElement): Promise<void> {
    const reload = () => { void render(view, root); };

    let stands: StaandInfo[];
    try {
        stands = await fetchStands();
    } catch (err: unknown) {
        showRefusal(root, err);
        return;
    }

    // A stand open in the detail view is refreshed from the new list; if it is
    // gone (deleted elsewhere), fall back to the list.
    if (view.current) {
        const fresh = stands.find((s) => s.market === view.current?.market && s.slug === view.current?.slug);
        if (!fresh) {
            view.current = null;
        } else {
            view.current = fresh;
        }
    }

    root.replaceChildren();

    if (view.current) {
        renderStandDetail(root, view.current, () => { view.current = null; reload(); }, async () => {
            await deleteStand(view.current!.market, view.current!.slug);
            view.current = null;
            reload();
        });
        return;
    }

    root.appendChild(buildCreateForm(async (market, slug, label, origin) => {
        await createStand(market, slug, label, origin);
        reload();
    }));
    const list = document.createElement('div');
    list.className = 'stands-list';
    renderStandList(list, stands, (s) => { view.current = s; reload(); });
    root.appendChild(list);
}

// showRefusal logs and shows: logging alone is hiding. The message lands in the
// glyph, copyable, so the operator reads exactly what the node said.
function showRefusal(root: HTMLElement, err: unknown): void {
    log.error(SEG.UI, '[Stands] the node refused', err);
    const message = `the node refused: ${err instanceof Error ? err.message : String(err)}`;
    root.replaceChildren();
    const box = copyable(message, message);
    box.className += ' glyph-error';
    box.style.display = 'block';
    box.style.padding = EDGE;
    root.appendChild(box);
}

export function createMarketGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Stands',
        symbol: '⛬',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'stands-glyph-content';
            content.style.fontFamily = FONT;
            content.style.fontSize = SIZE;

            const root = document.createElement('div');
            content.appendChild(root);

            // One tooltip attach on the stable root: it delegates, so rows and
            // detail added later carry tooltips without re-attaching.
            tooltip.attach(content);

            void render({ current: null }, root);
            return content;
        },
    };
}

/** Opens the Stands glyph. */
export function openMarketGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
