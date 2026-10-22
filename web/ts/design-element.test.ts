// ◈ — the Design Element: tokens.css's groups, the components, the element specimens, and no node behind any of it.

import { describe, test, expect, beforeEach, afterEach, mock } from 'bun:test';
import tokensCss from '../css/tokens.css' with { type: 'text' };

// Every call the element could make to the node is counted: through the client, and past it.
const asked: string[] = [];
mock.module('./client', () => ({
    connectivity: {
        get state() { return 'online' as const; },
        get authenticated() { return true; },
        subscribe: () => () => {},
        subscribeAuth: () => () => {},
        subscribeFailures: () => () => {},
        failures: [],
        reportReachable: () => {},
        reportHttpFailure: () => {},
        reportWsFailure: () => {},
        reportUnauthenticated: () => {},
        reportAuthenticated: () => {},
        setWebSocketConnected: () => {},
    },
    apiFetch: (path: string) => { asked.push(`apiFetch ${path}`); return Promise.resolve(new Response()); },
    apiJson: (path: string) => { asked.push(`apiJson ${path}`); return Promise.resolve({}); },
    backendUrl: () => 'http://localhost',
    backendWsUrl: () => 'ws://localhost',
    backendPath: (path: string) => 'http://localhost' + path,
    sendMessage: () => { asked.push('sendMessage'); return false; },
    connectWebSocket: () => { asked.push('connectWebSocket'); },
    registerHandler: () => {},
    unregisterHandler: () => {},
}));

const { createDesignElement, parseTokens, ELEMENT_ID } = await import('./design-element.ts');

function open(): HTMLElement {
    const body = createDesignElement().renderContent() as HTMLElement;
    document.body.appendChild(body);
    return body;
}

const realFetch = globalThis.fetch;

beforeEach(() => {
    document.body.innerHTML = '';
    asked.length = 0;
    globalThis.fetch = ((input: unknown) => { asked.push(`fetch ${String(input)}`); return Promise.reject(new Error('no network in this test')); }) as typeof fetch;
});

afterEach(() => {
    globalThis.fetch = realFetch;
});

describe('Tim', () => {
    test('it is ◈ Design in the tray', () => {
        const element = createDesignElement();
        expect(element.id).toBe(ELEMENT_ID);
        expect(element.title).toBe('Design');
        expect(element.symbol).toBe('◈');
    });

    test('every group of tokens.css is drawn, with every token in it', () => {
        const body = open();
        const groups = parseTokens(tokensCss);
        const drawn = [...body.querySelectorAll<HTMLElement>('.design-token-group')].map((g) => g.dataset.group);
        expect(drawn).toEqual(groups.map((g) => g.heading));
        expect(drawn).toContain('Typography');
        expect(drawn).toContain('Core Colors');
        const tokens = [...body.querySelectorAll<HTMLElement>('.design-token')].map((t) => t.dataset.token);
        expect(tokens).toEqual(groups.flatMap((g) => g.tokens.map((t) => t.name)));
        expect(tokens).toContain('--bg-canvas');
    });

    test('the components are drawn: press, ElementUI primitives, table, text size, title bar', () => {
        const body = open();
        const specimens = [...body.querySelectorAll<HTMLElement>('.design-components .design-specimen')].map((s) => s.dataset.specimen);
        expect(specimens).toEqual(['Press', 'ElementUI primitives', 'Table', 'Text size', 'title-bar']);
        for (const id of ['design-press-one', 'design-press-undoable', 'design-press-irrevocable', 'design-press-creates']) {
            expect(body.querySelector(`[data-press="${id}"]`)).not.toBeNull();
        }
        expect(body.querySelector('table.design-sample-table')).not.toBeNull();
    });

    test('the element specimens are drawn: proximity, morph, title bar controls, stash', () => {
        const body = open();
        const specimens = [...body.querySelectorAll<HTMLElement>('.design-specimens .design-specimen')].map((s) => s.dataset.specimen);
        expect(specimens).toEqual(['Proximity', 'Morph transactions', 'Title bar controls', 'Stash / restore']);
    });
});

describe('Spike', () => {
    test('drawn whole, it asked the node nothing', async () => {
        open();
        await new Promise((done) => setTimeout(done, 10));
        expect(asked).toEqual([]);
    });

    // An element is one DOM element for its life: a specimen is never a second one.
    test('nothing in it carries a data-element-id', () => {
        const body = open();
        expect(body.querySelectorAll('[data-element-id]').length).toBe(0);
    });

    test('a heading is a comment on its own line; one beside a declaration is that token\'s', () => {
        const groups = parseTokens(':root {\n    /* First: with a colon */\n    --a: #fff; /* white */\n    --b: var(--a);\n\n    /* Second */\n    --c: 7px;\n}\n');
        expect(groups).toEqual([
            { heading: 'First: with a colon', tokens: [{ name: '--a', value: '#fff', comment: 'white' }, { name: '--b', value: 'var(--a)' }] },
            { heading: 'Second', tokens: [{ name: '--c', value: '7px' }] },
        ]);
    });

    test('a heading with nothing under it is no group', () => {
        expect(parseTokens('/* Empty */\n/* Full */\n--x: 1px;\n')).toEqual([{ heading: 'Full', tokens: [{ name: '--x', value: '1px' }] }]);
    });
});

describe('Jenny', () => {
    test('stashed, the window is empty; restored, it holds the same nodes and what was typed', async () => {
        const body = open();
        const win = body.querySelector<HTMLElement>('.design-stash-window');
        if (!win) throw new Error('no stash window drawn');
        const field = win.querySelector('input');
        if (!field) throw new Error('no field in the stash window');
        field.value = 'kept';

        body.querySelector<HTMLButtonElement>('[data-press="design-stash"]')?.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(win.childNodes.length).toBe(0);

        body.querySelector<HTMLButtonElement>('[data-press="design-restore"]')?.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(win.querySelector('input')).toBe(field);
        expect(field.value).toBe('kept');
    });

    test('a two-stage press arms on the first press and does it on the second', async () => {
        const body = open();
        const press = body.querySelector<HTMLButtonElement>('[data-press="design-press-irrevocable"]');
        if (!press) throw new Error('no irrevocable press drawn');
        const said = press.closest('.design-specimen')?.querySelector('.design-status');
        press.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(press.dataset.stage).toBe('armed');
        expect(said?.textContent).toBe('');
        press.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(said?.textContent).toBe('Irrevocable: done');
    });

    test('title bar controls go on the bar and come off it', async () => {
        const body = open();
        const card = [...body.querySelectorAll<HTMLElement>('.design-specimen')].find((s) => s.dataset.specimen === 'Title bar controls');
        if (!card) throw new Error('no title bar controls specimen drawn');
        card.querySelector<HTMLButtonElement>('[data-press="design-controls-add"]')?.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(card.querySelector('.title-bar .window-controls')).not.toBeNull();
        card.querySelector<HTMLButtonElement>('[data-press="design-controls-remove"]')?.click();
        await new Promise((done) => setTimeout(done, 0));
        expect(card.querySelector('.title-bar .window-controls')).toBeNull();
    });
});
