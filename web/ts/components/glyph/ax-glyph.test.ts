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
