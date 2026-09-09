/**
 * Tests for the manifestation list.
 *
 * Personas:
 * - Tim: Happy path — the list is what the package's own files say it is
 * - Spike: Edge cases — the tray's destinations are a subset, and cursor is outside
 * - Jenny: Complex scenarios — every manifestation the code morphs to is named
 */

import { describe, test, expect } from 'bun:test';
import {
    MANIFESTATIONS,
    TRAY_DESTINATIONS,
    isManifestation,
    isTrayDestination,
    type Manifestation,
    type TrayDestination,
} from './manifestation';

describe('Tim: the list', () => {
    test('names every manifestation the package implements', () => {
        expect([...MANIFESTATIONS].sort()).toEqual([
            'canvas',
            'canvasPlaced',
            'cursor',
            'dot',
            'panel',
            'proximity',
            'window',
        ]);
    });

    test('holds the sequence proximity.ts documents: dot to proximity to window to dot', () => {
        for (const name of ['dot', 'proximity', 'window'] as const) {
            expect(MANIFESTATIONS).toContain(name);
        }
    });

    test('is frozen — the list is read, never edited at runtime', () => {
        expect(Object.isFrozen(MANIFESTATIONS)).toBe(true);
    });

    test('has no duplicates', () => {
        expect(new Set(MANIFESTATIONS).size).toBe(MANIFESTATIONS.length);
    });
});

describe('Spike: the tray opens onto a subset', () => {
    test('a dot morphs into window, panel or canvas', () => {
        expect([...TRAY_DESTINATIONS].sort()).toEqual(['canvas', 'panel', 'window']);
    });

    test('every tray destination is a manifestation', () => {
        for (const dest of TRAY_DESTINATIONS) {
            expect(MANIFESTATIONS).toContain(dest);
        }
    });

    test('dot is not a destination — it is where the morph starts', () => {
        expect(TRAY_DESTINATIONS).not.toContain('dot' as TrayDestination);
    });

    test('proximity is not a destination — it is the way there', () => {
        expect(TRAY_DESTINATIONS).not.toContain('proximity' as TrayDestination);
    });

    test('cursor is not a destination — cursor.ts says it stays outside the tray morph lifecycle', () => {
        expect(TRAY_DESTINATIONS).not.toContain('cursor' as TrayDestination);
    });

    test('canvasPlaced is not a destination — a tray dot does not become one', () => {
        expect(TRAY_DESTINATIONS).not.toContain('canvasPlaced' as TrayDestination);
    });
});

describe('Jenny: recognising a name', () => {
    test('isManifestation accepts each name on the list', () => {
        for (const name of MANIFESTATIONS) {
            expect(isManifestation(name)).toBe(true);
        }
    });

    test('isManifestation rejects a name that is not on it', () => {
        expect(isManifestation('fullscreen')).toBe(false);
        expect(isManifestation('canvas-placed')).toBe(false);
        expect(isManifestation('')).toBe(false);
    });

    test('isTrayDestination narrows to what a dot may open as', () => {
        expect(isTrayDestination('panel')).toBe(true);
        expect(isTrayDestination('dot')).toBe(false);
        expect(isTrayDestination('cursor')).toBe(false);
    });

    test('a Manifestation is assignable from the list', () => {
        const m: Manifestation = MANIFESTATIONS[0];
        expect(isManifestation(m)).toBe(true);
    });
});
