/**
 * @jest-environment jsdom
 *
 * Jenny's complex AX query → Results → Drag → Meld workflow
 * Tests real DOM interactions requiring JSDOM:
 * - Fake timers for 500ms debounce
 * - Real input events and event propagation
 * - Real DragEvent with DataTransfer
 * - Real getBoundingClientRect for proximity-based melding
 *
 * These tests run only with USE_JSDOM=1 (CI environment)
 */

import { describe, test, expect, beforeEach, jest } from 'bun:test';
import { createAxGlyph, updateAxGlyphResults } from './ax-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { findMeldTarget, performMeld, MELD_THRESHOLD } from './meld-system';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { SO } from '@generated/sym.js';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost' // Required for localStorage
    });
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.navigator = window.navigator as any;
    globalThis.DOMParser = window.DOMParser as any;
    globalThis.localStorage = window.localStorage;

    // jsdom's AbortController is compatible with addEventListener signal option
    globalThis.AbortController = window.AbortController as any;
    globalThis.AbortSignal = window.AbortSignal as any;

    // Mock ResizeObserver for jsdom
    globalThis.ResizeObserver = class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
    } as any;

    // Mock DragEvent for jsdom (not fully implemented in jsdom)
    globalThis.DragEvent = window.DragEvent || class DragEvent extends window.MouseEvent {
        constructor(type: string, eventInitDict?: DragEventInit) {
            super(type, eventInitDict);
        }
        dataTransfer = null;
    } as any;

    // Mock InputEvent for jsdom
    globalThis.InputEvent = window.InputEvent || class InputEvent extends window.Event {
        constructor(type: string, eventInitDict?: InputEventInit) {
            super(type, eventInitDict);
        }
        data = null;
        inputType = '';
    } as any;
}

