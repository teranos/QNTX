/**
 * Tests for note-to-prompt conversion
 * Focus: DOM integration and full conversion workflow
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { convertNoteToPrompt } from './note-to-prompt';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Note to Prompt Conversion - DOM Integration', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        container.className = 'canvas-workspace';
        document.body.appendChild(container);
        localStorage.clear();
    });

    test('returns false when note element not found', async () => {
        const result = await convertNoteToPrompt(container, 'nonexistent-note');
        expect(result).toBe(false);
    });

    test('returns false for note with missing glyph ID', async () => {
        const noteElement = document.createElement('div');
        noteElement.dataset.glyphSymbol = '▣';
        // No dataset.glyphId set
        container.appendChild(noteElement);

        const result = await convertNoteToPrompt(container, 'note-123');
        expect(result).toBe(false);
    });

    test('converts note to prompt and returns true', async () => {
        // Create mock note element
        const noteElement = document.createElement('div');
        noteElement.dataset.glyphId = 'note-123';
        noteElement.dataset.glyphSymbol = '▣';
        noteElement.style.position = 'absolute';
        noteElement.style.left = '100px';
        noteElement.style.top = '200px';
        noteElement.style.width = '300px';
        noteElement.style.height = '250px';
        container.appendChild(noteElement);

        // Mock storage with note content
        const mockContent = '# My Note\n\nSome content';
        localStorage.setItem('qntx-script:note-123', mockContent);

        const result = await convertNoteToPrompt(container, 'note-123');

        expect(result).toBe(true);

        // Note element should be removed
        expect(container.querySelector('[data-glyph-id="note-123"]')).toBeNull();

        // Prompt element should be created
        const promptElement = container.querySelector('[data-glyph-symbol="⟶"]');
        expect(promptElement).not.toBeNull();

        // Old note storage should be cleaned up
        expect(localStorage.getItem('qntx-script:note-123')).toBeNull();
    });

    test('preserves note position and size on conversion', async () => {
        const noteElement = document.createElement('div');
        noteElement.dataset.glyphId = 'note-456';
        noteElement.dataset.glyphSymbol = '▣';
        noteElement.style.position = 'absolute';
        noteElement.style.left = '150px';
        noteElement.style.top = '250px';
        noteElement.style.width = '400px';
        noteElement.style.height = '300px';
        container.appendChild(noteElement);

        localStorage.setItem('qntx-script:note-456', 'Test content');

        await convertNoteToPrompt(container, 'note-456');

        const promptElement = container.querySelector('[data-glyph-symbol="⟶"]') as HTMLElement;
        expect(promptElement).not.toBeNull();
        expect(promptElement?.style.left).toBe('150px');
        expect(promptElement?.style.top).toBe('250px');
        expect(promptElement?.style.width).toBe('400px');
        expect(promptElement?.style.height).toBe('300px');
    });

    test('transfers note content to prompt textarea', async () => {
        const noteElement = document.createElement('div');
        noteElement.dataset.glyphId = 'note-789';
        noteElement.dataset.glyphSymbol = '▣';
        noteElement.style.position = 'absolute';
        noteElement.style.left = '100px';
        noteElement.style.top = '100px';
        container.appendChild(noteElement);

        const noteContent = '---\nmodel: "anthropic/claude-haiku-4.5"\n---\nTransferred content';
        localStorage.setItem('qntx-script:note-789', noteContent);

        await convertNoteToPrompt(container, 'note-789');

        const textarea = container.querySelector('textarea');
        expect(textarea).not.toBeNull();
        expect(textarea?.value).toBe(noteContent);
    });
});
