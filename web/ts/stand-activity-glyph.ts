/**
 * Stand Activity — what one stand has recorded (ADR-035).
 *
 * The Stands glyph says what a stand IS: the door it inherits, who created it,
 * its defining attestation, the snippet to paste. What it has SEEN is a
 * different question asked at a different time, and it is a dataset rather than
 * a fact — so it is opened as its own panel and given the room a dataset needs.
 * In the stand's own panel these were two comma-joined lines that ran off the
 * right edge and took their tails with them.
 */

import type { Glyph } from '@qntx/glyphs';
import { glyphRun } from '@qntx/glyphs';
import { renderPager } from './components/pager.ts';
import { renderTally } from './components/tally.ts';
import { openPageGlyph } from './page-glyph.ts';
import type { StaandInfo, StandStep, StandWalk } from './market-glyph.ts';

// Literals, not references to another module's constants: the bundler resolves
// a const that points at an imported const to undefined (web/CLAUDE.md).
const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** One glyph per stand, so its id says which stand it is about. */
export function standGlyphId(market: string, slug: string): string {
    return `stand-activity-${market}-${slug}`;
}

/**
 * The site a stand's pages belong to: the host that actually reported, or the
 * door the stand inherits when nothing has reported yet. Empty when neither
 * names one, and then a page is text rather than a link.
 */
export function siteOf(s: StaandInfo): string {
    if (s.sites.length > 0) return s.sites[0];
    const door = s.origin.trim();
    if (door === '') return '';
    const first = door.indexOf(' ');
    return first === -1 ? door : door.slice(0, first);
}

/**
 * A page you can press. It opens that page's glyph — what the stand saw on it —
 * rather than the page itself, because the question is what happened there. The
 * page is one link on the glyph that opens, and that link is underlined.
 *
 * Only a path is pressable. Early snippet generations sent visitor ids and words
 * like `firsttest` as the page, and there is nothing to say about those.
 */
export function pageCell(s: StaandInfo, page: string, site: string): HTMLElement {
    const cell = document.createElement('span');
    cell.textContent = page;
    if (!page.startsWith('/')) return cell;

    cell.className = 'stand-page-open';
    cell.style.cursor = 'pointer';
    cell.addEventListener('click', (e) => {
        e.stopPropagation();
        openPageGlyph(s, page, site);
    });
    return cell;
}

/** The clock part of an RFC3339 stamp, in whatever the stamp says. Times are
 *  read against each other here — the gap between two steps — so what matters
 *  is that they are all the same clock, not which one it is. */
export function clockOf(at: string): string {
    const t = at.indexOf('T');
    if (t === -1) return at;
    return at.slice(t + 1, t + 9);
}

