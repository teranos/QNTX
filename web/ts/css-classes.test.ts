/**
 * Tests for data-attribute helpers
 *
 * Verifies that type-safe helpers correctly set data attributes on elements
 */

import { describe, it, expect } from 'bun:test';
import { Window } from 'happy-dom';
import {
    DATA,
    setVisibility,
    getVisibility,
    setExpansion,
    setActive,
    setLoading,
} from './css-classes';

// Setup happy-dom for DOM testing
const window = new Window();
const document = window.document;
globalThis.document = document as any;

describe('data-attribute helpers', () => {
    it('setVisibility sets data-visibility attribute', () => {
        const el = document.createElement('div');

        setVisibility(el, DATA.VISIBILITY.HIDDEN);
        expect(el.dataset.visibility).toBe('hidden');

        setVisibility(el, DATA.VISIBILITY.VISIBLE);
        expect(el.dataset.visibility).toBe('visible');

        setVisibility(el, DATA.VISIBILITY.FADING);
        expect(el.dataset.visibility).toBe('fading');
    });

    it('getVisibility returns current state', () => {
        const el = document.createElement('div');

        expect(getVisibility(el)).toBeUndefined();

        el.dataset.visibility = 'fading';
        expect(getVisibility(el)).toBe('fading');
    });

    it('setExpansion sets data-expansion attribute', () => {
        const el = document.createElement('div');

        setExpansion(el, DATA.EXPANSION.COLLAPSED);
        expect(el.dataset.expansion).toBe('collapsed');

        setExpansion(el, DATA.EXPANSION.EXPANDED);
        expect(el.dataset.expansion).toBe('expanded');
    });

    it('setActive sets data-active attribute', () => {
        const el = document.createElement('div');

        setActive(el, DATA.ACTIVE.INACTIVE);
        expect(el.dataset.active).toBe('inactive');

        setActive(el, DATA.ACTIVE.SELECTED);
        expect(el.dataset.active).toBe('selected');
    });

    it('setLoading sets data-loading attribute', () => {
        const el = document.createElement('div');

        setLoading(el, DATA.LOADING.IDLE);
        expect(el.dataset.loading).toBe('idle');

        setLoading(el, DATA.LOADING.LOADING);
        expect(el.dataset.loading).toBe('loading');

        setLoading(el, DATA.LOADING.ERROR);
        expect(el.dataset.loading).toBe('error');

        setLoading(el, DATA.LOADING.SUCCESS);
        expect(el.dataset.loading).toBe('success');
    });

    it('handles null element gracefully', () => {
        // None of these should throw
        expect(() => setVisibility(null, DATA.VISIBILITY.HIDDEN)).not.toThrow();
        expect(() => setExpansion(null, DATA.EXPANSION.COLLAPSED)).not.toThrow();
        expect(() => setActive(null, DATA.ACTIVE.INACTIVE)).not.toThrow();
        expect(() => setLoading(null, DATA.LOADING.IDLE)).not.toThrow();
        expect(getVisibility(null)).toBeUndefined();
    });

    it('handles undefined element gracefully', () => {
        expect(() => setVisibility(undefined, DATA.VISIBILITY.HIDDEN)).not.toThrow();
        expect(getVisibility(undefined)).toBeUndefined();
    });
});
