/**
 * Tests for canvas placement mode
 *
 * Personas:
 * - Tim: Happy path — selects a glyph, carries it, places it
 * - Spike: Edge cases — cancels placement, double-enters placement mode
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import {
    isPlacementActive,
    enterPlacementMode,
    cancelPlacement,
    showMenuScrim,
    removeScrim,
} from './placement-mode';
import type { GlyphTypeEntry } from '../glyph-registry';

// Suppress logger in tests
import { mock as bunMock } from 'bun:test';
bunMock.module('../../../logger', () => ({
    log: { debug: () => {}, error: () => {}, warn: () => {} },
    SEG: { GLYPH: 'glyph' },
}));

const fakeEntry: GlyphTypeEntry = {
    symbol: 'T',
    className: 'canvas-test-glyph',
    title: 'Test',
    label: 'Test',
    render: () => document.createElement('div'),
    spawnMenuOrder: 0,
};

function makeCanvas(): HTMLElement {
    const container = document.createElement('div');
    container.style.position = 'relative';
    container.getBoundingClientRect = () => ({
        left: 0, top: 0, right: 800, bottom: 600,
        width: 800, height: 600, x: 0, y: 0, toJSON: () => '',
    });
    const contentLayer = document.createElement('div');
    container.appendChild(contentLayer);
    document.body.appendChild(container);
    return contentLayer;
}

beforeEach(() => {
    cancelPlacement();
    removeScrim();
    document.body.innerHTML = '';
});

describe('Placement Mode - Tim (Happy Path)', () => {
    test('Tim sees placement starts inactive', () => {
        expect(isPlacementActive()).toBe(false);
    });

    test('Tim enters placement mode and it becomes active', () => {
        const canvas = makeCanvas();
        const callback = mock(() => {});
        enterPlacementMode(fakeEntry, canvas, callback);

        expect(isPlacementActive()).toBe(true);
        // Cursor glyph is in the DOM
        const cursorGlyph = document.querySelector('.placement-cursor-glyph');
        expect(cursorGlyph).not.toBeNull();
        expect(cursorGlyph!.textContent).toBe('T');
    });

    test('Tim cancels placement and it deactivates', () => {
        const canvas = makeCanvas();
        enterPlacementMode(fakeEntry, canvas, () => {});

        cancelPlacement();
        expect(isPlacementActive()).toBe(false);
        expect(document.querySelector('.placement-cursor-glyph')).toBeNull();
        expect(document.querySelector('.placement-scrim')).toBeNull();
    });

    test('Tim places glyph via left click and callback fires', () => {
        const canvas = makeCanvas();
        const callback = mock((_x: number, _y: number) => {});
        enterPlacementMode(fakeEntry, canvas, callback);

        // Simulate left click
        const event = new (window as any).MouseEvent('mousedown', {
            button: 0,
            clientX: 300,
            clientY: 200,
            bubbles: true,
        });
        document.dispatchEvent(event);

        expect(callback).toHaveBeenCalledTimes(1);
        expect(callback).toHaveBeenCalledWith(300, 200);
        expect(isPlacementActive()).toBe(false);
    });
});

describe('Placement Mode - Spike (Edge Cases)', () => {
    test('Spike cancels when not in placement mode — no crash', () => {
        expect(() => cancelPlacement()).not.toThrow();
    });

    test('Spike enters placement mode twice — first is cleaned up', () => {
        const canvas = makeCanvas();
        enterPlacementMode(fakeEntry, canvas, () => {});
        enterPlacementMode(fakeEntry, canvas, () => {});

        // Only one cursor glyph should exist
        const cursorGlyphs = document.querySelectorAll('.placement-cursor-glyph');
        expect(cursorGlyphs.length).toBe(1);
    });

    test('Spike right-clicks during placement — cancels it', () => {
        const canvas = makeCanvas();
        enterPlacementMode(fakeEntry, canvas, () => {});

        const event = new (window as any).MouseEvent('contextmenu', { bubbles: true });
        document.dispatchEvent(event);

        expect(isPlacementActive()).toBe(false);
    });
});

describe('Scrim', () => {
    test('showMenuScrim creates a scrim element', () => {
        showMenuScrim();
        const scrim = document.querySelector('.placement-scrim');
        expect(scrim).not.toBeNull();
        expect(scrim!.classList.contains('placement-scrim--menu')).toBe(true);
    });

    test('showMenuScrim replaces existing scrim', () => {
        showMenuScrim();
        showMenuScrim();
        const scrims = document.querySelectorAll('.placement-scrim');
        expect(scrims.length).toBe(1);
    });

    test('removeScrim cleans up', () => {
        showMenuScrim();
        removeScrim();
        expect(document.querySelector('.placement-scrim')).toBeNull();
    });
});
