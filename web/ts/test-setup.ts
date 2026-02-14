/**
 * Test setup - initializes DOM environment for tests
 *
 * Single DOM per mode: USE_JSDOM=1 creates JSDOM, otherwise happy-dom.
 * Per-file JSDOM setup is eliminated to prevent cross-DOM node rejection.
 */

export {};

const USE_JSDOM = process.env.USE_JSDOM === '1';

if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost',
        pretendToBeVisual: true,
    });
    const { window } = dom;

    // Core globals
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
    globalThis.localStorage = window.localStorage;
    // @ts-ignore
    globalThis.DOMParser = window.DOMParser;
    // @ts-ignore
    globalThis.getComputedStyle = window.getComputedStyle;
    // @ts-ignore
    globalThis.MutationObserver = window.MutationObserver;
    // @ts-ignore
    globalThis.AbortController = window.AbortController;
    // @ts-ignore
    globalThis.AbortSignal = window.AbortSignal;

    // requestAnimationFrame — pretendToBeVisual provides it, but ensure globalThis has it
    if (!globalThis.requestAnimationFrame) {
        globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => setTimeout(cb, 0) as any;
        globalThis.cancelAnimationFrame = (id: number) => clearTimeout(id);
    }
    (window as any).requestAnimationFrame ??= globalThis.requestAnimationFrame;
    (window as any).cancelAnimationFrame ??= globalThis.cancelAnimationFrame;

    // ResizeObserver — not in JSDOM; stub with no-op
    // @ts-ignore
    globalThis.ResizeObserver = class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
    };

    // CSS.escape — not in JSDOM
    if (!globalThis.CSS) {
        // @ts-ignore
        globalThis.CSS = { escape: (s: string) => s.replace(/([^\w-])/g, '\\$1') };
    }

    // crypto.randomUUID — not always present in JSDOM
    if (!globalThis.crypto?.randomUUID) {
        // @ts-ignore
        globalThis.crypto = {
            ...globalThis.crypto,
            randomUUID: () => 'test-uuid-' + Math.random() as `${string}-${string}-${string}-${string}-${string}`,
        };
    }

} else {
    // happy-dom path (unchanged)
    const { Window } = await import('happy-dom');
    const window = new Window();
    const document = window.document;

    // @ts-ignore
    globalThis.window = window;
    // @ts-ignore
    globalThis.document = document;
    // @ts-ignore
    globalThis.HTMLElement = window.HTMLElement;
    // @ts-ignore
    globalThis.localStorage = window.localStorage;
    // @ts-ignore
    if (!globalThis.CSS) {
        // @ts-ignore
        globalThis.CSS = { escape: (s: string) => s.replace(/([^\w-])/g, '\\$1') };
    }
}
