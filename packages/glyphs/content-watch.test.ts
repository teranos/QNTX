/**
 * Tests for the content watch.
 *
 * The behaviour under test is the one that reached a phone screen: a window
 * manifested, its chrome committed, and its body never arrived — 44px of title
 * bar over 16px of padding, with nothing thrown and nothing logged. These are
 * the assertions that would have failed instead.
 *
 * Personas:
 * - Tim: Happy path — a body that draws is present, at mount or later
 * - Spike: Edge cases — a body that never draws is refused, and says so
 * - Jenny: Complex scenarios — the glyph's own word, re-arming, and the stash
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import type { Glyph } from './glyph';
import {
    watchContent,
    disarmContentWatch,
    declareContent,
    showsSomething,
    isWatched,
} from './content-watch';
import { getContentState } from './dataset';
import { stashContent } from './manifestations/stash';

const DEADLINE = 20;

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

function glyphFixture(id: string = 'tokens-glyph'): Glyph {
    return {
        id,
        title: 'Access Tokens',
        renderContent: () => document.createElement('div'),
    };
}

/** A window as render-content.ts leaves it: element, title bar, content area. */
function manifested(body?: HTMLElement): { element: HTMLElement; contentArea: HTMLElement } {
    const element = document.createElement('div');
    element.dataset.glyphId = 'tokens-glyph';

    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    titleBar.textContent = 'Access Tokens';
    element.appendChild(titleBar);

    const contentArea = document.createElement('div');
    contentArea.classList.add('glyph-content-area');
    if (body) contentArea.appendChild(body);
    element.appendChild(contentArea);

    document.body.appendChild(element);
    return { element, contentArea };
}

function text(content: string): HTMLElement {
    const div = document.createElement('div');
    div.textContent = content;
    return div;
}

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: a body that draws', () => {
    test('settles present at mount when the glyph rendered synchronously', () => {
        const { element, contentArea } = manifested(text('Loading tokens…'));
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);

        expect(getContentState(element)).toBe('present');
        expect(isWatched(element)).toBe(false);
    });

    test('a placeholder counts — the question is whether the user sees anything', () => {
        expect(showsSomething(manifested(text('Loading tokens…')).contentArea)).toBe(true);
        expect(showsSomething(manifested(text('No access tokens.')).contentArea)).toBe(true);
    });

    test('settles present when the body draws later', async () => {
        const body = document.createElement('div');
        const { element, contentArea } = manifested(body);
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);
        expect(getContentState(element)).toBe('pending');

        body.appendChild(text('ci-runner'));
        await sleep(5);

        expect(getContentState(element)).toBe('present');
        expect(isWatched(element)).toBe(false);
    });

    test('a body that drew before the deadline is never refused', async () => {
        const body = document.createElement('div');
        const { element, contentArea } = manifested(body);
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);

        body.appendChild(text('ci-runner'));
        await sleep(DEADLINE * 3);

        expect(getContentState(element)).toBe('present');
        expect(contentArea.textContent).not.toContain('nothing was drawn');
    });
});

describe('Spike: a body that never draws', () => {
    test('is refused when the deadline passes', async () => {
        const { element, contentArea } = manifested(document.createElement('div'));
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);
        expect(getContentState(element)).toBe('pending');

        await sleep(DEADLINE * 3);

        expect(getContentState(element)).toBe('refused');
        expect(isWatched(element)).toBe(false);
    });

    test('says so in the body, naming the glyph and how long it waited', async () => {
        const { element, contentArea } = manifested();
        watchContent(element, contentArea, glyphFixture(), 'Panel', DEADLINE);
        await sleep(DEADLINE * 3);

        const said = contentArea.textContent ?? '';
        expect(said).toContain('nothing was drawn, and nothing said why');
        expect(said).toContain('tokens-glyph');
        expect(said).toContain('Panel');
        expect(said).toContain(`${DEADLINE}ms`);
    });

    test('an empty content area shows nothing, whitespace included', () => {
        expect(showsSomething(manifested().contentArea)).toBe(false);
        expect(showsSomething(manifested(text('   \n  ')).contentArea)).toBe(false);
    });

    test('something that draws without words still counts', () => {
        const { contentArea } = manifested();
        contentArea.appendChild(document.createElement('img'));
        expect(showsSomething(contentArea)).toBe(true);
    });

    test('disarming stops the deadline', async () => {
        const { element, contentArea } = manifested();
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);
        disarmContentWatch(element);

        await sleep(DEADLINE * 3);

        expect(getContentState(element)).toBe('pending');
        expect(contentArea.textContent).not.toContain('nothing was drawn');
    });
});

describe('Jenny: the glyph\'s own word', () => {
    test('a glyph may declare empty from anywhere inside its body', async () => {
        const body = document.createElement('div');
        const { element, contentArea } = manifested(body);
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);

        declareContent(body, 'empty');
        await sleep(DEADLINE * 3);

        expect(getContentState(element)).toBe('empty');
        expect(contentArea.textContent).not.toContain('nothing was drawn');
    });

    test('a declared state is not overridden by the deadline', async () => {
        const { element, contentArea } = manifested();
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);

        declareContent(contentArea, 'refused');
        await sleep(DEADLINE * 3);

        expect(getContentState(element)).toBe('refused');
        // The glyph said why itself; the watch does not say it a second time.
        expect(contentArea.textContent).not.toContain('nothing was drawn');
    });

    test('a node outside any glyph declares nothing and throws nothing', () => {
        const orphan = document.createElement('div');
        expect(() => declareContent(orphan, 'present')).not.toThrow();
        expect(getContentState(orphan)).toBeNull();
    });

    test('one element, one watch — re-arming replaces the last', async () => {
        const { element, contentArea } = manifested();
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE * 6);

        await sleep(DEADLINE * 3);
        // The first deadline is gone with the first watch; the second still runs.
        expect(getContentState(element)).toBe('pending');
        expect(isWatched(element)).toBe(true);

        disarmContentWatch(element);
    });

    test('stashing to the tray ends the watch — a stashed body did not fail', async () => {
        const { element, contentArea } = manifested();
        watchContent(element, contentArea, glyphFixture(), 'Window', DEADLINE);

        stashContent(element);
        await sleep(DEADLINE * 3);

        expect(isWatched(element)).toBe(false);
        expect(getContentState(element)).toBe('pending');
        expect(contentArea.textContent).not.toContain('nothing was drawn');
    });
});
