/**
 * Comet element — one repository as a comet, and what state it is in.
 *
 * Personas: Tim (happy path), Spike (edge cases).
 */

import { describe, test, expect } from 'bun:test';
import { cometBody } from './comet-element';

describe('Comet - Tim', () => {
    // "it opens the comet element"
    test('the comet is drawn alone, named, with its state', () => {
        const body = cometBody({ repo: 'teranos/ground', built: true, size: 1.6, moved: 60, state: 'observed' });
        expect(body.querySelector('.gr-name b')?.textContent).toBe('Comet');
        expect(body.querySelector('.comet-repo')?.textContent).toBe('teranos/ground');
        expect(body.querySelector('.comet-plate svg.gr-plate')).not.toBeNull();
        expect(body.querySelector('.comet-plate .gr-comet-burst')).not.toBeNull();
        expect(body.querySelector('.comet-state')?.textContent).toBe('Observed. When main moves, ground moves.');
    });
});

describe('Comet - Spike', () => {
    // What is not in the node yet is said on tape, not offered as a button.
    test('nothing here observes: the limit says so and no button is drawn', () => {
        const body = cometBody({ repo: 'teranos/new', built: false, size: 0, moved: 2, state: 'unobserved' });
        expect(body.querySelector('.comet-state')?.textContent).toBe('Not observed.');
        expect(body.querySelector('.gr-limit')?.textContent).toContain('not in the node yet');
        expect(body.querySelector('button')).toBeNull();
    });
});
