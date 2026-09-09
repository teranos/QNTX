/**
 * @jest-environment jsdom
 *
 * Stand Activity — one stand's tallies as their own panel (ADR-035).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderStandActivity, renderTally, renderWalk, renderWalkPager, eventsOf, spanOf, standGlyphId, siteOf } from './stand-activity-glyph.ts';
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
            ['staand:page_view', '2'],
            ['staand:contact_click', '1'],
            ['/deep-clean', '2'],
            ['/', '1'],
        ]);
    });

    test('a section with nothing in it says so rather than drawing nothing', () => {
        renderStandActivity(container, aStand({ events: [], pages: [] }));
        expect(container.textContent).toContain('nothing recorded');
        expect(rowsIn(container).length).toBe(0);
    });

    test('the glyph id names the stand, so two stands are two panels', () => {
        expect(standGlyphId('clean', 'boutique')).toBe('stand-activity-clean-boutique');
        expect(standGlyphId('clean', 'home')).not.toBe(standGlyphId('clean', 'boutique'));
        expect(standGlyphId('other', 'boutique')).not.toBe(standGlyphId('clean', 'boutique'));
    });

    test('a walk is one row per event, in the order they happened', () => {
        const one = document.createElement('div');
        renderWalk(one, {
            who: 'v-1',
            steps: [
                { at: '2026-09-07T14:00:00Z', page: '/', event: 'staand:page_view' },
                { at: '2026-09-07T14:00:20Z', page: '/deep-clean', event: 'staand:page_view' },
                { at: '2026-09-07T14:00:30Z', page: '/deep-clean', event: 'staand:contact_click' },
            ],
        });
        const steps = Array.from(one.querySelectorAll('.stand-step'))
            .map((r) => Array.from(r.children).map((c) => c.textContent));
        expect(steps).toEqual([
            ['14:00:00', '/', 'page_view'],
            ['14:00:20', '/deep-clean', 'page_view'],
            ['14:00:30', '/deep-clean', 'contact_click'],
        ]);
    });

    test('walks are paged one at a time, and the counter says which of how many', () => {
        const box = document.createElement('div');
        renderWalkPager(box, [
            { who: 'v-1', steps: [{ at: '2026-09-07T14:00:00Z', page: '/one', event: 'staand:page_view' }] },
            { who: 'v-2', steps: [{ at: '2026-09-07T15:00:00Z', page: '/two', event: 'staand:page_view' }] },
            { who: 'v-3', steps: [{ at: '2026-09-07T16:00:00Z', page: '/three', event: 'staand:page_view' }] },
        ]);
        const counter = (): string | null => box.querySelector('.stand-walk-counter')?.textContent ?? null;
        const buttons = box.querySelectorAll('.stand-walk-nav button');

        expect(counter()).toBe('1 / 3');
        expect(box.querySelector('.stand-walk')?.textContent).toContain('/one');

        (buttons[1] as HTMLButtonElement).click();
        expect(counter()).toBe('2 / 3');
        expect(box.querySelector('.stand-walk')?.textContent).toContain('/two');
        expect(box.querySelector('.stand-walk')?.textContent).not.toContain('/one');

        (buttons[0] as HTMLButtonElement).click();
        expect(counter()).toBe('1 / 3');
    });

    test('one walk needs no pager', () => {
        const box = document.createElement('div');
        renderWalkPager(box, [
            { who: 'v-1', steps: [{ at: '2026-09-07T14:00:00Z', page: '/', event: 'staand:page_view' }] },
        ]);
        expect(box.querySelector('.stand-walk-nav')).toBeNull();
        expect(box.querySelector('.stand-walk')?.textContent).toContain('/');
    });

    test('a page is a link to the page, and what is not a path is not a link', () => {
        renderStandActivity(container, aStand({ sites: ['golem.club'] }));
        const links = Array.from(container.querySelectorAll('a'))
            .map((a) => (a as HTMLAnchorElement).href);
        expect(links).toContain('https://golem.club/deep-clean');
        expect(links).toContain('https://golem.club/');

        const odd = document.createElement('div');
        renderTally(odd, 'Pages', [{ name: 'firsttest', count: 1 }], 'golem.club');
        expect(odd.querySelector('a')).toBeNull();
        expect(odd.textContent).toContain('firsttest');
    });

    test('a stand nothing has reported to falls back to its door', () => {
        expect(siteOf(aStand({ sites: [], origin: 'golem.club other.example' }))).toBe('golem.club');
        expect(siteOf(aStand({ sites: [], origin: '' }))).toBe('');
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
