/**
 * Tests for config.ts - app configuration and runtime state
 */

import { describe, test, expect } from 'bun:test';
import { appState, GRAPH_PHYSICS, GRAPH_STYLES, UI_TEXT, MAX_LOGS } from './config';

describe('appState', () => {
    test('has correct default values', () => {
        expect(appState.currentVerbosity).toBe(2); // Debug level
        expect(appState.logBuffer).toEqual([]);
        expect(appState.progressBuffer).toEqual([]);
        expect(appState.currentQuery).toBe('');
        expect(appState.currentGraphData).toBeNull();
        expect(appState.currentTransform).toBeNull();
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

    test('GRAPH_PHYSICS has required properties', () => {
        expect(GRAPH_PHYSICS.LINK_DISTANCE).toBeGreaterThan(0);
        expect(GRAPH_PHYSICS.ZOOM_MIN).toBeLessThan(GRAPH_PHYSICS.ZOOM_MAX);
        expect(GRAPH_PHYSICS.COLLISION_RADIUS).toBeGreaterThan(0);
    });

    test('GRAPH_STYLES has color values', () => {
        expect(GRAPH_STYLES.NODE_STROKE_COLOR).toMatch(/^#/);
        expect(GRAPH_STYLES.LINK_COLOR).toMatch(/^#/);
    });

    test('UI_TEXT has required strings', () => {
        expect(UI_TEXT.LOADING).toBeTruthy();
        expect(UI_TEXT.CONNECTION_LOST).toBeTruthy();
        expect(UI_TEXT.CONNECTION_RESTORED).toBeTruthy();
    });
});
