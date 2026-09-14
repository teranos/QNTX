/**
 * Tests for the content-state list.
 *
 * Personas:
 * - Tim: Happy path — the table names every state a body can be in
 * - Spike: Edge cases — a word is not a state, and the table cannot be edited
 * - Jenny: Complex scenarios — reading a state back off an element
 */

import { describe, test, expect } from 'bun:test';
import {
    CONTENT_STATES,
    isContentState,
    isSettled,
    type ContentState,
} from './content-state';
import { setContentState, getContentState } from './dataset';

const names = Object.keys(CONTENT_STATES) as ContentState[];

describe('Tim: the table', () => {
    test('names every state a body can be in', () => {
        expect([...names].sort()).toEqual(['empty', 'pending', 'present', 'refused']);
    });

    test('pending is the only state still moving', () => {
        const moving = names.filter(n => !CONTENT_STATES[n].settled);
        expect(moving).toEqual(['pending']);
    });

    test('empty and present are both settled — both draw', () => {
        expect(isSettled('empty')).toBe(true);
        expect(isSettled('present')).toBe(true);
    });
});

describe('Spike: a word is not a state', () => {
    test('refuses a name the table does not have', () => {
        expect(isContentState('loading')).toBe(false);
        expect(isContentState('')).toBe(false);
        expect(isSettled('loading')).toBe(false);
    });

    test('refuses inherited names', () => {
        expect(isContentState('toString')).toBe(false);
        expect(isContentState('constructor')).toBe(false);
    });

    test('the table and its rows are frozen', () => {
        expect(Object.isFrozen(CONTENT_STATES)).toBe(true);
        for (const name of names) {
            expect(Object.isFrozen(CONTENT_STATES[name])).toBe(true);
        }
    });
});

describe('Jenny: written on the element, read back off it', () => {
    test('a state survives the round trip', () => {
        const element = document.createElement('div');
        for (const name of names) {
            setContentState(element, name);
            expect(getContentState(element)).toBe(name);
        }
    });

    test('an unstamped body says nothing rather than guessing', () => {
        const element = document.createElement('div');
        expect(getContentState(element)).toBeNull();
    });

    test('a body carrying a word is carrying a word, not a state', () => {
        const element = document.createElement('div');
        element.dataset.content = 'loading';
        expect(getContentState(element)).toBeNull();
    });
});
