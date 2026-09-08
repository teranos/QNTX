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
import type { StaandInfo, StandCount, StandStep, StandWalk } from './market-glyph.ts';

const GLYPH_ID = 'stand-activity-glyph';

// Literals, not references to another module's constants: the bundler resolves
// a const that points at an imported const to undefined (web/CLAUDE.md).
const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

// Which stand the panel is about. A glyph is opened by id and carries no
// argument, so the caller names the stand here and opens it in the same breath.
let showing: StaandInfo | null = null;

/** Opens the activity panel for one stand. Called from the Stands glyph. */
export function openStandActivity(s: StaandInfo): void {
    showing = s;
    glyphRun.openGlyph(GLYPH_ID);
}

/** Exported for tests: the stand the panel would draw right now. */
export function standShowing(): StaandInfo | null {
    return showing;
}

/** A run of consecutive steps on one page. A walk changes page when the person
 *  does, so the run is the unit a colour is worth spending on. */
export interface PageRun {
    page: string;
    at: string;
    events: string[];
}

/** Consecutive steps on the same page folded into one run, in order. Returning
 *  to a page later is a new run, not the same one — coming back is a thing the
 *  person did and the walk has to keep it. */
export function pageRuns(steps: StandStep[]): PageRun[] {
    const runs: PageRun[] = [];
    for (const step of steps) {
        const last = runs.length > 0 ? runs[runs.length - 1] : null;
        if (last !== null && last.page === step.page) {
            last.events.push(step.event);
        } else {
            runs.push({ page: step.page, at: step.at, events: [step.event] });
        }
    }
    return runs;
}

/** A hue for a page, the same one every time. Colour carries which page across
 *  every walk in the panel, so nine people converging on one page is a colour
 *  they share rather than a name each of them repeats. */
export function pageHue(page: string): number {
    let h = 0;
    for (let i = 0; i < page.length; i++) {
        h = (h * 31 + page.charCodeAt(i)) % 360;
    }
    return h;
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
export function renderWalk(container: HTMLElement, walk: StandWalk): void {
    const head = document.createElement('div');
    head.style.display = 'flex';
    head.style.alignItems = 'baseline';
    head.style.gap = '10px';
    head.style.marginBottom = '3px';

    const when = document.createElement('span');
    when.textContent = walk.steps.length > 0 ? clockOf(walk.steps[0].at) : '';
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

    head.appendChild(when);
    head.appendChild(who);
    head.appendChild(size);
    container.appendChild(head);

    for (const run of pageRuns(walk.steps)) {
        const line = document.createElement('div');
        line.className = 'stand-run';
        line.style.display = 'flex';
        line.style.alignItems = 'baseline';
        line.style.gap = '8px';
        line.style.padding = '1px 0 1px 10px';

        // The colour is the page. It is the only thing on the line that says
        // "still here" or "somewhere else" without being read.
        const mark = document.createElement('span');
        mark.className = 'stand-run-mark';
        mark.textContent = '▌';
        mark.style.flexShrink = '0';
        mark.style.color = `hsl(${pageHue(run.page)}, 55%, 60%)`;

        const at = document.createElement('span');
        at.textContent = clockOf(run.at);
        at.style.flexShrink = '0';
        at.style.color = MUTE;

        const page = document.createElement('span');
        page.textContent = run.page;
        page.style.flexShrink = '0';
        page.style.overflowWrap = 'break-word';
        page.style.wordBreak = 'break-word';

        const events = document.createElement('span');
        events.textContent = eventsOf(run.events);
        events.style.flex = '1';
        events.style.minWidth = '0';
        events.style.color = MUTE;
        events.style.overflowWrap = 'break-word';
        events.style.wordBreak = 'break-word';

        line.appendChild(mark);
        line.appendChild(at);
        line.appendChild(page);
        line.appendChild(events);
        container.appendChild(line);
    }
}

/** A tally read down rather than across: one entry per line, the bar against
 *  the largest, so the shape is read without doing the division. Across, this
 *  was one string with no natural length inside a panel with a fixed width. */
export function renderTally(container: HTMLElement, label: string, items: StandCount[]): void {
    const heading = document.createElement('div');
    heading.textContent = label;
    heading.style.color = MUTE;
    heading.style.padding = '0 0 4px';
    heading.style.borderBottom = '1px solid ' + LINE;
    heading.style.marginBottom = '6px';
    container.appendChild(heading);

    if (items.length === 0) {
        const none = document.createElement('div');
        none.textContent = 'nothing recorded';
        none.style.color = MUTE;
        container.appendChild(none);
        return;
    }

    const largest = items[0].count;

    for (const item of items) {
        const line = document.createElement('div');
        line.className = 'stand-tally';
        line.style.display = 'flex';
        line.style.alignItems = 'baseline';
        line.style.gap = '10px';
        line.style.padding = '2px 0';

        const name = document.createElement('span');
        name.textContent = item.name;
        name.style.flex = '1';
        name.style.minWidth = '0';
        name.style.overflowWrap = 'break-word';
        name.style.wordBreak = 'break-word';

        // Between the name and the count, so counts line up in their own column
        // and bars in theirs however long the names run.
        const track = document.createElement('span');
        track.style.flexShrink = '0';
        track.style.width = '110px';
        track.style.height = '3px';
        track.style.borderRadius = '2px';
        track.style.background = LINE;

        const fill = document.createElement('span');
        fill.style.display = 'block';
        fill.style.height = '100%';
        fill.style.borderRadius = '2px';
        fill.style.background = MUTE;
        fill.style.width = largest > 0 ? String((item.count / largest) * 100) + '%' : '0';
        track.appendChild(fill);

        const count = document.createElement('span');
        count.textContent = String(item.count);
        count.style.flexShrink = '0';
        count.style.minWidth = '3em';
        count.style.textAlign = 'right';
        count.style.color = MUTE;

        line.appendChild(name);
        line.appendChild(track);
        line.appendChild(count);
        container.appendChild(line);
    }
}

/** Exported for tests: the panel for one stand. */
export function renderStandActivity(container: HTMLElement, s: StaandInfo | null): void {
    container.replaceChildren();

    if (s === null) {
        const empty = document.createElement('div');
        empty.className = 'glyph-loading';
        empty.textContent = 'No stand open. Open one from Stands.';
        container.appendChild(empty);
        return;
    }

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

    for (const walk of taken) {
        const one = document.createElement('div');
        one.className = 'stand-walk';
        one.style.marginBottom = '12px';
        renderWalk(one, walk);
        walks.appendChild(one);
    }

    container.appendChild(walks);

    const events = document.createElement('div');
    events.className = 'stand-events';
    events.style.marginBottom = '18px';
    renderTally(events, 'Events', s.events);
    container.appendChild(events);

    const pages = document.createElement('div');
    pages.className = 'stand-pages';
    renderTally(pages, 'Pages', s.pages);
    container.appendChild(pages);
}

export function createStandActivityGlyph(): Glyph {
    return {
        id: GLYPH_ID,
        title: 'Stand Activity',
        symbol: '⛬',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'stand-activity-content';
            content.style.fontFamily = FONT;
            content.style.fontSize = SIZE;
            content.style.padding = EDGE;
            renderStandActivity(content, showing);
            return content;
        },
        // A dataset needs room a fact row does not. Wide enough for a page path
        // and its bar on one line, tall enough for the twenty events and ten
        // pages the node will send (server/staand.go, topCounts).
        initialWidth: '720px',
        initialHeight: '560px',
    };
}
