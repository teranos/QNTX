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
}));

const { initNamespacePage } = await import('./namespace-page.ts');

const root = {
    user: 'US-ROOT', name: 'root', picture: '', level: 'ROOT', namespaces: [], standing: 'aws',
    identity: 'x', via: 'session', accounts: [], keys: [],
};

describe('the namespace page', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '<main id="container"></main>';
        created = [];
    });

    // jsdom has no page to build again, so entering a canvas builds nothing.
    const built = (person: typeof root, rows: Parameters<typeof initNamespacePage>[1]) =>
        initNamespacePage(person, rows, () => {});

    test('names the namespace, lists no canvas, and shows ⌗ alone', () => {
        built(root, []);
        expect(document.querySelector('.namespace-page-name')?.textContent).toBe('aws');
        expect(document.querySelector('.canvas-none')).not.toBeNull();
        expect(document.querySelector('input.canvas-birth-name')).toBeNull();
        expect(document.querySelectorAll('button.canvas-birth-symbol').length).toBe(2);
    });

    test('⌗ reveals the name and a +; the + is pressed once to arm and once to create', async () => {
        built(root, []);
        const symbol = document.querySelector<HTMLButtonElement>('button.canvas-birth-symbol[data-kind="namespace"]')!;
        symbol.click();
        const name = document.querySelector<HTMLInputElement>('input.canvas-birth-name')!;
        expect(name).not.toBeNull();
        name.value = 'garden';

        const plus = () => document.querySelector<HTMLButtonElement>('.canvas-birth-plus')!;
        expect(plus().classList.contains('armed')).toBe(false);
        plus().click();
        expect(plus().classList.contains('armed')).toBe(true);
        expect(created).toEqual([]);

        plus().click();
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(created).toEqual([{ name: 'garden', kind: 'namespace' }]);
    });

    test('Enter commits nothing', () => {
        built(root, []);
        document.querySelector<HTMLButtonElement>('button.canvas-birth-symbol')!.click();
        const name = document.querySelector<HTMLInputElement>('input.canvas-birth-name')!;
        name.value = 'garden';
        name.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
        expect(created).toEqual([]);
    });

    test('a disabled canvas is listed desaturated, and a User\'s canvas names its owners', () => {
        built(root, [
            { id: 'CV-1', name: 'garden', kind: 'namespace', created_by: '', created_at: '', disabled_by: '', owners: [], access: [], owner_views: [], mine: false },
            { id: 'CV-2', name: 'bob\'s', kind: 'user', created_by: 'US-BOB', created_at: '', disabled_by: 'US-ROOT', owners: ['US-BOB'], access: [], owner_views: [{ id: 'US-BOB', name: 'Bob', picture: '' }], mine: false },
        ]);
        const rows = document.querySelectorAll('.canvas-row');
        expect(rows.length).toBe(2);
        expect(rows[1].classList.contains('disabled')).toBe(true);
        expect(rows[1].querySelector('.canvas-row-owners')?.textContent).toContain('Bob');
        expect(rows[1].querySelector('.canvas-row-state')?.textContent).toContain('disabled by US-ROOT');
    });
});
