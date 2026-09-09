/**
 * A pager: one of many, with a way to the next one.
 *
 * Written twice before this existed — the triplet glyph paging the attestations
 * that share a subject, predicate and context, and the stand panel paging the
 * walks past one stand. Both wanted the same four things and got them by hand:
 * arrows that dim at the ends, a counter saying which of how many, the left and
 * right keys, and one detail area redrawn in place.
 *
 * The caller says how to draw one. The pager owns nothing else.
 */

import { preventDrag } from '@qntx/glyphs';

export interface PagerOptions {
    /** Border colour for the arrows. Callers differ; neither should guess. */
    line?: string;
    /** Colour for the counter between them. */
    mute?: string;
    /** Class on the element one item is drawn into. */
    itemClass?: string;
}

/**
 * Draws `items` one at a time into `container`, newest choice left to the
 * caller. Returns nothing: the pager is the DOM it built.
 *
 * A single item draws no nav — one of one is not a place you can move within.
 */
export function renderPager<T>(
    container: HTMLElement,
    items: T[],
    drawOne: (into: HTMLElement, item: T) => void,
    options: PagerOptions = {},
): void {
    const line = options.line ?? 'var(--border-on-dark)';
    const mute = options.mute ?? 'var(--text-on-dark-tertiary)';

    let index = 0;

    const nav = document.createElement('div');
    nav.className = 'pager-nav';
    nav.style.display = 'flex';
    nav.style.alignItems = 'center';
    nav.style.gap = '8px';
    nav.style.marginBottom = '4px';

    const prev = document.createElement('button');
    prev.textContent = '◀';
    prev.style.cssText = 'background:none;border:1px solid ' + line + ';color:inherit;cursor:pointer;padding:2px 6px;font-size:11px;border-radius:3px';
    preventDrag(prev);

    const next = document.createElement('button');
    next.textContent = '▶';
    next.style.cssText = prev.style.cssText;
    preventDrag(next);

    const counter = document.createElement('span');
    counter.className = 'pager-counter';
    counter.style.color = mute;

    nav.append(prev, counter, next);
    if (items.length > 1) container.appendChild(nav);

    const one = document.createElement('div');
    one.className = options.itemClass ?? 'pager-item';
    container.appendChild(one);

    const show = (): void => {
        counter.textContent = `${index + 1} / ${items.length}`;
        prev.style.opacity = index === 0 ? '0.3' : '1';
        next.style.opacity = index === items.length - 1 ? '0.3' : '1';
        one.replaceChildren();
        drawOne(one, items[index]);
    };

    prev.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index > 0) { index--; show(); }
    });
    next.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index < items.length - 1) { index++; show(); }
    });

    container.tabIndex = 0;
    container.style.outline = 'none';
    container.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowLeft' && index > 0) {
            index--; show(); e.preventDefault(); e.stopPropagation();
        } else if (e.key === 'ArrowRight' && index < items.length - 1) {
            index++; show(); e.preventDefault(); e.stopPropagation();
        }
    });

    if (items.length > 0) show();
}
