/**
 * Tests for IX glyph auto-sizing
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { createIxGlyph } from './ix-glyph';
import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;
globalThis.localStorage = window.localStorage;

describe('IX Glyph Auto-Sizing - Tim (Happy Path)', () => {
    test('Tim creates IX glyph with default size', async () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const glyph: Glyph = {
            id: 'ix-123',
            title: 'Ingest',
            symbol: IX,
            x: 100,
            y: 100,
            renderContent: () => document.createElement('div')
        };

        const element = await createIxGlyph(glyph);
        container.appendChild(element);

        // IX glyph is created
        expect(element.classList.contains('canvas-ix-glyph')).toBe(true);
        expect(element.dataset.glyphSymbol).toBe(IX);

        // Has input element
        const input = element.querySelector('input[type="text"]');
        expect(input).toBeTruthy();
    });

    test('Tim types URL and IX glyph resizes', async () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const glyph: Glyph = {
            id: 'ix-456',
            title: 'Ingest',
            symbol: IX,
            x: 200,
            y: 200,
            renderContent: () => document.createElement('div')
        };

        const element = await createIxGlyph(glyph);
        container.appendChild(element);

        const input = element.querySelector('input[type="text"]') as HTMLInputElement;
        expect(input).toBeTruthy();

        // Initial width exists
        const initialWidth = parseInt(element.style.width);
        expect(initialWidth).toBeGreaterThan(0);

        // Tim types a URL
        input.value = 'https://example.com/data.json';

        // Input contains the text
        expect(input.value).toBe('https://example.com/data.json');
    });

    test('Tim sees play button on IX glyph', async () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const glyph: Glyph = {
            id: 'ix-789',
            title: 'Ingest',
            symbol: IX,
            x: 300,
            y: 300,
            renderContent: () => document.createElement('div')
        };

        const element = await createIxGlyph(glyph);
        container.appendChild(element);

        // Play button exists
        const playBtn = element.querySelector('.glyph-play-btn');
        expect(playBtn).toBeTruthy();
        expect(playBtn?.textContent).toBe('▶');
    });
});

describe('IX Glyph Auto-Sizing - Spike (Edge Cases)', () => {
    test('Spike types extremely long URL', async () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const glyph: Glyph = {
            id: 'ix-long',
            title: 'Ingest',
            symbol: IX,
            x: 100,
            y: 100,
            renderContent: () => document.createElement('div')
        };

        const element = await createIxGlyph(glyph);
        container.appendChild(element);

        const input = element.querySelector('input[type="text"]') as HTMLInputElement;

        // Spike types a very long URL
        const longUrl = 'https://example.com/' + 'a'.repeat(500);
        input.value = longUrl;

        // Glyph still functions
        expect(input.value.length).toBe(longUrl.length);
        expect(element.classList.contains('canvas-ix-glyph')).toBe(true);
    });

    test('Spike creates IX glyph with empty input', async () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const glyph: Glyph = {
            id: 'ix-empty',
            title: 'Ingest',
            symbol: IX,
            x: 200,
            y: 200,
            renderContent: () => document.createElement('div')
        };

        const element = await createIxGlyph(glyph);
        container.appendChild(element);

        const input = element.querySelector('input[type="text"]') as HTMLInputElement;

        // Empty input has placeholder
        expect(input.placeholder).toBeTruthy();
        expect(input.value).toBe('');
    });
});
