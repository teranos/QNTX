/**
 * Meld feedback — the glow must give back what it took.
 *
 * A glyph that owns an inline boxShadow (a note's post-it shadow) keeps it:
 * everything about a glyph survives every transition (Element Axioma).
 *
 * Personas:
 * - Tim: happy path — glow on, glow off, own shadow back
 * - Spike: the "approaching" state, which glows without a meld class
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { applyMeldFeedback, clearMeldFeedback, clearFeedbackShadow } from './meld-feedback';
import { PROXIMITY_THRESHOLD, MELD_THRESHOLD } from './meld-detect';

let canvas: HTMLElement;
let initiator: HTMLElement;
let target: HTMLElement;

beforeEach(() => {
    document.body.innerHTML = '';
    canvas = document.createElement('div');
    canvas.className = 'canvas-workspace';
    initiator = document.createElement('div');
    target = document.createElement('div');
    canvas.appendChild(initiator);
    canvas.appendChild(target);
    document.body.appendChild(canvas);
});

describe('Tim: glow on, glow off', () => {
    test('within meld range both glyphs glow and wear their classes', () => {
        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 1);

        expect(initiator.style.boxShadow).not.toBe('');
        expect(target.style.boxShadow).not.toBe('');
        expect(initiator.classList.contains('meld-ready')).toBe(true);
        expect(target.classList.contains('meld-target')).toBe(true);
    });

    test('clear removes glow and classes', () => {
        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 1);
        clearMeldFeedback(initiator);

        expect(initiator.style.boxShadow).toBe('');
        expect(target.style.boxShadow).toBe('');
        expect(initiator.classList.contains('meld-ready')).toBe(false);
        expect(target.classList.contains('meld-target')).toBe(false);
    });

    // Drag a note near anything meldable and its post-it shadow must not be
    // gone for good.
    test('a glyph that owns a shadow gets it back', () => {
        target.style.boxShadow = '2px 2px 8px rgba(0, 0, 0, 0.15)';

        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 1);
        expect(target.style.boxShadow).not.toBe('2px 2px 8px rgba(0, 0, 0, 0.15)');

        clearMeldFeedback(initiator);
        expect(target.style.boxShadow).toBe('2px 2px 8px rgba(0, 0, 0, 0.15)');
    });

    test('the owned shadow survives repeated apply/clear cycles', () => {
        target.style.boxShadow = '2px 2px 8px rgba(0, 0, 0, 0.15)';

        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 1);
        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 2);
        applyMeldFeedback(initiator, target, MELD_THRESHOLD - 3);
        clearMeldFeedback(initiator);

        expect(target.style.boxShadow).toBe('2px 2px 8px rgba(0, 0, 0, 0.15)');
    });
});

describe('Spike: the approaching state has no class', () => {
    test('an approaching glow is still found and cleared', () => {
        applyMeldFeedback(initiator, target, PROXIMITY_THRESHOLD - 1);

        expect(target.style.boxShadow).not.toBe('');
        expect(target.classList.contains('meld-target')).toBe(false);

        clearMeldFeedback(initiator);
        expect(target.style.boxShadow).toBe('');
    });

    test('an approaching glow gives back an owned shadow', () => {
        target.style.boxShadow = '1px 1px 2px black';

        applyMeldFeedback(initiator, target, PROXIMITY_THRESHOLD - 1);
        clearMeldFeedback(initiator);

        expect(target.style.boxShadow).toBe('1px 1px 2px black');
    });

    test('clearFeedbackShadow on an untouched element changes nothing', () => {
        target.style.boxShadow = '1px 1px 2px black';
        clearFeedbackShadow(target);
        expect(target.style.boxShadow).toBe('1px 1px 2px black');
    });
});