/** How long a walk lasted, from its first step to its last. */
export function spanOf(steps: StandStep[]): string {
    if (steps.length < 2) return '';
    const from = Date.parse(steps[0].at);
    const to = Date.parse(steps[steps.length - 1].at);
    if (Number.isNaN(from) || Number.isNaN(to)) return '';
    const secs = Math.round((to - from) / 1000);
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m${secs % 60}s`;
    return `${Math.floor(secs / 3600)}h${Math.floor((secs % 3600) / 60)}m`;
}

/** Repeats folded, order kept: `page_view ×2 · hover` rather than three names.
 *  The event vocabulary is the pixel side's, and `staand:` is on every one of
 *  them, so it is dropped — a prefix on everything distinguishes nothing. */
export function eventsOf(events: string[]): string {
    const out: string[] = [];
    let run = '';
    let n = 0;
    const flush = (): void => {
        if (run === '') return;
        const name = run.startsWith('staand:') ? run.slice(7) : run;
        out.push(n > 1 ? `${name} ×${n}` : name);
    };
    for (const e of events) {
        if (e === run) { n++; continue; }
        flush();
        run = e;
        n = 1;
    }
    flush();
    return out.join(' · ');
}

/** Exported for tests: one person's walk past the stand. The stand stands; this
 *  is what moved past it. One line per page they were on, in order, so coming
 *  back to a page reads as coming back rather than as a bigger number. */
export function renderWalk(container: HTMLElement, s: StaandInfo, walk: StandWalk): void {
    const site = siteOf(s);
    const head = document.createElement('div');
    head.style.display = 'flex';
    head.style.alignItems = 'baseline';
    head.style.gap = '10px';
    head.style.marginBottom = '3px';

    const who = document.createElement('span');
    who.textContent = walk.who;
    who.style.flex = '1';
    who.style.minWidth = '0';
    who.style.overflowWrap = 'break-word';
    who.style.wordBreak = 'break-word';
    who.style.color = MUTE;

    const size = document.createElement('span');
    const span = spanOf(walk.steps);
    size.textContent = `${walk.steps.length} step${walk.steps.length === 1 ? '' : 's'}${span === '' ? '' : ' · ' + span}`;
    size.style.flexShrink = '0';
    size.style.color = MUTE;

    head.appendChild(who);
    head.appendChild(size);
    container.appendChild(head);

    for (const step of walk.steps) {
        const line = document.createElement('div');
        line.className = 'stand-step';
        line.style.display = 'flex';
        line.style.gap = '12px';
        line.style.padding = '1px 0';

        const at = document.createElement('span');
        at.textContent = clockOf(step.at);
        at.style.flexShrink = '0';
        at.style.color = MUTE;

        const page = pageCell(s, step.page, site);
        page.style.flex = '1';
        page.style.minWidth = '0';
        page.style.overflowWrap = 'break-word';
        page.style.wordBreak = 'break-word';

        const event = document.createElement('span');
        event.textContent = eventsOf([step.event]);
        event.style.flexShrink = '0';
        event.style.color = MUTE;

        line.appendChild(at);
        line.appendChild(page);
        line.appendChild(event);
        container.appendChild(line);
    }
}

/** Walks one at a time. The paging is the shared one (components/pager.ts);
 *  what a walk looks like is the only part that belongs here. */
export function renderWalkPager(container: HTMLElement, s: StaandInfo, walks: StandWalk[]): void {
    renderPager(container, walks, (into, walk) => { renderWalk(into, s, walk); },
        { line: LINE, mute: MUTE, itemClass: 'stand-walk' });
}

/** Exported for tests: the panel for one stand. */
export function renderStandActivity(container: HTMLElement, s: StaandInfo): void {
    container.replaceChildren();

    const title = document.createElement('div');
    title.textContent = s.market + ' / ' + s.slug;
    title.style.fontSize = '15px';
    title.style.marginBottom = '2px';
    container.appendChild(title);

    // How much there is to read, before reading it. The one count that earns
    // its place here: it sizes the thing rather than standing in for it.
    const summary = document.createElement('div');
    const parts = [`${s.arrivals} recorded`];
    if (s.visitors > 0) parts.push(`${s.visitors} visitor${s.visitors === 1 ? '' : 's'}`);
    if (s.dropped > 0) parts.push(`${s.dropped} rate-limited`);
    if (s.lastSeen !== '') parts.push(`last ${s.lastSeen}`);
    summary.textContent = parts.join(' · ');
    summary.style.color = MUTE;
    summary.style.marginBottom = '14px';
    container.appendChild(summary);

    // The bundle and the node ship separately, so this page meets a node that
    // has never heard of a walk and sends none.
    const taken = s.walks ?? [];

    const walks = document.createElement('div');
    walks.className = 'stand-walks';
    walks.style.marginBottom = '18px';

    const heading = document.createElement('div');
    heading.textContent = 'Walks';
    heading.style.color = MUTE;
    heading.style.padding = '0 0 4px';
    heading.style.borderBottom = '1px solid ' + LINE;
    heading.style.marginBottom = '8px';
    walks.appendChild(heading);

    if (taken.length === 0) {
        const none = document.createElement('div');
        none.textContent = s.arrivals > 0
            ? 'this node records arrivals but does not yet report walks'
            : 'nobody walked past yet';
        none.style.color = MUTE;
        walks.appendChild(none);
    }

    if (taken.length > 0) {
        renderWalkPager(walks, s, taken);
    }

    container.appendChild(walks);

    const events = document.createElement('div');
    events.className = 'stand-events';
    events.style.marginBottom = '18px';
    renderTally(events, 'Events', s.events);
    container.appendChild(events);

    const pages = document.createElement('div');
    pages.className = 'stand-pages';
    renderTally(pages, 'Pages', s.pages, (name) => pageCell(s, name, siteOf(s)));
    container.appendChild(pages);
}

/**
 * Opens one stand's activity as its own glyph, the way a token opens as its own
 * (token-glyph.ts). A glyph renders its content exactly once and keeps that one
 * element for its lifetime, so the stand it is about is fixed when it is made.
 *
 * That is why there is no module-level "which stand" and no glyph registered at
 * startup: a stand-less activity panel could only ever draw an empty one, and a
 * shared panel would show the first stand opened forever.
 */
export function openStandActivity(s: StaandInfo): void {
    const glyphId = standGlyphId(s.market, s.slug);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: s.market + ' / ' + s.slug,
        symbol: '⛬',
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'stand-activity-content';
            content.style.fontFamily = FONT;
            content.style.fontSize = SIZE;
            content.style.padding = EDGE;
            renderStandActivity(content, s);
            return content;
        },
        // A dataset needs room a fact row does not. Wide enough for a page path
        // and its bar on one line, tall enough for the twenty events and ten
        // pages the node will send (server/staand.go, topCounts).
        initialWidth: '720px',
        initialHeight: '560px',
    } satisfies Glyph);

    glyphRun.openGlyph(glyphId);
}
