/**
 * @jest-environment jsdom
 *
 * Stand Activity — one stand's tallies as their own panel (ADR-035).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderStandActivity, renderTally, renderWalk, pageRuns, eventsOf, spanOf, pageHue } from './stand-activity-glyph.ts';
import type { StaandInfo } from './market-glyph.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const aStand = (over: Partial<StaandInfo> = {}): StaandInfo => ({
    slug: 'boutique',
    market: 'clean',
    url: '/s/clean/boutique',
    origin: 'golem.club',
    creator: 'did:key:z6MkabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP',
    defId: 'AS-1788000000000-abcdef',
    created: '2026-09-07T14:00:00Z',
    sites: ['golem.club'],
    arrivals: 3,
    visitors: 2,
    dropped: 1,
    lastSeen: '2026-09-07T14:30:00Z',
    events: [{ name: 'staand:page_view', count: 2 }, { name: 'staand:contact_click', count: 1 }],
    pages: [{ name: '/deep-clean', count: 2 }, { name: '/', count: 1 }],
    walks: [{
        who: 'v-1',
        steps: [
            { at: '2026-09-07T14:00:00Z', page: '/', event: 'staand:page_view' },
            { at: '2026-09-07T14:00:20Z', page: '/deep-clean', event: 'staand:page_view' },
            { at: '2026-09-07T14:00:30Z', page: '/deep-clean', event: 'staand:contact_click' },
        ],
    }],
    ...over,
});

const rowsIn = (container: HTMLElement): (string | null)[][] =>
    Array.from(container.querySelectorAll('.stand-tally'))
        .map((row) => Array.from(row.children).map((cell) => cell.textContent));

describe('Stand Activity panel', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('names the stand it is about and how much there is to read', () => {
        renderStandActivity(container, aStand());
        expect(container.textContent).toContain('clean / boutique');
        expect(container.textContent).toContain('3 recorded');
        expect(container.textContent).toContain('2 visitors');
        expect(container.textContent).toContain('1 rate-limited');
    });

    test('one line per entry, name and count on the same row', () => {
        renderStandActivity(container, aStand());
        expect(rowsIn(container)).toEqual([
            ['staand:page_view', '', '2'],
            ['staand:contact_click', '', '1'],
            ['/deep-clean', '', '2'],
            ['/', '', '1'],
        ]);
    });

    test('the bar is drawn against the largest entry, not the total', () => {
        const tally = document.createElement('div');
        renderTally(tally, 'Events', [{ name: 'a', count: 8 }, { name: 'b', count: 2 }]);
        const fills = Array.from(tally.querySelectorAll('.stand-tally'))
            .map((row) => (row.children[1].firstElementChild as HTMLElement).style.width);
        expect(fills).toEqual(['100%', '25%']);
    });

    test('a section with nothing in it says so rather than drawing nothing', () => {
        renderStandActivity(container, aStand({ events: [], pages: [] }));
        expect(container.textContent).toContain('nothing recorded');
        expect(rowsIn(container).length).toBe(0);
    });

    test('opened with no stand, it says which door to come in by', () => {
        renderStandActivity(container, null);
        expect(container.textContent).toContain('Open one from Stands');
    });

    test('a walk is one line per page, in the order the person was on them', () => {
        const one = document.createElement('div');
        renderWalk(one, {
            who: 'v-1',
            steps: [
                { at: '2026-09-07T14:00:00Z', page: '/', event: 'staand:page_view' },
                { at: '2026-09-07T14:00:20Z', page: '/deep-clean', event: 'staand:page_view' },
                { at: '2026-09-07T14:00:30Z', page: '/deep-clean', event: 'staand:contact_click' },
            ],
        });
        const runs = Array.from(one.querySelectorAll('.stand-run'))
            .map((r) => Array.from(r.children).map((c) => c.textContent));
        expect(runs).toEqual([
            ['▌', '14:00:00', '/', 'page_view'],
            ['▌', '14:00:20', '/deep-clean', 'page_view · contact_click'],
        ]);
    });

    test('coming back to a page is a second run, not a bigger first one', () => {
        expect(pageRuns([
            { at: 'a', page: '/', event: 'e1' },
            { at: 'b', page: '/x', event: 'e2' },
            { at: 'c', page: '/', event: 'e3' },
        ]).map((r) => r.page)).toEqual(['/', '/x', '/']);
    });

    test('a page keeps its colour wherever it appears', () => {
        expect(pageHue('/deep-clean')).toBe(pageHue('/deep-clean'));
        expect(pageHue('/deep-clean')).not.toBe(pageHue('/'));
    });

    test('repeats fold and order survives', () => {
        expect(eventsOf(['staand:page_view', 'staand:page_view', 'staand:csa_locked']))
            .toBe('page_view ×2 · csa_locked');
    });

    test('a walk of one step has no span to report', () => {
        expect(spanOf([{ at: '2026-09-07T14:00:00Z', page: '/', event: 'e' }])).toBe('');
        expect(spanOf([
            { at: '2026-09-07T14:00:00Z', page: '/', event: 'e' },
            { at: '2026-09-07T14:03:12Z', page: '/', event: 'e' },
        ])).toBe('3m12s');
    });

    test('one block per walk, and no walk invented for arrivals nobody named', () => {
        renderStandActivity(container, aStand());
        expect(container.querySelectorAll('.stand-walk').length).toBe(1);
    });
});
