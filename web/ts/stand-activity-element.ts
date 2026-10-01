/**
 * Stand Activity — what one stand has recorded (ADR-035).
 *
 * The Stands element says what a stand IS: the door it inherits, who created it,
 * its defining attestation, the snippet to paste. What it has SEEN is a
 * different question asked at a different time, and it is a dataset rather than
 * a fact — so it is opened as its own panel and given the room a dataset needs.
 * In the stand's own panel these were two comma-joined lines that ran off the
 * right edge and took their tails with them.
 */

import type { Element } from '@teranos/elements';
import { renderPager } from './components/pager.ts';
import { renderSparklines, windowOf } from './components/sparkline.ts';
import { renderDoughnut, type Slice } from './components/doughnut.ts';
import { apiJson } from './client/http';
import { openPageElement } from './page-element.ts';
import { renderPredicate } from './components/element/attestation-triple.ts';
import { openPredicateElement } from './components/element/predicate-element.ts';
import type { StaandInfo, StandSeen, StandStep, StandWalk } from './market-element.ts';

// Literals, not references to another module's constants: the bundler resolves
// a const that points at an imported const to undefined (web/CLAUDE.md).
const FONT = 'var(--font-mono)';
const SIZE = '13px';
const EDGE = '12px';
const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** One element per stand, so its id says which stand it is about. */
export function standElementId(market: string, slug: string): string {
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
 * A page you can press. It opens that page's element — what the stand saw on it —
 * rather than the page itself, because the question is what happened there. The
 * page is one link on the element that opens, and that link is underlined.
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
        openPageElement(s, page, site);
    });
    return cell;
}

/**
 * A predicate you can press. It opens that predicate's element — what else is
 * filed under it — the way a page opens the page's.
 *
 * The label is what the stand shows; the predicate itself stays the identity,
 * so a stripped `staand:` prefix changes the reading and not the thing.
 */
export function predicateCell(predicate: string, label?: string): HTMLElement {
    return renderPredicate([predicate], {
        color: 'inherit',
        label,
        onPress: () => { openPredicateElement(predicate); },
    });
}

/** The clock part of an RFC3339 stamp, in whatever the stamp says. Times are
 *  read against each other here — the gap between two steps — so what matters
 *  is that they are all the same clock, not which one it is. */
export function clockOf(at: string): string {
    const t = at.indexOf('T');
    if (t === -1) return at;
    return at.slice(t + 1, t + 9);
}

