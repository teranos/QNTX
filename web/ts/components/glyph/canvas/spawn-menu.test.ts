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

// Mock animate for tests
(globalThis.window as any).Element.prototype.animate = function() {
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
