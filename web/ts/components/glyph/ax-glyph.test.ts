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
    test('Tim creates AX glyph with query', () => {
        // Tim creates an AX query glyph
        const glyph = createAxGlyph('ax-123', 'test query', 100, 200);

        // AX glyph is created
        expect(glyph.id).toBe('ax-123');
        expect(glyph.symbol).toBe(AX);
        expect(glyph.x).toBe(100);
        expect(glyph.y).toBe(200);

        // Glyph has render function
        expect(glyph.renderContent).toBeDefined();
    });
});

describe('AX Glyph - Spike (Edge Cases)', () => {
    test('Spike types wildcard "*" and sees helpful error message', () => {
        // Spike creates an AX glyph
        const glyph = createAxGlyph('ax-wildcard', '', 100, 200);

        // Render to DOM
        const container = document.createElement('div');
        container.className = 'canvas-workspace';
        document.body.appendChild(container);

        const glyphElement = glyph.renderContent();
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

        // Glyph container has error background tint
        const axContainer = glyphElement.closest('.canvas-ax-glyph') as HTMLElement;
        expect(axContainer?.style.backgroundColor).toContain('rgba(61, 31, 31'); // Red tint

        // Cleanup
        document.body.innerHTML = '';
    });
});
