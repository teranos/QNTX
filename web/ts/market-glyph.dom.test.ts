/**
 * @jest-environment jsdom
 *
 * Market glyph — what a staand row says, the empty market, and the raise form
 * (ADR-035).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderStaands, buildRaiseForm, type StaandInfo } from './market-glyph.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const noop = (): void => {};

describe('Market glyph', () => {
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

    test('a staand row shows the slug, the ware, the label, the URL and a strike', () => {
        const staands: StaandInfo[] = [
            { slug: 'boutique', ware: 'page:seen', label: 'home', url: '/s/default/boutique' },
            { slug: 'butcher', ware: 'card:scanned', label: 'meat', url: '/s/default/butcher' },
        ];
        renderStaands(container, staands, noop);

        expect(container.textContent).toContain('boutique');
        expect(container.textContent).toContain('page:seen');
        expect(container.textContent).toContain('home');
        expect(container.textContent).toContain('/s/default/boutique');
        expect(container.textContent).toContain('butcher');
        expect(container.textContent).toContain('/s/default/butcher');
        expect(container.textContent).toContain('Strike');
    });

    test('a market with nothing standing says so', () => {
        renderStaands(container, [], noop);
        expect(container.textContent).toContain('Nothing stands in this market yet');
    });

    test('the raise form asks for a slug, a ware and a label, and offers to raise', () => {
        const form = buildRaiseForm(noop);
        container.appendChild(form);

        expect(container.querySelector('.staand-slug')).not.toBeNull();
        expect(container.querySelector('.staand-ware')).not.toBeNull();
        expect(container.querySelector('.staand-label')).not.toBeNull();
        expect(container.textContent).toContain('Raise');
    });
});
