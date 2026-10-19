// Presses in markup: a template says where each one stands, and it is put there once the markup is in.

import { describe, it, expect } from 'bun:test';
import { pressPlace, placePresses } from './press-places';

describe('placePresses', () => {
    it('puts each press where its place stands, and says each one it put', () => {
        const container = document.createElement('div');
        container.innerHTML = `<div class="row">${pressPlace('save-1')}${pressPlace('drop-1')}</div>`;
        const placed = placePresses(container, {
            'save-1': { id: 'save-1', title: 'Save', does: () => {} },
            'drop-1': { id: 'drop-1', title: 'Drop', again: 'it is dropped', does: () => {} },
        });
        const row = container.querySelector('.row')!;
        expect(Array.from(row.children).map((c) => (c as HTMLElement).dataset.press)).toEqual(['save-1', 'drop-1']);
        expect(placed['drop-1']!.element.dataset.press).toBe('drop-1');
        expect(container.querySelector('[data-press-at]')).toBeNull();
    });

    it('a place with no press is a mistake said out loud; a press with no place is never made', () => {
        const container = document.createElement('div');
        container.innerHTML = pressPlace('save-1');
        expect(() => placePresses(container, {})).toThrow('save-1');
        container.innerHTML = '';
        expect(placePresses(container, { 'save-1': { id: 'save-1', title: 'Save', does: () => {} } })).toEqual({});
    });

    it('a place can name a class of the page for what lays the press out', () => {
        const container = document.createElement('div');
        container.innerHTML = pressPlace('add-1', 'plugin-add-confirm');
        placePresses(container, { 'add-1': { id: 'add-1', title: 'Add', does: () => {} } });
        expect(container.querySelector('[data-press="add-1"]')!.classList.contains('plugin-add-confirm')).toBe(true);
    });

    it('a place says its id as markup safely', () => {
        expect(pressPlace('a"b<c')).toBe('<span data-press-at="a&quot;b&lt;c" data-press-class=""></span>');
    });
});
