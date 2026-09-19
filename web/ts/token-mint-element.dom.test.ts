/**
 * @jest-environment jsdom
 *
 * Mint Token element — which kind a mint opens on (ADR-025).
 *
 * "making a selection between two mutually exclusive options and it being
 * instantiated having neither selected"
 *
 * A select opened on its first option, which was SUPER, and a mint that never
 * touched it minted the widest kind. The rows open on none.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { kindRows, renderMint, KINDS, ATTESTOR, OAUTH } from './token-mint-element.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Mint Token kind rows', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    function rows(): HTMLButtonElement[] {
        return Array.from(container.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    }

    test('opens with no kind pressed', () => {
        const kind = kindRows();
        container.appendChild(kind.element);

        expect(kind.value).toBe('');
        expect(rows()).toHaveLength(KINDS.length);
        for (const row of rows()) {
            expect(row.getAttribute('aria-checked')).toBe('false');
        }
    });

    test('pressing a row names the kind, and pressing another moves it', () => {
        const kind = kindRows();
        container.appendChild(kind.element);
        let heard = 0;
        kind.onChange(() => { heard++; });

        rows().find(r => r.dataset.kind === OAUTH)!.click();
        expect(kind.value).toBe(OAUTH);
        expect(rows().filter(r => r.getAttribute('aria-checked') === 'true').map(r => r.dataset.kind)).toEqual([OAUTH]);

        rows().find(r => r.dataset.kind === ATTESTOR)!.click();
        expect(kind.value).toBe(ATTESTOR);
        expect(rows().filter(r => r.getAttribute('aria-checked') === 'true').map(r => r.dataset.kind)).toEqual([ATTESTOR]);
        expect(heard).toBe(2);
    });

    test('the form asks for what the pressed kind needs and nothing else', () => {
        renderMint(container);
        const labelOf = (text: string) =>
            Array.from(container.querySelectorAll('label')).find(l => l.textContent?.startsWith(text))!;

        // Nothing pressed: neither the narrowing nor the return address is asked for.
        expect(labelOf('Namespace').hidden).toBe(true);
        expect(labelOf('Return address').hidden).toBe(true);

        rows().find(r => r.dataset.kind === ATTESTOR)!.click();
        expect(labelOf('Namespace').hidden).toBe(false);
        expect(labelOf('Return address').hidden).toBe(true);

        rows().find(r => r.dataset.kind === OAUTH)!.click();
        expect(labelOf('Namespace').hidden).toBe(true);
        expect(labelOf('Return address').hidden).toBe(false);
    });

    test('a mint with no kind pressed is refused before it reaches the node', async () => {
        renderMint(container);
        const label = container.querySelector<HTMLInputElement>('input')!;
        label.value = 'app';

        const mint = Array.from(container.querySelectorAll('button')).find(b => b.textContent?.includes('Mint token'))!;
        mint.click();
        await new Promise(resolve => setTimeout(resolve, 0));

        expect(container.querySelector('.tokens-refusal')?.textContent).toBe('no kind');
    });
});
