/**
 * @jest-environment jsdom
 *
 * Button error display — a rejected onClick must not turn the label into the
 * word "Error" or hide the reason in a hover tooltip. The reason sits beside
 * the button, selectable, and a press copies it (tokens-glyph.ts didCell()).
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { Button } from './button';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Button error box', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;
    let writeText: ReturnType<typeof mock>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);

        writeText = mock(() => Promise.resolve());
        Object.defineProperty(navigator, 'clipboard', {
            value: { writeText },
            configurable: true,
        });
    });

    test('a rejected onClick keeps the label and shows the reason beside the button', async () => {
        const btn = new Button({
            label: 'Save',
            onClick: async () => { throw new Error('the node refused the write'); },
        });
        container.appendChild(btn.element);

        btn.element.click();
        await new Promise(resolve => setTimeout(resolve, 10));

        expect(btn.element.querySelector('.qntx-btn-label')?.textContent).toBe('Save');

        const box = btn.element.nextElementSibling as HTMLElement;
        expect(box?.className).toBe('qntx-btn-error-box');
        expect(box?.textContent).toBe('the node refused the write');
    });

    test('pressing the error copies the full reason to the clipboard', async () => {
        const btn = new Button({
            label: 'Save',
            onClick: async () => { throw new Error('the node refused the write'); },
        });
        container.appendChild(btn.element);

        btn.element.click();
        await new Promise(resolve => setTimeout(resolve, 10));

        const box = btn.element.nextElementSibling as HTMLElement;
        box.click();
        await new Promise(resolve => setTimeout(resolve, 10));

        expect(writeText).toHaveBeenCalledWith('the node refused the write');
        expect(box.textContent).toBe('copied');
    });

    test('a cleared error removes the box', async () => {
        let fail = true;
        const btn = new Button({
            label: 'Save',
            onClick: async () => { if (fail) throw new Error('boom'); },
        });
        container.appendChild(btn.element);

        btn.element.click();
        await new Promise(resolve => setTimeout(resolve, 10));
        expect(btn.element.nextElementSibling).not.toBeNull();

        fail = false;
        btn.element.click();
        await new Promise(resolve => setTimeout(resolve, 10));
        expect(btn.element.nextElementSibling).toBeNull();
    });
});
