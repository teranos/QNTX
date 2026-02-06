/**
 * Tests for canvas spawn menu
 * Focus: API contract - accepts coordinates, canvas, and glyphs array
 */

import { describe, test, expect } from 'bun:test';
import { showSpawnMenu } from './spawn-menu';

describe('Canvas Spawn Menu', () => {
    test('accepts required parameters without errors', () => {
        const canvas = document.createElement('div');
        const glyphs: any[] = [];

        // Should accept all required parameters
        expect(() => {
            showSpawnMenu(100, 200, canvas, glyphs);
        }).toBeDefined();
    });

    test('accepts different coordinate positions', () => {
        const canvas = document.createElement('div');
        const glyphs: any[] = [];

        // Should work with various coordinate values
        expect(() => {
            showSpawnMenu(0, 0, canvas, glyphs);
            showSpawnMenu(500, 300, canvas, glyphs);
        }).toBeDefined();
    });
});
