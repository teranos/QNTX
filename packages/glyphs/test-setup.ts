/**
 * Test setup for @qntx/glyphs — provides DOM environment.
 *
 * Uses happy-dom for local runs, JSDOM when USE_JSDOM=1 (CI).
 */

const USE_JSDOM = process.env.USE_JSDOM === '1';

if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost',
        pretendToBeVisual: true,
    });
    const { window } = dom;

    // @ts-ignore
    globalThis.window = window;
    // @ts-ignore
    globalThis.document = window.document;
    // @ts-ignore
    globalThis.HTMLElement = window.HTMLElement;
    // @ts-ignore
    globalThis.Element = window.Element;
    // @ts-ignore
    globalThis.Event = window.Event;
    // @ts-ignore
    globalThis.MouseEvent = window.MouseEvent;
    // @ts-ignore
    globalThis.navigator = window.navigator;
    // @ts-ignore
    globalThis.getComputedStyle = window.getComputedStyle;
    // @ts-ignore
    globalThis.MutationObserver = window.MutationObserver;
    // JSDOM's addEventListener realm-checks the `signal` option against its own
    // AbortSignal class. Bun's native AbortController fails that isinstance
    // check ("parameter 3 dictionary has member 'signal' that is not of type
    // 'AbortSignal'"). Route both through JSDOM's realm.
    // @ts-ignore
    globalThis.AbortController = window.AbortController;
    // @ts-ignore
    globalThis.AbortSignal = window.AbortSignal;

    if (!globalThis.requestAnimationFrame) {
        globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => setTimeout(cb, 0) as any;
        globalThis.cancelAnimationFrame = (id: number) => clearTimeout(id);
    }

    // @ts-ignore
    globalThis.ResizeObserver = class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
    };

    if (!globalThis.CSS) {
        // @ts-ignore
        globalThis.CSS = { escape: (s: string) => s };
    }

    if (!globalThis.crypto?.randomUUID) {
        // @ts-ignore
        globalThis.crypto = {
            ...globalThis.crypto,
            randomUUID: () => 'test-uuid-' + Math.random() as `${string}-${string}-${string}-${string}-${string}`,
        };
    }
} else {
    const { Window } = await import('happy-dom');
    const window = new Window();

    // @ts-ignore
    globalThis.window = window;
    // @ts-ignore
    globalThis.document = window.document;
    // @ts-ignore
    globalThis.HTMLElement = window.HTMLElement;
    // @ts-ignore
    globalThis.localStorage = window.localStorage;
    // @ts-ignore
    globalThis.MutationObserver = window.MutationObserver;
}
