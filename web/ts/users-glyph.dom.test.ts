/**
 * @jest-environment jsdom
 *
 * Users glyph — what a row says and which control it offers (ADR-031).
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderList, nameOf } from './users-glyph.ts';

const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('Users glyph rows', () => {
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

    test('Tim de Facile is on, with what he said and an offer to switch him off', () => {
        renderList(container, [{
            id: 'US-TIM-1',
            display_name: 'Tim de Facile',
            email_addresses: ['tim@example.com'],
            phone_numbers: ['+31612345678', '0201234567'],
            level: 'ATTESTOR',
            created_at: 1_700_000_000,
        }]);

        expect(container.textContent).toContain('Tim de Facile');
        expect(container.textContent).toContain('tim@example.com');
        expect(container.textContent).toContain('+31612345678, 0201234567');
        expect(container.textContent).toContain('Switch off');
        expect(container.textContent).not.toContain('Switch on');
    });

    test('a User ROOT switched off says so, and by whom, and offers to switch on', () => {
        renderList(container, [{
            id: 'US-TIM-1',
            display_name: 'Tim de Facile',
            level: 'ATTESTOR',
            disabled_by: 'US-ROOT-1',
            created_at: 1_700_000_000,
        }]);

        expect(container.textContent).toContain('off');
        expect(container.textContent).toContain('by US-ROOT-1');
        expect(container.textContent).toContain('Switch on');
        expect(container.textContent).not.toContain('Switch off');
    });

    test('the ROOT User is root without saying so, and a quiet User is a dash', () => {
        expect(nameOf({ id: 'US-R', level: 'ROOT', created_at: 1 })).toBe('root');
        expect(nameOf({ id: 'US-Q', level: 'ATTESTOR', created_at: 1 })).toBe('—');
        expect(nameOf({ id: 'US-T', level: 'ROOT', display_name: 'Tim de Facile', created_at: 1 })).toBe('Tim de Facile');
    });
});