/** The date part of an RFC3339 stamp. */
export function dayOf(at: string): string {
    const t = at.indexOf('T');
    return t === -1 ? '' : at.slice(0, t);
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
    const day = walk.steps.length > 0 ? dayOf(walk.steps[0].at) : '';
    const counted = `${walk.steps.length} step${walk.steps.length === 1 ? '' : 's'}${span === '' ? '' : ' · ' + span}`;
    size.textContent = day === '' ? counted : `${day} · ${counted}`;
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
        const stepDay = dayOf(step.at);
        at.textContent = stepDay === day ? clockOf(step.at) : `${stepDay} ${clockOf(step.at)}`;
        at.style.flexShrink = '0';
        at.style.color = MUTE;

        const page = pageCell(s, step.page, site);
        page.style.flex = '1';
        page.style.minWidth = '0';
        page.style.overflowWrap = 'break-word';
        page.style.wordBreak = 'break-word';

        const event = predicateCell(step.event, eventsOf([step.event]));
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
export function renderStandActivity(container: HTMLElement, s: StaandInfo, now: number = Date.now()): void {
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

    // Events and pages share one window, so their lines read on the same axis.
    const w = windowOf([...s.events, ...s.pages].flatMap((e) => e.seen), now);
    const seen = (rows: StandSeen[]) => rows.map((r) => ({ name: r.name, times: r.seen }));

    const events = document.createElement('div');
    events.className = 'stand-events';
    events.style.marginBottom = '18px';
    renderSparklines(events, 'Events', seen(s.events), w, (name) => predicateCell(name));
    container.appendChild(events);

    const pages = document.createElement('div');
    pages.className = 'stand-pages';
    renderSparklines(pages, 'Pages', seen(s.pages), w, (name) => pageCell(s, name, siteOf(s)));
    container.appendChild(pages);
}

/** Umami's UTM_PARAMS, in its order (src/lib/constants.ts). */
export const CAMPAIGN = ['utm_campaign', 'utm_content', 'utm_medium', 'utm_source', 'utm_term'];

/** How a stand's page views divide by one campaign parameter. */
export type CampaignReader = (s: StaandInfo, param: string) => Promise<Slice[]>;

async function readCampaign(s: StaandInfo, param: string): Promise<Slice[]> {
    const q = new URLSearchParams({ market: s.market, slug: s.slug, type: param });
    const body = await apiJson<{ counts: { name: string; count: number }[] | null }>(`/api/staands/metrics?${q}`);
    return (body.counts ?? []).map((c) => ({ name: c.name, value: c.count }));
}

/**
 * Umami's UTM report for one stand: a ring per campaign parameter, in Umami's
 * order and colors, and nothing beside it. How many is the tooltip's to say.
 */
export function renderCampaigns(container: HTMLElement, s: StaandInfo, read: CampaignReader = readCampaign): Promise<void> {
    const section = document.createElement('div');
    section.className = 'stand-campaigns';
    section.style.marginTop = '18px';

    const heading = document.createElement('div');
    heading.textContent = 'UTM';
    heading.style.color = MUTE;
    heading.style.padding = '0 0 4px';
    heading.style.borderBottom = '1px solid ' + LINE;
    heading.style.marginBottom = '8px';
    section.appendChild(heading);

    const rings = document.createElement('div');
    rings.style.display = 'flex';
    rings.style.flexWrap = 'wrap';
    rings.style.gap = '18px';
    section.appendChild(rings);
    container.appendChild(section);

    return Promise.all(CAMPAIGN.map(async (param) => {
        const cell = document.createElement('div');
        cell.className = 'stand-campaign';
        cell.dataset.param = param;
        cell.style.display = 'flex';
        cell.style.flexDirection = 'column';
        cell.style.alignItems = 'center';
        cell.style.gap = '6px';
        cell.style.width = '96px';
        // The ring's place is held while it is read, so the window it opens in
        // is measured the size it will be (@teranos/elements buttonFrom).
        cell.style.minHeight = 'calc(96px + 6px + 1.6em)';

        const name = document.createElement('div');
        // Umami's heading: the parameter without its prefix, capitalized.
        const bare = param.slice('utm_'.length);
        name.textContent = bare.charAt(0).toUpperCase() + bare.slice(1);
        cell.appendChild(name);
        rings.appendChild(cell);

        const said = document.createElement('div');
        said.style.color = MUTE;
        said.style.textAlign = 'center';
        said.style.overflowWrap = 'break-word';
        said.style.wordBreak = 'break-word';
        try {
            const ring = renderDoughnut(await read(s, param), 96, name.textContent ?? param);
            if (ring) {
                cell.appendChild(ring);
                return;
            }
            said.textContent = 'nothing recorded';
        } catch (err: unknown) {
            said.textContent = `could not read ${param}: ${err instanceof Error ? err.message : String(err)}`;
            said.style.color = 'var(--color-error)';
        }
        cell.appendChild(said);
    })).then(() => undefined);
}

/**
 * One stand's activity as its own element. An element renders its content exactly
 * once and keeps that one element for its lifetime, so the stand it is about is
 * fixed when it is made.
 *
 * "Button as another element form": it rests as the stand's Activity → button,
 * and the button itself becomes the window (market-element.ts). "It really feels
 * like a button. Until you click it."
 */
export function standActivity(s: StaandInfo): Element {
    return {
        id: standElementId(s.market, s.slug),
        title: 'Activity →',
        symbol: '⛬',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'stand-activity-content';
            content.style.fontFamily = FONT;
            content.style.fontSize = SIZE;
            content.style.padding = EDGE;
            renderStandActivity(content, s);
            void renderCampaigns(content, s);
            return content;
        },
    };
}
