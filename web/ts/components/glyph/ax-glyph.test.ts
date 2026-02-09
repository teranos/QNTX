/**
 * Tests for AX glyph
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { createAxGlyph } from './ax-glyph';
import type { Glyph } from './glyph';
import { AX } from '@generated/sym.js';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;
globalThis.localStorage = window.localStorage;

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

describe('AX Glyph - Tim (Happy Path)', () => {
    test('Tim creates AX glyph and gets HTMLElement back', () => {
        // Tim creates a Glyph object and passes it to createAxGlyph
        const glyph: Glyph = {
            id: 'ax-123',
            title: 'AX Query',
            symbol: AX,
            x: 100,
            y: 200,
            renderContent: () => document.createElement('div') as any,
        };

        const element = createAxGlyph(glyph);

        // Element has correct data attributes
        expect(element.dataset.glyphId).toBe('ax-123');
        expect(element.dataset.glyphSymbol).toBe(AX);
        expect(element.classList.contains('canvas-ax-glyph')).toBe(true);
        expect(element.classList.contains('canvas-glyph')).toBe(true);

        // Has title bar with shared CSS class
        const titleBar = element.querySelector('.canvas-glyph-title-bar');
        expect(titleBar).toBeTruthy();

        // Has query input
        const input = element.querySelector('.ax-query-input') as HTMLInputElement;
        expect(input).toBeTruthy();

        // Has results container
        const results = element.querySelector('.ax-glyph-results');
        expect(results).toBeTruthy();
    });
});

describe('AX Glyph - Spike (Edge Cases)', () => {
    test('Spike types wildcard "*" and sees helpful error message', () => {
        // Spike creates an AX glyph
        const glyph: Glyph = {
            id: 'ax-wildcard',
            title: 'AX Query',
            symbol: AX,
            x: 100,
            y: 200,
            renderContent: () => document.createElement('div') as any,
        };

        // Render to DOM
        const container = document.createElement('div');
        container.className = 'canvas-workspace';
        document.body.appendChild(container);

        const glyphElement = createAxGlyph(glyph);
        container.appendChild(glyphElement);

        // Spike types "*" in the query editor
        const editor = glyphElement.querySelector('.ax-query-input') as HTMLInputElement;
        expect(editor).toBeTruthy();
        editor.value = '*';

        // Trigger input event (would normally trigger debounced watcher_upsert)
        const inputEvent = new window.Event('input', { bubbles: true });
        editor.dispatchEvent(inputEvent);

        // Simulate WebSocket receiving watcher_error from backend
        // (Rust parser rejects "*" → Go broadcasts watcher_error → TypeScript calls updateAxGlyphError)
        const { updateAxGlyphError } = require('./ax-glyph');
        updateAxGlyphError(
            'ax-wildcard',
            'wildcard/special character is not supported in ax queries - use specific query names',
            'error',
            ['Parser rejected wildcard token']
        );

        // Spike sees error display with helpful message
        const errorDisplay = glyphElement.querySelector('.ax-glyph-error') as HTMLElement;
        expect(errorDisplay).toBeTruthy();
        expect(errorDisplay.textContent).toContain('wildcard/special character is not supported');
        expect(errorDisplay.textContent).toContain('use specific query names');
        expect(errorDisplay.textContent).toContain('ERROR');

        // Error display has error styling
        expect(errorDisplay.style.backgroundColor).toContain('var(--glyph-status-error-section-bg)');

        // Glyph element itself gets error background (element IS the container now)
        expect(glyphElement.style.backgroundColor).toContain('rgba(61, 31, 31'); // Red tint

        // Cleanup
        document.body.innerHTML = '';
    });
});
