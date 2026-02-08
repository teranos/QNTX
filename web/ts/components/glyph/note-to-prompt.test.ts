/**
 * Tests for glyph conversions
 * Focus: API contract and error handling (fast tests)
 */

import { describe, test, expect } from 'bun:test';
import { convertNoteToPrompt } from './conversions';

describe('Note to Prompt Conversion - API Contract', () => {
    test('returns boolean for missing element', async () => {
        const container = document.createElement('div');
        const result = await convertNoteToPrompt(container, 'nonexistent-id');

        expect(typeof result).toBe('boolean');
        expect(result).toBe(false);
    });

    test('accepts valid parameters without throwing', async () => {
        const container = document.createElement('div');

        await expect(async () => {
            await convertNoteToPrompt(container, 'note-123');
        }).not.toThrow();
    });
});
