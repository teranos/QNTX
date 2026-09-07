/**
 * Market Glyph — a market's staands (ADR-035).
 */

// A staand is a market's public receive point for one predicate: a stall in the
// market. This lists the stalls that stand in the market now, and raises a new
// one from a slug, a ware and a label. Raising and striking are attestations
// (ADR-035), posted to /api/attestations, so they land in the market this
// session acts in. Choosing another market waits on per-session namespace
// selection (ADR-026).

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { createPrimaryButton, createDangerButton } from './components/button';
import { log, SEG } from './logger';

/** One staand as the market glyph sees it: the slug it answers on, the ware it
 *  writes, its label, and the URL to place. */
export interface StaandInfo {
    slug: string;
    ware: string;
    label: string;
    url: string;
}

const GLYPH_ID = 'market-glyph';

async function fetchStaands(): Promise<StaandInfo[]> {
    const body = await apiJson<{ staands: StaandInfo[] }>('/api/staands');
    return body.staands ?? [];
}

/** Raises a staand by writing its defining attestation. The node's refusal is
 *  the error the Button shows. */
async function raiseStaand(slug: string, ware: string, label: string): Promise<void> {
    await apiJson('/api/attestations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            subjects: [slug],
            predicates: ['staand:raised'],
            contexts: ['_'],
            attributes: { writes: ware, label },
            source: 'glyph',
        }),
    });
}

/** Strikes a staand: a superseding line, and arrivals stop (ADR-035). */
async function strikeStaand(slug: string): Promise<void> {
    await apiJson('/api/attestations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            subjects: [slug],
            predicates: ['staand:struck'],
            contexts: ['_'],
            source: 'glyph',
        }),
    });
}

function cell(text: string): HTMLTableCellElement {
    const td = document.createElement('td');
    td.style.padding = '4px 8px';
    td.style.wordBreak = 'break-word';
    td.style.overflowWrap = 'break-word';
    td.textContent = text;
    return td;
}

/** Exported for tests: a row per staand with what stands and the strike. */
export function renderStaands(container: HTMLElement, staands: StaandInfo[], reload: () => void): void {
    container.innerHTML = '';

    if (staands.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No staands. Nothing stands in this market yet.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'staands-table';
    table.style.borderCollapse = 'collapse';
    table.style.fontFamily = 'var(--font-mono)';

    const head = 'text-align:left;padding:4px 8px;font-weight:normal;' +
        'color:var(--text-on-dark-tertiary);border-bottom:1px solid var(--border-on-dark);';
    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>
        <th style="${head}">Slug</th>
        <th style="${head}">Ware</th>
        <th style="${head}">Label</th>
        <th style="${head}">URL</th>
        <th style="${head}"></th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const s of staands) {
        const tr = document.createElement('tr');
        tr.appendChild(cell(s.slug));
        tr.appendChild(cell(s.ware));
        tr.appendChild(cell(s.label || '—'));

        // The URL is the thing to place on a page, so it is copyable rather than
        // a link the node would follow itself.
        const url = cell(s.url);
        url.style.cursor = 'pointer';
        url.title = 'press to copy';
        url.addEventListener('click', () => {
            void navigator.clipboard.writeText(s.url).then(
                () => { const was = url.textContent; url.textContent = 'copied'; setTimeout(() => { url.textContent = was; }, 1200); },
                () => { const was = url.textContent; url.textContent = 'refused'; setTimeout(() => { url.textContent = was; }, 1200); },
            );
        });
        tr.appendChild(url);

        const action = document.createElement('td');
        action.style.padding = '4px 8px';
        action.style.textAlign = 'right';
        const strike = createDangerButton('Strike', 'Confirm strike', async () => {
            await strikeStaand(s.slug);
            reload();
        });
        action.appendChild(strike.element);
        tr.appendChild(action);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

/** Exported for tests: the raise form is a slug, a ware, a label and the act.
 *  Empty slug or ware throws, so the Button shows it. */
export function buildRaiseForm(reload: () => void): HTMLElement {
    const form = document.createElement('div');
    form.className = 'staand-raise';
    form.style.display = 'flex';
    form.style.gap = '6px';
    form.style.alignItems = 'center';
    form.style.flexWrap = 'wrap';

    const input = (placeholder: string, cls: string): HTMLInputElement => {
        const el = document.createElement('input');
        el.type = 'text';
        el.placeholder = placeholder;
        el.className = cls;
        el.style.fontFamily = 'var(--font-mono)';
        el.style.padding = '4px 8px';
        form.appendChild(el);
        return el;
    };

    const slug = input('slug', 'staand-slug');
    const ware = input('ware, e.g. page:seen', 'staand-ware');
    const label = input('label', 'staand-label');

    const raise = createPrimaryButton('Raise', async () => {
        const s = slug.value.trim();
        const w = ware.value.trim();
        if (s === '' || w === '') {
            throw new Error('a staand needs a slug and a ware');
        }
        await raiseStaand(s, w, label.value.trim());
        slug.value = '';
        ware.value = '';
        label.value = '';
        reload();
    });
    form.appendChild(raise.element);
    return form;
}

// render lists the market and mounts the raise form. Every failure the node
// hands back is shown where it happened: a refused raise or strike surfaces on
// its Button, and a refused list — the first one or the one after an act —
// paints here. The error is data, so nothing is caught and only logged.
async function render(list: HTMLElement, form: HTMLElement): Promise<void> {
    const reload = () => { void render(list, form); };
    try {
        const staands = await fetchStaands();
        renderStaands(list, staands, reload);
        form.replaceChildren(...buildRaiseForm(reload).childNodes);
    } catch (err: unknown) {
        showRefusal(list, err);
    }
}

// showRefusal logs and shows: logging alone is hiding. The message lands in the
// glyph, copyable, so the operator reads exactly what the node said.
function showRefusal(list: HTMLElement, err: unknown): void {
    log.error(SEG.UI, '[MarketGlyph] the node refused', err);
    const message = `the node refused: ${err instanceof Error ? err.message : String(err)}`;
    list.innerHTML = '';
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
    list.appendChild(box);
}

export function createMarketGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Market',
        symbol: '⛬',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'market-glyph-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '10px';
            content.style.padding = '12px';

            const form = document.createElement('div');
            content.appendChild(form);

            const list = document.createElement('div');
            list.className = 'staands-list';
            list.innerHTML = '<div class="glyph-loading">Loading staands…</div>';
            content.appendChild(list);

            void render(list, form);

            return content;
        },
    };
}

/** Opens the Market glyph. */
export function openMarketGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