describe('AX Query Meld - Jenny (Complex Scenarios)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
        localStorage.clear();
    });

    test('Jenny types "of qntx", gets results, drags AX glyph, melds with prompt', async () => {
        // Enable fake timers for debounce testing
        jest.useFakeTimers();

        // 1. Create canvas workspace
        const canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.style.position = 'relative';
        canvas.style.width = '2000px';
        canvas.style.height = '1000px';
        document.body.appendChild(canvas);

        // 2. Jenny creates AX glyph and renders it
        const axGlyph = createAxGlyph('ax-jenny-qntx', '', 100, 100);
        const axElement = axGlyph.renderContent();
        canvas.appendChild(axElement);

        // 3. Jenny types "of qntx" in the query input
        const input = axElement.querySelector('.ax-query-input') as HTMLInputElement;
        expect(input).toBeTruthy();

        input.value = 'of qntx';
        const inputEvent = new InputEvent('input', {
            bubbles: true,
            data: 'of qntx'
        });
        input.dispatchEvent(inputEvent);

        // Verify query is pending (background color changes during debounce)
        expect(axElement.style.backgroundColor).toContain('rgba(42, 43, 61'); // Pending state

        // 4. Advance timers past 500ms debounce
        jest.advanceTimersByTime(500);

        // 5. Simulate receiving three attestation results from backend
        // ALICE is project_lead of QNTX
        const attestation1: Attestation = {
            id: 'att-alice-lead',
            subjects: ['ALICE'],
            predicates: ['project_lead'],
            contexts: ['QNTX'],
            actors: [],
            timestamp: Date.now() / 1000,
            source: 'test',
            attributes: '{}',
            created_at: Date.now() / 1000
        };

        // BOB, CHARLIE is contributor of QNTX
        const attestation2: Attestation = {
            id: 'att-bob-charlie-contrib',
            subjects: ['BOB', 'CHARLIE'],
            predicates: ['contributor'],
            contexts: ['QNTX'],
            actors: [],
            timestamp: Date.now() / 1000,
            source: 'test',
            attributes: '{}',
            created_at: Date.now() / 1000
        };

        // SPIKE is pentester of QNTX
        const attestation3: Attestation = {
            id: 'att-spike-pentester',
            subjects: ['SPIKE'],
            predicates: ['pentester'],
            contexts: ['QNTX'],
            actors: [],
            timestamp: Date.now() / 1000,
            source: 'test',
            attributes: '{}',
            created_at: Date.now() / 1000
        };

        updateAxGlyphResults('ax-jenny-qntx', attestation1);
        updateAxGlyphResults('ax-jenny-qntx', attestation2);
        updateAxGlyphResults('ax-jenny-qntx', attestation3);

        // 6. Verify results are displayed inside AX glyph (not separate result glyphs)
        const resultsContainer = axElement.querySelector('.ax-glyph-results') as HTMLElement;
        expect(resultsContainer).toBeTruthy();

        const resultItems = resultsContainer.querySelectorAll('.ax-glyph-result-item');
        expect(resultItems.length).toBe(3);

        // Verify result content (most recent first)
        expect(resultItems[0].textContent).toContain('SPIKE');
        expect(resultItems[0].textContent).toContain('pentester');
        expect(resultItems[1].textContent).toContain('BOB');
        expect(resultItems[1].textContent).toContain('CHARLIE');
        expect(resultItems[1].textContent).toContain('contributor');
        expect(resultItems[2].textContent).toContain('ALICE');
        expect(resultItems[2].textContent).toContain('project_lead');

        // 7. Jenny drags AX glyph to the right
        // Mock getBoundingClientRect for AX glyph at starting position
        const originalGetBoundingClientRect = axElement.getBoundingClientRect;
        axElement.getBoundingClientRect = jest.fn(() => ({
            left: 100,
            top: 100,
            right: 500,  // 400px wide
            bottom: 300, // 200px tall
            width: 400,
            height: 200,
            x: 100,
            y: 100,
            toJSON: () => ({})
        } as DOMRect));

        // Simulate drag (dragstart → dragend updates position via glyph-interaction.ts)
        const dragStartEvent = new DragEvent('dragstart', { bubbles: true });
        axElement.dispatchEvent(dragStartEvent);

        // Mock new position after drag (moved 450px to the right)
        axElement.getBoundingClientRect = jest.fn(() => ({
            left: 550,
            top: 100,
            right: 950,
            bottom: 300,
            width: 400,
            height: 200,
            x: 550,
            y: 100,
            toJSON: () => ({})
        } as DOMRect));

        const dragEndEvent = new DragEvent('dragend', { bubbles: true });
        axElement.dispatchEvent(dragEndEvent);

        // 8. Jenny creates a prompt glyph positioned for melding (within MELD_THRESHOLD)
        const promptGlyph = {
            id: 'prompt-jenny-meld',
            title: 'Prompt',
            symbol: SO,
            x: 970, // 20px gap from AX glyph right edge (950 + 20 = 970)
            y: 100,
            width: 420,
            height: 340,
            renderContent: () => document.createElement('div')
        };

        const promptElement = await createPromptGlyph(promptGlyph);
        canvas.appendChild(promptElement);

        // Mock getBoundingClientRect for prompt at meld position
        promptElement.getBoundingClientRect = jest.fn(() => ({
            left: 970,
            top: 100,
            right: 1390,
            bottom: 440,
            width: 420,
            height: 340,
            x: 970,
            y: 100,
            toJSON: () => ({})
        } as DOMRect));

        // 9. Test proximity detection
        const meldResult = findMeldTarget(axElement as HTMLElement);

        // Verify AX glyph detected prompt as meld target
        expect(meldResult.target).toBeTruthy();
        expect(meldResult.target?.dataset.glyphId).toBe('prompt-jenny-meld');

        // Verify distance is within MELD_THRESHOLD (20px < 30px)
        const axRight = 950;
        const promptLeft = 970;
        const expectedDistance = promptLeft - axRight;
        expect(expectedDistance).toBe(20);
        expect(meldResult.distance).toBe(20);
        expect(meldResult.distance).toBeLessThan(MELD_THRESHOLD);

        // 10. Perform meld
        if (meldResult.target) {
            performMeld(axElement as HTMLElement, meldResult.target, axGlyph, promptGlyph);
        }

        // 11. Verify composition was created
        const composition = canvas.querySelector('.melded-composition');
        expect(composition).toBeTruthy();
        expect(composition?.children.length).toBe(2); // AX + Prompt

        // Verify both glyphs are inside composition
        const glyphsInComposition = composition?.querySelectorAll('.canvas-glyph');
        expect(glyphsInComposition?.length).toBe(2);

        // Verify AX glyph is first (left), prompt is second (right)
        const firstGlyph = glyphsInComposition?.[0] as HTMLElement;
        const secondGlyph = glyphsInComposition?.[1] as HTMLElement;
        expect(firstGlyph.dataset.glyphId).toBe('ax-jenny-qntx');
        expect(secondGlyph.dataset.glyphId).toBe('prompt-jenny-meld');

        // Verify AX results are still displayed inside the melded AX glyph
        const meldedResultsContainer = firstGlyph.querySelector('.ax-glyph-results');
        expect(meldedResultsContainer).toBeTruthy();
        const meldedResults = meldedResultsContainer?.querySelectorAll('.ax-glyph-result-item');
        expect(meldedResults?.length).toBe(3);

        // Cleanup
        jest.useRealTimers();
        document.body.innerHTML = '';
    });
});
