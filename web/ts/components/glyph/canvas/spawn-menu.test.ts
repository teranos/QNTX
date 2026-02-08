/**
 * Tests for canvas spawn menu
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { showSpawnMenu } from './spawn-menu';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

// Mock animate for tests
(window as any).Element.prototype.animate = function() {
    return { finished: Promise.resolve() } as any;
};

describe('Canvas Spawn Menu - Tim (Happy Path)', () => {
    test('Tim opens spawn menu at canvas position', () => {
        const canvas = document.createElement('div');
        canvas.style.position = 'relative';
        document.body.appendChild(canvas);

        const glyphs: any[] = [];

        // Tim right-clicks on canvas and spawn menu appears
        expect(() => {
            showSpawnMenu(150, 200, canvas, glyphs);
        }).not.toThrow();

        // Cleanup
        document.body.innerHTML = '';
    });
});
