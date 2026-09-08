/**
 * @jest-environment jsdom
 *
 * Stands glyph — a list row, a stand opened, and the create form (ADR-035).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderStandList, renderStandDetail, buildCreateForm, standSnippet, type StaandInfo } from './market-glyph.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const noop = (): void => {};
const noopAsync = async (): Promise<void> => {};

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
    walks: [],
    ...over,
});

describe('Stands glyph', () => {
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

    test('a list row names its namespace and slug and opens the stand on press', () => {
        let opened: StaandInfo | null = null;
        renderStandList(container, [aStand()], (s) => { opened = s; });

        expect(container.textContent).toContain('clean');
        expect(container.textContent).toContain('boutique');
        (container.querySelector('.stand-row') as HTMLElement).click();
        expect(opened).not.toBeNull();
        expect(opened!.slug).toBe('boutique');
    });

    test('an empty list says so', () => {
        renderStandList(container, [], noop);
        expect(container.textContent).toContain('No stands yet');
    });

    test('an opened stand shows its door, creator, activity, defining id, and delete', () => {
        renderStandDetail(container, aStand(), noop, noopAsync);

        expect(container.textContent).toContain('Door');
        expect(container.textContent).toContain('golem.club');
        expect(container.textContent).toContain('Created by');
        expect(container.textContent).toContain('Defined by');
        expect(container.textContent).toContain('3 recorded');
        expect(container.textContent).toContain('2 visitors');
        expect(container.textContent).toContain('1 rate-limited');
        expect(container.textContent).toContain('Delete');
    });

    test('the snippet is collapsed once the stand has data, open while it is quiet', () => {
        renderStandDetail(container, aStand({ arrivals: 3 }), noop, noopAsync);
        const withData = container.querySelector('details.stand-snippet, details') as HTMLDetailsElement;
        expect(withData.open).toBe(false);

        document.body.innerHTML = '';
        const fresh = document.createElement('div');
        document.body.appendChild(fresh);
        renderStandDetail(fresh, aStand({ arrivals: 0, visitors: 0, events: [], pages: [], sites: [], dropped: 0, lastSeen: '' }), noop, noopAsync);
        const quiet = fresh.querySelector('details') as HTMLDetailsElement;
        expect(quiet.open).toBe(true);
    });

    test('an opened stand hands its activity to its own panel, not a fact row', () => {
        renderStandDetail(container, aStand(), noop, noopAsync);

        // The tallies are a dataset and live in the activity panel. What belongs
        // here is the way in, and none of the entries themselves.
        expect(container.textContent).toContain('Activity →');
        expect(container.textContent).not.toContain('staand:page_view');
        expect(container.textContent).not.toContain('/deep-clean');
        expect(container.querySelectorAll('.stand-tally').length).toBe(0);
    });

    test('a stand with nothing recorded offers no way into an empty panel', () => {
        renderStandDetail(container, aStand({ events: [], pages: [] }), noop, noopAsync);
        expect(container.textContent).not.toContain('Activity →');
    });

    test('the long DID and defining id are carried in tooltips, not truncated away', () => {
        renderStandDetail(container, aStand(), noop, noopAsync);
        const tips = Array.from(container.querySelectorAll('.has-tooltip')).map((e) => e.getAttribute('data-tooltip'));
        expect(tips).toContain('did:key:z6MkabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP');
        expect(tips).toContain('AS-1788000000000-abcdef');
    });

    test('the snippet names the event, sends the page as subject and the id as v', () => {
        const snip = standSnippet('/s/clean/boutique');
        expect(snip).toContain('window.stand');
        expect(snip).toContain('/s/clean/boutique');
        expect(snip).toContain('u.searchParams.set("e", event)');
        expect(snip).toContain('u.searchParams.set("page", location.pathname)');
        expect(snip).toContain('u.searchParams.set("v", id)');
        expect(snip).toContain('stand("page_view")');
    });

    test('the create form is a live URL: a namespace dropdown, a prefilled slug, no label, no door', () => {
        const form = buildCreateForm(['clean', 'haarlem'], new Set<string>(), noopAsync);
        container.appendChild(form);

        const ns = container.querySelector('.stand-market') as HTMLSelectElement | null;
        expect(ns).not.toBeNull();
        expect(ns!.tagName).toBe('SELECT');
        expect(Array.from(ns!.options).map((o) => o.value)).toContain('clean');

        const slug = container.querySelector('.stand-slug') as HTMLInputElement | null;
        expect(slug).not.toBeNull();
        expect(slug!.value.length).toBeGreaterThan(0); // prefilled from the pool

        // The two fields that were never approved / moved to the namespace are gone.
        expect(container.querySelector('.stand-label')).toBeNull();
        expect(container.querySelector('.stand-origin')).toBeNull();

        // The address is shown as a URL, /s/ and all.
        expect(container.textContent).toContain('/s/');
        expect(container.textContent).toContain('Create');
    });

    test('the prefilled slug avoids one already in use', () => {
        const used = new Set<string>(['kiosk']); // first in the pool
        const form = buildCreateForm(['clean'], used, noopAsync);
        const slug = form.querySelector('.stand-slug') as HTMLInputElement;
        expect(slug.value).not.toBe('kiosk');
    });
});
