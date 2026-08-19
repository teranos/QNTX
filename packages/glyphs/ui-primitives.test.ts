/**
 * GlyphUI DOM primitives — the pure half of the GlyphUI factory (GLYUI).
 *
 * Personas:
 * - Tim: happy path — the building blocks build what plugins expect
 * - Spike: the status line's timer behavior
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createInput, createButton, createStatusLine } from './ui-primitives';

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: building blocks', () => {
    test('an input with a label', () => {
        const wrapper = createInput({ label: 'Name', placeholder: 'type here', value: 'x' });
        expect(wrapper.className).toBe('glyph-form-group');
        expect(wrapper.querySelector('label')!.textContent).toBe('Name');
        const input = wrapper.querySelector('input')!;
        expect(input.placeholder).toBe('type here');
        expect(input.value).toBe('x');
        expect(input.type).toBe('text');
    });

    test('an input without options is a bare text field', () => {
        const wrapper = createInput();
        expect(wrapper.querySelector('label')).toBeNull();
        expect(wrapper.querySelector('input')!.type).toBe('text');
    });

    test('a button fires its handler', () => {
        let fired = 0;
        const btn = createButton({ label: 'Run', onClick: () => { fired++; } });
        expect(btn.textContent).toBe('Run');
        expect(btn.className).toBe('glyph-btn');
        btn.click();
        expect(fired).toBe(1);
    });

    test('a primary button wears the modifier', () => {
        const btn = createButton({ label: 'Save', onClick: () => {}, primary: true });
        expect(btn.className).toBe('glyph-btn glyph-btn--primary');
    });
});

describe('Spike: status line', () => {
    test('show puts the message up, clear takes it down', () => {
        const status = createStatusLine();
        status.show('saved');
        expect(status.element.textContent).toBe('saved');
        status.clear();
        expect(status.element.textContent).toBe('');
    });

    test('an error message stays until cleared', () => {
        const status = createStatusLine();
        status.show('broken', true);
        expect(status.element.textContent).toBe('broken');
    });
});
