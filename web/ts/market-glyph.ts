/**
 * Market Glyph — a market's staands (ADR-035).
 */

// A staand is a public receive point for one predicate. You pick a market (a
// namespace, never system or default), see the staands standing in it, and
// create new ones. Create and delete write into that market through
// /api/staands, ROOT only. Each row hands you the full URL and an <img> snippet
// to paste, the way an analytics tag does.

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { apiJson } from './client/http';
import { backendUrl } from './client/url';
import { createPrimaryButton, createDangerButton } from './components/button';
import { log, SEG } from './logger';

/** One staand as the market glyph sees it. */
export interface StaandInfo {
    slug: string;
    predicate: string;
    label: string;
    url: string;
}

const GLYPH_ID = 'market-glyph';

async function fetchStaands(market: string): Promise<StaandInfo[]> {
    const body = await apiJson<{ staands: StaandInfo[] }>(`/api/staands?market=${encodeURIComponent(market)}`);
    return body.staands ?? [];
}

/** Creates a staand in a market. The node's refusal is the error the Button shows. */
async function createStaand(market: string, slug: string, predicate: string, label: string): Promise<void> {
    await apiJson('/api/staands', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ market, slug, predicate, label }),
    });
}

/** Deletes a staand: it stops recording, and the record stays (ADR-026). */
async function deleteStaand(market: string, slug: string): Promise<void> {
    await apiJson(`/api/staands?market=${encodeURIComponent(market)}&slug=${encodeURIComponent(slug)}`, {
        method: 'DELETE',
    });
}

/** The full URL of a staand's pixel, host and all — the thing you paste. */
export function fullURL(url: string): string {
    return backendUrl() + url;
}

/** The <img> snippet to drop on a page. The subject defaults to the slug, so it
 *  works pasted as-is and records that predicate about that subject. */
export function snippet(url: string, slug: string): string {
    return `<img src="${fullURL(url)}?subject=${encodeURIComponent(slug)}" alt="" width="1" height="1" style="position:absolute;left:-9999px">`;
}

function cell(text: string): HTMLTableCellElement {
    const td = document.createElement('td');
    td.style.padding = '4px 8px';
    td.style.wordBreak = 'break-word';
    td.style.overflowWrap = 'break-word';
    td.textContent = text;
    return td;
}

/** A cell whose text copies to the clipboard on click. */
function copyCell(shown: string, toCopy: string): HTMLTableCellElement {
    const td = cell(shown);
    td.style.cursor = 'pointer';
    td.title = 'press to copy';
    td.addEventListener('click', () => {
        void navigator.clipboard.writeText(toCopy).then(
            () => { td.textContent = 'copied'; setTimeout(() => { td.textContent = shown; }, 1200); },
            () => { td.textContent = 'refused'; setTimeout(() => { td.textContent = shown; }, 1200); },
        );
    });
    return td;
}

/** Exported for tests: a row per staand — what it records, the full URL, the
 *  paste snippet, and delete. */
export function renderStaands(container: HTMLElement, market: string, staands: StaandInfo[], reload: () => void): void {
    container.innerHTML = '';

    if (staands.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No staands in this market yet.';
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
        <th style="${head}">Predicate</th>
        <th style="${head}">Label</th>
        <th style="${head}">URL</th>
        <th style="${head}">Snippet</th>
        <th style="${head}"></th>
    </tr>`;
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const s of staands) {
        const tr = document.createElement('tr');
        tr.appendChild(cell(s.slug));
        tr.appendChild(cell(s.predicate));
        tr.appendChild(cell(s.label || '—'));
        tr.appendChild(copyCell(fullURL(s.url), fullURL(s.url)));
        tr.appendChild(copyCell('copy snippet', snippet(s.url, s.slug)));

        const action = document.createElement('td');
        action.style.padding = '4px 8px';
        action.style.textAlign = 'right';
        const del = createDangerButton('Delete', 'Confirm delete', async () => {
            await deleteStaand(market, s.slug);
            reload();
        });
        action.appendChild(del.element);
        tr.appendChild(action);

        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

/** Exported for tests: the create form — slug, predicate, label, and the act.
 *  An empty slug or predicate throws, so the Button shows it. */
export function buildCreateForm(market: string, reload: () => void): HTMLElement {
    const form = document.createElement('div');
    form.className = 'staand-create';
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
    const predicate = input('predicate, e.g. page:seen', 'staand-predicate');
    const label = input('label', 'staand-label');

    const create = createPrimaryButton('Create', async () => {
        const s = slug.value.trim();
        const p = predicate.value.trim();
        if (s === '' || p === '') {
            throw new Error('a staand needs a slug and a predicate');
        }
        await createStaand(market, s, p, label.value.trim());
        slug.value = '';
        predicate.value = '';
        label.value = '';
        reload();
    });
    form.appendChild(create.element);
    return form;
}

interface MarketState { market: string }

// render lists the chosen market and mounts the create form. Every failure the
// node hands back is shown where it happened: a refused create or delete
// surfaces on its Button, a refused list paints here. Nothing is only logged.
async function render(state: MarketState, list: HTMLElement, form: HTMLElement): Promise<void> {
    const reload = () => { void render(state, list, form); };

    if (state.market === '') {
        form.replaceChildren();
        list.innerHTML = '';
        const hint = document.createElement('div');
        hint.className = 'glyph-loading';
        hint.textContent = 'Enter a market to see and create its staands.';
        list.appendChild(hint);
        return;
    }

    try {
        const staands = await fetchStaands(state.market);
        renderStaands(list, state.market, staands, reload);
        form.replaceChildren(...buildCreateForm(state.market, reload).childNodes);
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

            const state: MarketState = { market: '' };

            const marketInput = document.createElement('input');
            marketInput.type = 'text';
            marketInput.className = 'market-name';
            marketInput.placeholder = 'market — a namespace, never system or default';
            marketInput.style.fontFamily = 'var(--font-mono)';
            marketInput.style.padding = '4px 8px';
            content.appendChild(marketInput);

            const form = document.createElement('div');
            content.appendChild(form);

            const list = document.createElement('div');
            list.className = 'staands-list';
            content.appendChild(list);

            marketInput.addEventListener('change', () => {
                state.market = marketInput.value.trim();
                void render(state, list, form);
            });

            void render(state, list, form);

            return content;
        },
    };
}

/** Opens the Market glyph. */
export function openMarketGlyph(): void {
    glyphRun.openGlyph(GLYPH_ID);
}
