/**
 * Tests for generic glyph status storage (IndexedDB)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { saveGlyphStatus, loadGlyphStatus, type GlyphStatus, type ExecutableGlyphStatus } from './glyph-storage';
import { initStorage } from '../../indexeddb-storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup fake-indexeddb if enabled
if (USE_JSDOM) {
    const fakeIndexedDB = await import('fake-indexeddb');
    const FDBKeyRange = (await import('fake-indexeddb/lib/FDBKeyRange')).default;

    // Create a minimal window object with IndexedDB
    const mockWindow = {
        indexedDB: fakeIndexedDB.default,
    };

    globalThis.window = mockWindow as any;
    globalThis.indexedDB = fakeIndexedDB.default as any;
    globalThis.IDBKeyRange = FDBKeyRange as any;
}

describe('Generic Glyph Storage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(async () => {
        // Initialize storage once before tests
        if (!(await import('../../indexeddb-storage')).isStorageInitialized()) {
            await initStorage();
        }
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
