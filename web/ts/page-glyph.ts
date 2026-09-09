/**
 * One page of a site, as the stand saw it (ADR-035).
 *
 * Pressing a page anywhere in the stand's activity opens this, not the page —
 * the question being asked is what happened on that page, and answering it by
 * navigating away is answering a different one. The page itself is one link on
 * here, and it is underlined.
 *
 * Underline means the press leaves QNTX. Nothing internal is underlined, so a
 * line under text is the whole of the warning that the node is about to stop
 * being where you are.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { renderTally } from './components/tally.ts';
import type { StaandInfo, StandCount } from './market-glyph.ts';

const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** One glyph per page of one stand, so its id says which page it is about. */
export function pageGlyphId(market: string, slug: string, page: string): string {
    return `stand-page-${market}-${slug}-${page}`;
}

/** What the stand saw on one page. Derived from the walks it reported, which is
 *  the only place the order and the events of a page survive. */
export interface PageStats {
    page: string;
    arrivals: number;
    visitors: number;
    events: StandCount[];
    first: string;
    last: string;
}

/**
 * One page's account, folded out of the walks. The arrival count comes from the
 * stand's own page tally rather than from the walks, because the walks are what
 * the node chose to report and the tally is what it counted.
 */
export function pageStatsOf(s: StaandInfo, page: string): PageStats {
    const counted = s.pages.find((p) => p.name === page);
    const events = new Map<string, number>();
    const visitors = new Set<string>();
    let first = '';
    let last = '';

    for (const walk of s.walks ?? []) {
        for (const step of walk.steps) {
            if (step.page !== page) continue;
            visitors.add(walk.who);
            events.set(step.event, (events.get(step.event) ?? 0) + 1);
            if (first === '' || step.at < first) first = step.at;
            if (last === '' || step.at > last) last = step.at;
        }
    }

    const tally = Array.from(events, ([name, count]) => ({ name, count }));
    tally.sort((a, b) => (b.count - a.count) || a.name.localeCompare(b.name));

    return {
        page,
        arrivals: counted?.count ?? 0,
        visitors: visitors.size,
        events: tally,
        first,
        last,
    };
}

/**
 * The page itself, as a link out. Underlined, because pressing it leaves the
 * node; `rel` is set because a page a stranger's site names is a stranger's
 * page. Empty when there is no site to build a URL from.
 */
export function externalLink(site: string, page: string): HTMLElement | null {
    if (site === '' || !page.startsWith('/')) return null;
    const link = document.createElement('a');
    link.className = 'external-link';
    link.textContent = 'https://' + site + page;
    link.href = 'https://' + site + page;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.style.color = 'inherit';
    link.style.textDecoration = 'underline';
    link.style.overflowWrap = 'break-word';
    link.style.wordBreak = 'break-word';
    return link;
}

/** Exported for tests: what one page's glyph draws. */
export function renderPageStats(container: HTMLElement, s: StaandInfo, page: string, site: string): void {
    container.replaceChildren();

    const stats = pageStatsOf(s, page);

    const title = document.createElement('div');
    title.textContent = page;
    title.style.fontSize = '15px';
    title.style.marginBottom = '2px';
    title.style.overflowWrap = 'break-word';
    title.style.wordBreak = 'break-word';
    container.appendChild(title);

    const summary = document.createElement('div');
    const parts = [`${stats.arrivals} recorded`];
    if (stats.visitors > 0) parts.push(`${stats.visitors} visitor${stats.visitors === 1 ? '' : 's'}`);
    if (stats.first !== '') parts.push(`first ${stats.first}`);
    if (stats.last !== '' && stats.last !== stats.first) parts.push(`last ${stats.last}`);
    summary.textContent = parts.join(' · ');
    summary.style.color = MUTE;
    summary.style.marginBottom = '14px';
    container.appendChild(summary);

    const out = externalLink(site, page);
    if (out !== null) {
        const row = document.createElement('div');
        row.style.marginBottom = '18px';
        row.style.paddingBottom = '10px';
        row.style.borderBottom = '1px solid ' + LINE;
        row.appendChild(out);
        container.appendChild(row);
    }

    const events = document.createElement('div');
    events.className = 'page-events';
    renderTally(events, 'Events on this page', stats.events);
    container.appendChild(events);
}

/** Opens one page as its own glyph. The stand and the page are fixed when it is
 *  made, the way a stand's activity is (stand-activity-glyph.ts). */
export function openPageGlyph(s: StaandInfo, page: string, site: string): void {
    const glyphId = pageGlyphId(s.market, s.slug, page);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: page,
        symbol: '⌸',
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'page-glyph-content';
            content.style.fontFamily = FONT;
            content.style.fontSize = SIZE;
            content.style.padding = EDGE;
            renderPageStats(content, s, page, site);
            return content;
        },
        initialWidth: '560px',
        initialHeight: '420px',
    } satisfies Glyph);

    glyphRun.openGlyph(glyphId);
}
