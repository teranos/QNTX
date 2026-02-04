/**
 * Tests for generic glyph status storage
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { saveGlyphStatus, loadGlyphStatus, type GlyphStatus, type ExecutableGlyphStatus } from './glyph-storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost'
    });
    const { window } = dom;

    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;
}

describe('Generic Glyph Storage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        localStorage.clear();
    });

    test('save and load basic glyph status', () => {
        const glyphId = 'test-glyph-123';
        const status: GlyphStatus = {
            state: 'success',
            message: 'Test completed',
            timestamp: Date.now(),
        };

        saveGlyphStatus('prompt', glyphId, status);
        const loaded = loadGlyphStatus<GlyphStatus>('prompt', glyphId);

        expect(loaded).toEqual(status);
    });

    test('different glyph types have separate storage namespaces', () => {
        const glyphId = 'shared-id-456';

        const promptStatus: GlyphStatus = {
            state: 'success',
            message: 'Prompt succeeded',
            timestamp: 1000,
        };

        const ixStatus: ExecutableGlyphStatus = {
            state: 'running',
            message: 'IX executing',
            timestamp: 2000,
            scheduledJobId: 'job-123',
            executionId: 'exec-456',
        };

        saveGlyphStatus('prompt', glyphId, promptStatus);
        saveGlyphStatus('ix', glyphId, ixStatus);

        const loadedPrompt = loadGlyphStatus<GlyphStatus>('prompt', glyphId);
        const loadedIx = loadGlyphStatus<ExecutableGlyphStatus>('ix', glyphId);

        expect(loadedPrompt).toEqual(promptStatus);
        expect(loadedIx).toEqual(ixStatus);
        expect(loadedPrompt).not.toEqual(loadedIx);
    });
});
