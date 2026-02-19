/**
 * Tests for config.ts constants and state/app.ts runtime state
 */

import { describe, test, expect } from 'bun:test';
import { UI_TEXT } from './config';
import { appState } from './state/app';

describe('appState', () => {
    test('has correct default values', () => {
        expect(appState.currentVerbosity).toBe(2); // Debug level
        expect(appState.currentQuery).toBe('');
    });

    test('is mutable for runtime updates', () => {
        const originalVerbosity = appState.currentVerbosity;
        appState.currentVerbosity = 5;
        expect(appState.currentVerbosity).toBe(5);
        appState.currentVerbosity = originalVerbosity; // Restore
    });
});

describe('Constants', () => {
    test('UI_TEXT has required strings', () => {
        expect(UI_TEXT.LOADING).toBeTruthy();
        expect(UI_TEXT.CONNECTION_LOST).toBeTruthy();
        expect(UI_TEXT.CONNECTION_RESTORED).toBeTruthy();
    });
});
