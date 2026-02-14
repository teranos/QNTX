/**
 * Tests for config.ts constants and state/app.ts runtime state
 */

import { describe, test, expect } from 'bun:test';
import { UI_TEXT } from './config';
import { appState, MAX_LOGS } from './state/app';

describe('appState', () => {
    test('has correct default values', () => {
        expect(appState.currentVerbosity).toBe(2); // Debug level
        expect(appState.logBuffer).toEqual([]);
        expect(appState.progressBuffer).toEqual([]);
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
    test('MAX_LOGS is a reasonable limit', () => {
        expect(MAX_LOGS).toBeGreaterThan(0);
        expect(MAX_LOGS).toBeLessThanOrEqual(10000);
    });

    test('UI_TEXT has required strings', () => {
        expect(UI_TEXT.LOADING).toBeTruthy();
        expect(UI_TEXT.CONNECTION_LOST).toBeTruthy();
        expect(UI_TEXT.CONNECTION_RESTORED).toBeTruthy();
    });
});
