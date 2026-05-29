/**
 * Tim tests for thread navigation — the critical behaviors that define
 * the feature. If any of these break, navigating along a thread is broken.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { navigateThread } from './thread-navigation';
import { addSpine } from './spine-renderer';

// spine-renderer drives a RAF loop on first addSpine; stub for tests
globalThis.requestAnimationFrame = (_cb: FrameRequestCallback) => 0;
globalThis.cancelAnimationFrame = () => {};

function makeSpine(id: string, nodes: string[]) {
    return { id, color: '#c45454', nodes };
}

describe('Tim: ←/→ steps along the active thread', () => {
    const canvasId = 'tim-step';
    let container: HTMLElement;
    let activeSpinePerGlyph: Map<string, string>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        activeSpinePerGlyph = new Map();
        // Spine: A — B — C — 〽
        addSpine(canvasId, container, makeSpine('tim-step-spine', ['a', 'b', 'c', 'thread-marker']));
    });

    test('Tim presses → from a middle glyph and lands on the next', () => {
        const r = navigateThread('right', { canvasId, currentGlyphId: 'b', activeSpinePerGlyph });
        expect(r.handled).toBe(true);
        expect(r.targetGlyphId).toBe('c');
    });

    test('Tim presses ← from a middle glyph and lands on the previous', () => {
        const r = navigateThread('left', { canvasId, currentGlyphId: 'b', activeSpinePerGlyph });
        expect(r.handled).toBe(true);
        expect(r.targetGlyphId).toBe('a');
    });

    test('Tim at the second-to-last glyph presses → — 〽 is skipped, no-op', () => {
        // C is the last REAL glyph (thread-marker is 〽); → would land on 〽 which is skipped
        const r = navigateThread('right', { canvasId, currentGlyphId: 'c', activeSpinePerGlyph });
        expect(r.handled).toBe(true);
        expect(r.targetGlyphId).toBeUndefined();
    });

    test('Tim at the first glyph presses ← — no-op (start of thread)', () => {
        const r = navigateThread('left', { canvasId, currentGlyphId: 'a', activeSpinePerGlyph });
        expect(r.handled).toBe(true);
        expect(r.targetGlyphId).toBeUndefined();
    });
});

describe('Tim: active thread is carried forward', () => {
    const canvasId = 'tim-carry';
    let container: HTMLElement;
    let activeSpinePerGlyph: Map<string, string>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        activeSpinePerGlyph = new Map();
        addSpine(canvasId, container, makeSpine('tim-carry-spine', ['a', 'b', 'c', 'thread-marker']));
    });

    test('Tim → from A lands on B, and B inherits the same active spine', () => {
        const r = navigateThread('right', { canvasId, currentGlyphId: 'a', activeSpinePerGlyph });
        expect(r.targetGlyphId).toBe('b');
        expect(activeSpinePerGlyph.get('b')).toBe('tim-carry-spine');
    });
});

describe('Tim: ↑/↓ rotates the active thread on a multi-thread glyph', () => {
    const canvasId = 'tim-rotate';
    let container: HTMLElement;
    let activeSpinePerGlyph: Map<string, string>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        activeSpinePerGlyph = new Map();
        // Two spines both pass through 'pivot'; creation order: T1 then T2.
        addSpine(canvasId, container, makeSpine('T1', ['pivot', 'x', 'thread-marker-1']));
        addSpine(canvasId, container, makeSpine('T2', ['pivot', 'y', 'thread-marker-2']));
    });

    test('Tim presses ↓ on the pivot — active spine rotates to T2', () => {
        const r = navigateThread('down', { canvasId, currentGlyphId: 'pivot', activeSpinePerGlyph });
        expect(r.handled).toBe(true);
        expect(r.activatedSpineId).toBe('T2');
        expect(activeSpinePerGlyph.get('pivot')).toBe('T2');
    });

    test('Tim presses ↓ then → — navigation now follows T2 (lands on y, not x)', () => {
        navigateThread('down', { canvasId, currentGlyphId: 'pivot', activeSpinePerGlyph });
        const r = navigateThread('right', { canvasId, currentGlyphId: 'pivot', activeSpinePerGlyph });
        expect(r.targetGlyphId).toBe('y');
    });

    test('Tim presses ↑ on the pivot — wraps from T1 to T2 (creation-order list)', () => {
        const r = navigateThread('up', { canvasId, currentGlyphId: 'pivot', activeSpinePerGlyph });
        expect(r.activatedSpineId).toBe('T2');
    });
});

describe('Tim: off-thread arrows fall through to spatial', () => {
    const canvasId = 'tim-offthread';
    let container: HTMLElement;
    let activeSpinePerGlyph: Map<string, string>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        activeSpinePerGlyph = new Map();
        addSpine(canvasId, container, makeSpine('tim-off-spine', ['a', 'b', 'thread-marker']));
    });

    test('Tim presses → while a non-thread glyph is selected — handled is false', () => {
        const r = navigateThread('right', { canvasId, currentGlyphId: 'unrelated', activeSpinePerGlyph });
        expect(r.handled).toBe(false);
    });

    test('Tim presses → with nothing selected — handled is false', () => {
        const r = navigateThread('right', { canvasId, currentGlyphId: null, activeSpinePerGlyph });
        expect(r.handled).toBe(false);
    });
});
