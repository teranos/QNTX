/**
 * @jest-environment jsdom
 *
 * "i dont want the inputfield to be there by default, it should be gated
 * behind clicking on the symbol, and i dont want enter as the thing that
 * commits, a separate button right from the box with a same sized + button,
 * commits you to creating it press once to check, button becomes green and
 * shimmers, 2nd click actually creates it."
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';

const USE_JSDOM = process.env.USE_JSDOM === '1';

let created: { name: string; kind: string }[] = [];
mock.module('./api/canvases', () => ({
    listCanvases: async () => [],
    createCanvas: async (name: string, kind: string) => { created.push({ name, kind }); return { id: 'CV-NEW', name, kind }; },
    disableCanvas: async () => ({}),
    enableCanvas: async () => ({}),
    disownCanvas: async () => ({}),
    addOwner: async () => ({}),
    removeOwner: async () => ({}),
    grantAccess: async () => ({}),
    revokeAccess: async () => ({}),
    inviteOwner: async () => {},
    acceptInvitation: async () => ({}),
    nukeCanvas: async () => {},
}));

const { initNamespacePage, opening, enter } = await import('./namespace-page.ts');
const { setStanding, openCanvasKey, openOnceKey } = await import('./standing.ts');

const namespaces = { id: 'CV-NS', name: 'garden', kind: 'namespace' as const, created_by: '', created_at: '', disabled_by: '', owners: [], access: [], owner_views: [], mine: false };
const bobs = { id: 'CV-BOB', name: 'bob\'s', kind: 'user' as const, created_by: 'US-BOB', created_at: '', disabled_by: '', owners: ['US-BOB'], access: [], owner_views: [], mine: true };

describe('which canvas opens', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        // enter() builds the page again; jsdom has none to build.
        document.body.innerHTML = '<main id="container"></main>';
        initNamespacePage(root, [], () => {});
        setStanding('aws');
        localStorage.removeItem(openCanvasKey());
        localStorage.removeItem(openOnceKey());
    });

    test('nothing remembered opens the namespace\'s own', () => {
        expect(opening([namespaces, bobs])).toEqual({ open: '', hasCanvas: true, disabled: false });
    });

    test('a remembered canvas opens', () => {
        localStorage.setItem(openCanvasKey(), 'CV-BOB');
        expect(opening([namespaces, bobs])).toEqual({ open: 'CV-BOB', hasCanvas: true, disabled: false });
    });

    test('a disabled canvas never opens on its own', () => {
        localStorage.setItem(openCanvasKey(), 'CV-BOB');
        expect(opening([namespaces, { ...bobs, disabled_by: 'US-ROOT' }])).toEqual({ open: '', hasCanvas: false, disabled: false });
        expect(opening([{ ...namespaces, disabled_by: 'US-ROOT' }])).toEqual({ open: '', hasCanvas: false, disabled: false });
    });

    test('a disabled canvas pressed on the list opens once, desaturated', () => {
        enter('CV-BOB');
        expect(opening([namespaces, { ...bobs, disabled_by: 'US-ROOT' }])).toEqual({ open: 'CV-BOB', hasCanvas: true, disabled: true });
        // The next build, without a press, does not open it again.
        expect(opening([namespaces, { ...bobs, disabled_by: 'US-ROOT' }])).toEqual({ open: '', hasCanvas: false, disabled: false });
    });
});

const root = {
    user: 'US-ROOT', name: 'root', picture: '', level: 'ROOT', namespaces: [], standing: 'aws',
    identity: 'x', via: 'session', accounts: [], keys: [],
};

const tick = () => new Promise(resolve => setTimeout(resolve, 0));

describe('the namespace page', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '<main id="container"><div id="left-panel"><header id="header"><div class="who"><img id="who-picture"><div class="who-words"><span id="who-name"></span><span id="who-namespace"></span></div></div></header></div></main>';
        created = [];
    });

    // jsdom has no page to build again, so entering a canvas builds nothing.
    const built = (rows: Parameters<typeof initNamespacePage>[1]) => initNamespacePage(root, rows, () => {});

    test('names the namespace, lists no canvas, shows one ⌗ and no field, and takes who to its top-left', () => {
        built([]);
        expect(document.querySelector('.namespace-page-name')?.textContent).toBe('aws');
        expect(document.querySelector('.canvas-none')).not.toBeNull();
        expect(document.querySelector('input.canvas-birth-name')).toBeNull();
        const symbols = document.querySelectorAll('button.canvas-birth-symbol');
        expect(symbols.length).toBe(1);
        expect(document.querySelector('.namespace-page-top .who #who-picture')).not.toBeNull();
        expect(document.getElementById('container')?.classList.contains('namespace-page-shown')).toBe(true);
    });

    test('⌗ reveals the name and a +; the + is pressed once to arm and once to create', async () => {
        built([]);
        document.querySelector<HTMLButtonElement>('button.canvas-birth-symbol')!.click();
        await tick();
        const name = document.querySelector<HTMLInputElement>('input.canvas-birth-name')!;
        expect(name).not.toBeNull();
        name.value = 'garden';

        const plus = () => document.querySelector<HTMLButtonElement>('.canvas-birth-plus')!;
        expect(plus().classList.contains('qntx-btn-confirming')).toBe(false);
        plus().click();
        await tick();
        expect(plus().classList.contains('qntx-btn-confirming')).toBe(true);
        expect(created).toEqual([]);

        plus().click();
        await tick();
        await tick();
        // ROOT with no namespace canvas makes the namespace's first.
        expect(created).toEqual([{ name: 'garden', kind: 'namespace' }]);
    });

    test('Enter commits nothing', async () => {
        built([]);
        document.querySelector<HTMLButtonElement>('button.canvas-birth-symbol')!.click();
        await tick();
        const name = document.querySelector<HTMLInputElement>('input.canvas-birth-name')!;
        name.value = 'garden';
        name.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
        await tick();
        expect(created).toEqual([]);
    });

    test('a disabled canvas is listed desaturated, and a User\'s canvas names its owners', () => {
        built([
            { id: 'CV-1', name: 'garden', kind: 'namespace', created_by: '', created_at: '', disabled_by: '', owners: [], access: [], owner_views: [], mine: false },
            { id: 'CV-2', name: 'bob\'s', kind: 'user', created_by: 'US-BOB', created_at: '', disabled_by: 'US-ROOT', owners: ['US-BOB'], access: [], owner_views: [{ id: 'US-BOB', name: 'Bob', picture: '' }], mine: false },
        ]);
        const rows = document.querySelectorAll('.canvas-card');
        expect(rows.length).toBe(2);
        expect(rows[1].classList.contains('disabled')).toBe(true);
        expect(rows[1].querySelector('.canvas-card-owners')?.textContent).toContain('Bob');
        expect(rows[1].querySelector('.canvas-card-state')?.textContent).toContain('disabled by US-ROOT');
        // The acts are the system's buttons.
        expect(rows[1].querySelector('.canvas-act.qntx-btn')).not.toBeNull();
    });
});
