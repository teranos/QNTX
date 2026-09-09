/**
 * What manifestation an element records it is in.
 *
 * Personas:
 * - Tim: the element carries the name
 * - Spike: one at a time, and a fresh element carries none
 * - Jenny: the boolean this replaces, and what it could and could not say
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import {
    setManifestation,
    getManifestation,
    isInWindowState,
    setWindowState,
} from './dataset';
import { MANIFESTATIONS, type Manifestation } from './manifestation';

let el: HTMLElement;

beforeEach(() => {
    el = document.createElement('div');
});

const names = Object.keys(MANIFESTATIONS) as Manifestation[];

describe('Tim: the element carries the name', () => {
    test('every manifestation in the table can be recorded and read back', () => {
        for (const name of names) {
            setManifestation(el, name);
            expect(getManifestation(el)).toBe(name);
        }
    });

    test('the name is what a stylesheet can select on', () => {
        setManifestation(el, 'panel');
        expect(el.getAttribute('data-manifestation')).toBe('panel');
    });
});

describe('Spike: one at a time', () => {
    // Element Axioma: one element for the glyph's whole lifetime. It is in one
    // manifestation at a time, so recording a second replaces the first rather
    // than adding to it.
    test('recording a manifestation replaces the one before it', () => {
        setManifestation(el, 'window');
        setManifestation(el, 'canvasPlaced');
        expect(getManifestation(el)).toBe('canvasPlaced');
    });

    test('an element that has never been told is in none', () => {
        expect(getManifestation(el)).toBeNull();
    });

    test('a name that is not in the table reads as none, not as itself', () => {
        el.setAttribute('data-manifestation', 'fullscreen');
        expect(getManifestation(el)).toBeNull();
    });
});

describe('Jenny: the boolean this replaces', () => {
    // setWindowState/isInWindowState read and write the same store, so a
    // consumer still on them sees what the ported code writes, and the reverse.
    test('the old setter records the window manifestation', () => {
        setWindowState(el, true);
        expect(getManifestation(el)).toBe('window');
    });

    test('the old getter is true for window', () => {
        setManifestation(el, 'window');
        expect(isInWindowState(el)).toBe(true);
    });

    // The boolean was set true by canvas-expanded.ts for a glyph filling the
    // viewport, which is not a window. That is what it meant; the shim keeps
    // meaning it.
    test('the old getter is true for canvasExpanded, as it always was', () => {
        setManifestation(el, 'canvasExpanded');
        expect(isInWindowState(el)).toBe(true);
    });

    // And what it could never say: panel and workspace were set false, the same
    // answer it gave for a dot resting in the tray.
    test('the old getter cannot tell a panel from a dot', () => {
        setManifestation(el, 'panel');
        expect(isInWindowState(el)).toBe(false);
        setManifestation(el, 'dot');
        expect(isInWindowState(el)).toBe(false);
    });

    test('the old getter cannot tell the workspace from a dot', () => {
        setManifestation(el, 'workspace');
        expect(isInWindowState(el)).toBe(false);
    });

    test('the old clear leaves the element in no manifestation', () => {
        setManifestation(el, 'window');
        setWindowState(el, false);
        expect(getManifestation(el)).toBeNull();
        expect(isInWindowState(el)).toBe(false);
    });
});
