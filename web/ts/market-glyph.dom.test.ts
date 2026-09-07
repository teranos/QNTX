/**
 * @jest-environment jsdom
 *
 * Market glyph — a staand row, the empty market, and the create form (ADR-035).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderStaands, buildCreateForm, snippet, type StaandInfo } from './market-glyph.ts';

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

    test('a staand row shows the predicate, the full URL, a snippet, and delete', () => {
        const staands: StaandInfo[] = [
            { slug: 'boutique', predicate: 'page:seen', label: 'home', url: '/s/clean/boutique' },
        ];
        renderStaands(container, 'clean', staands, noop);

        expect(container.textContent).toContain('boutique');
        expect(container.textContent).toContain('page:seen');
        expect(container.textContent).toContain('home');
        // Full URL carries the path, host prefixed by backendUrl().
        expect(container.textContent).toContain('/s/clean/boutique');
        expect(container.textContent).toContain('copy snippet');
        expect(container.textContent).toContain('Delete');
    });

    test('the snippet is a pasteable img whose subject defaults to the slug', () => {
        const s = snippet('/s/clean/boutique', 'boutique');
        expect(s).toContain('<img');
        expect(s).toContain('/s/clean/boutique?subject=boutique');
    });

    test('a market with nothing standing says so', () => {
        renderStaands(container, 'clean', [], noop);
        expect(container.textContent).toContain('No staands in this market yet');
    });

    test('the create form asks for a slug, a predicate and a label, and offers to create', () => {
        const form = buildCreateForm('clean', noop);
        container.appendChild(form);

        expect(container.querySelector('.staand-slug')).not.toBeNull();
        expect(container.querySelector('.staand-predicate')).not.toBeNull();
        expect(container.querySelector('.staand-label')).not.toBeNull();
        expect(container.textContent).toContain('Create');
    });
});
