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
    label: 'home',
    url: '/s/clean/boutique',
    origin: 'golem.club',
    creator: 'did:key:z6MkabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP',
    defId: 'AS-1788000000000-abcdef',
    created: '2026-09-07T14:00:00Z',
    sites: ['golem.club'],
    arrivals: 3,
    dropped: 1,
    lastSeen: '2026-09-07T14:30:00Z',
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

    test('a list row names its market and slug and opens the stand on press', () => {
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
        // recorded vs rate-limited both visible
        expect(container.textContent).toContain('3 recorded');
        expect(container.textContent).toContain('1 rate-limited');
        expect(container.textContent).toContain('Delete');
    });

    test('the long DID and defining id are carried in tooltips, not the line', () => {
        renderStandDetail(container, aStand(), noop, noopAsync);
        const tips = Array.from(container.querySelectorAll('.has-tooltip')).map((e) => e.getAttribute('data-tooltip'));
        expect(tips).toContain('did:key:z6MkabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP');
        expect(tips).toContain('AS-1788000000000-abcdef');
    });

    test('the snippet is a paste helper that names the event and hits the full URL', () => {
        const snip = standSnippet('/s/clean/boutique');
        expect(snip).toContain('window.stand');
        expect(snip).toContain('/s/clean/boutique');
        expect(snip).toContain('u.searchParams.set("e", event)');
        expect(snip).toContain('u.searchParams.set("subject"');
    });

    test('the create form asks for market, slug, label and a door — and no predicate', () => {
        const form = buildCreateForm(async () => {});
        container.appendChild(form);

        expect(container.querySelector('.stand-market')).not.toBeNull();
        expect(container.querySelector('.stand-slug')).not.toBeNull();
        expect(container.querySelector('.stand-label')).not.toBeNull();
        expect(container.querySelector('.stand-origin')).not.toBeNull();
        expect(container.querySelector('.stand-predicate')).toBeNull();
        expect(container.textContent).toContain('Create');
    });
});
