/**
 * @jest-environment jsdom
 *
 * "i expect to not see any canvas if a namespace has none"
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { askForACanvas } from './canvas-birth.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('a namespace with no canvas', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '<main id="container"></main>';
    });

    const enter = (input: HTMLInputElement, value: string) => {
        input.value = value;
        input.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    };

    test('shows ⌗ and a name field, and nothing else', () => {
        const host = document.getElementById('container')!;
        askForACanvas(host, async () => {});
        expect(host.children.length).toBe(1);
        expect(host.querySelector('.canvas-birth-symbol')?.textContent).toBe('⌗');
        expect(host.querySelector('input.canvas-birth-name')).not.toBeNull();
    });

    test('Enter creates the canvas under the name typed', async () => {
        const host = document.getElementById('container')!;
        let created = '';
        askForACanvas(host, async (name) => { created = name; });
        enter(host.querySelector('input')!, '  aws  ');
        await Promise.resolve();
        expect(created).toBe('aws');
    });

    test('no name is no canvas', () => {
        const host = document.getElementById('container')!;
        let asked = false;
        askForACanvas(host, async () => { asked = true; });
        enter(host.querySelector('input')!, '   ');
        expect(asked).toBe(false);
    });

    test('the node\'s refusal is shown, in its words', async () => {
        const host = document.getElementById('container')!;
        askForACanvas(host, async () => { throw new Error('this namespace already has a canvas'); });
        enter(host.querySelector('input')!, 'aws');
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(host.querySelector('.canvas-birth-said')?.textContent).toBe('this namespace already has a canvas');
        expect(host.querySelector('input')!.disabled).toBe(false);
    });
});
