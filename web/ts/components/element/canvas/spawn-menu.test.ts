/**
 * Tests for canvas spawn menu
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { showSpawnMenu } from './spawn-menu';
import { registerElementType } from '../element-registry';

// Mock browser APIs not available in happy-dom
(globalThis.window as any).Element.prototype.animate = function() {
    return { finished: Promise.resolve(), onfinish: null, cancel: () => {} } as any;
};
globalThis.requestAnimationFrame = (_cb: FrameRequestCallback) => 0;
globalThis.cancelAnimationFrame = () => {};

describe('Canvas Spawn Menu - Tim (Happy Path)', () => {
    test('Tim opens spawn menu at canvas position', () => {
        const canvas = document.createElement('div');
        canvas.style.position = 'relative';
        document.body.appendChild(canvas);

        const items: any[] = [];

        // Tim right-clicks on canvas and spawn menu appears
        expect(() => {
            showSpawnMenu(150, 200, canvas, items);
        }).not.toThrow();

        // Cleanup
        document.body.innerHTML = '';
    });

    test('Tim places an element that was published, not built in', () => {
        registerElementType({
            symbol: '◇',
            className: 'canvas-published-element element-example',
            title: 'EXAMPLE',
            label: 'example',
            publishedName: 'example',
            render: () => document.createElement('div'),
        });
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        showSpawnMenu(150, 200, canvas, []);
        const offered = [...document.querySelectorAll('.canvas-spawn-button')].map(b => b.firstChild?.textContent);
        expect(offered).toContain('◇');

        document.body.innerHTML = '';
    });
});
