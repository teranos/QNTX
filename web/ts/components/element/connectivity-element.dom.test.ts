/**
 * @jest-environment jsdom
 *
 * The connectivity element on a phone: it rests in the tray and its dot
 * changes color, and nothing is opened over what is being done.
 *
 * "While it's in the tray, it may change dot color on activity"
 *
 * These tests run only with USE_JSDOM=1 (CI environment)
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';

const USE_JSDOM = process.env.USE_JSDOM === '1';

// Web Animations API is not in JSDOM; finish at once so a morph completes.
if (USE_JSDOM) {
    (globalThis.window as any).HTMLElement.prototype.animate = function() {
        return {
            finished: Promise.resolve(),
            cancel: () => {},
            finish: () => {},
            play: () => {},
            pause: () => {},
            addEventListener: (type: string, cb: Function) => {
                if (type === 'finish') queueMicrotask(() => cb());
            },
            removeEventListener: () => {},
        };
    };
}

import { createMockUiState } from '../../test/mock-ui-state';
const { uiState } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

const { tray, getForm, DEFAULT_COLOR } = await import('@teranos/elements');
const { signalConnectivityInTray } = await import('./connectivity-element');

const ID = 'connectivity';
const UNSEEN = 'rgb(224, 96, 96)'; // #e06060 as the DOM reads it back

function dot(): HTMLElement {
    const els = document.querySelectorAll<HTMLElement>(`[data-element-id="${ID}"]`);
    expect(els.length).toBe(1);
    return els[0];
}

async function settle(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    await new Promise(r => setTimeout(r, 0));
}

describe('Connectivity element on a phone', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
        (tray as any).element = null;
        (tray as any).indicatorContainer = null;
        (tray as any).items.clear();
        (tray as any).elements.clear();
        (tray as any).deferredItems = [];
        tray.init();
    });

    test('Tim: a failure puts it in the tray with its dot colored, and opens nothing', () => {
        signalConnectivityInTray();

        expect(tray.has(ID)).toBe(true);
        const el = dot();
        expect(getForm(el)).not.toBe('window');
        expect(el.style.backgroundColor).toBe(UNSEEN);
    });

    test('Spike: a failure a second for minutes is still one dot, still closed', () => {
        for (let i = 0; i < 120; i++) signalConnectivityInTray();

        const el = dot();
        expect(getForm(el)).not.toBe('window');
        expect(el.style.backgroundColor).toBe(UNSEEN);
    });

    test('Jenny: opening it is seeing it — the color comes off, and a failure while open adds none', async () => {
        const fresh = document.createElement('div');
        fresh.style.backgroundColor = DEFAULT_COLOR;
        const plain = fresh.style.backgroundColor;

        signalConnectivityInTray();
        tray.open(ID);
        await settle();

        const el = dot();
        expect(getForm(el)).toBe('window');
        expect(el.style.backgroundColor).toBe(plain);

        signalConnectivityInTray();
        await settle();
        expect(el.style.backgroundColor).toBe(plain);
    });

    for (const control of ['Minimize', 'Close']) {
        test(`Jenny: put away with ${control}, the next failure colors the dot and opens nothing`, async () => {
            signalConnectivityInTray();
            tray.open(ID);
            await settle();
            const button = dot().querySelector<HTMLElement>(`[aria-label="${control}"]`);
            expect(button).not.toBeNull();
            button!.click();
            await settle();

            signalConnectivityInTray();

            const el = dot();
            expect(getForm(el)).not.toBe('window');
            expect(el.style.backgroundColor).toBe(UNSEEN);
        });
    }

    test('Spike: once opened and put away, a dot with no new failure is not colored', async () => {
        signalConnectivityInTray();
        tray.open(ID);
        await settle();
        dot().querySelector<HTMLElement>('[aria-label="Minimize"]')!.click();
        await settle();

        expect(dot().style.backgroundColor).not.toBe(UNSEEN);
    });
});
