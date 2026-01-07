// Tests for focus mode state management
// Focus on stable, canonical state transitions - not physics or dimensions which are still evolving

import { describe, it, expect, beforeEach } from 'bun:test';
import { isFocused, getFocusedId } from './focus.ts';
import { setFocusedNodeId, getFocusedNodeId, setPreFocusTransform, getPreFocusTransform, clearState } from './state.ts';

describe('Focus State Management', () => {
    beforeEach(() => {
        // Clear state before each test
        clearState();
    });

    describe('isFocused()', () => {
        it('should return false when no node is focused', () => {
            expect(isFocused()).toBe(false);
        });

        it('should return true when a node is focused', () => {
            setFocusedNodeId('test-node-123');
            expect(isFocused()).toBe(true);
        });

        it('should return false after unfocus (focused ID cleared)', () => {
            setFocusedNodeId('test-node-123');
            expect(isFocused()).toBe(true);

            setFocusedNodeId(null);
            expect(isFocused()).toBe(false);
        });
    });

    describe('getFocusedId()', () => {
        it('should return null when no node is focused', () => {
            expect(getFocusedId()).toBeNull();
        });

        it('should return the focused node ID when set', () => {
            setFocusedNodeId('test-node-456');
            expect(getFocusedId()).toBe('test-node-456');
        });

        it('should return null after clearing focused node', () => {
            setFocusedNodeId('test-node-456');
            setFocusedNodeId(null);
            expect(getFocusedId()).toBeNull();
        });
    });

    describe('Focus State Transitions', () => {
        it('should handle initial focus → focused state', () => {
            // Start unfocused
            expect(isFocused()).toBe(false);
            expect(getFocusedId()).toBeNull();

            // Focus on a node
            setFocusedNodeId('node-1');

            // Verify focused state
            expect(isFocused()).toBe(true);
            expect(getFocusedId()).toBe('node-1');
        });

        it('should handle tile-to-tile transition (focused → different focused)', () => {
            // Focus on first node
            setFocusedNodeId('node-1');
            expect(getFocusedId()).toBe('node-1');

            // Transition to second node
            setFocusedNodeId('node-2');

            // Verify new focused state
            expect(isFocused()).toBe(true);
            expect(getFocusedId()).toBe('node-2');
        });

        it('should handle unfocus transition (focused → unfocused)', () => {
            // Focus on a node
            setFocusedNodeId('node-1');
            expect(isFocused()).toBe(true);

            // Unfocus
            setFocusedNodeId(null);

            // Verify unfocused state
            expect(isFocused()).toBe(false);
            expect(getFocusedId()).toBeNull();
        });
    });

    describe('Pre-Focus Transform Preservation', () => {
        it('should save and restore pre-focus transform', () => {
            const transform = { x: 100, y: 200, k: 1.5 };

            // Save transform before focusing
            setPreFocusTransform(transform);

            // Verify it's saved
            expect(getPreFocusTransform()).toEqual(transform);

            // Clear on unfocus
            setPreFocusTransform(null);
            expect(getPreFocusTransform()).toBeNull();
        });

        it('should not overwrite pre-focus transform during tile-to-tile transition', () => {
            const originalTransform = { x: 100, y: 200, k: 1.5 };

            // Initial focus saves transform
            setPreFocusTransform(originalTransform);
            setFocusedNodeId('node-1');

            // Tile-to-tile transition should NOT overwrite
            const currentTransform = getPreFocusTransform();
            setFocusedNodeId('node-2');

            // Pre-focus transform should still be original
            expect(getPreFocusTransform()).toEqual(originalTransform);
        });

        it('should clear pre-focus transform on unfocus', () => {
            const transform = { x: 100, y: 200, k: 1.5 };

            setPreFocusTransform(transform);
            setFocusedNodeId('node-1');

            // Unfocus clears everything
            setFocusedNodeId(null);
            setPreFocusTransform(null);

            expect(getFocusedId()).toBeNull();
            expect(getPreFocusTransform()).toBeNull();
        });
    });

    describe('Focus State Invariants', () => {
        it('should maintain state consistency through multiple transitions', () => {
            // Initial → Focus
            setFocusedNodeId('node-1');
            expect(isFocused()).toBe(true);
            expect(getFocusedId()).toBe('node-1');

            // Focus → Transition
            setFocusedNodeId('node-2');
            expect(isFocused()).toBe(true);
            expect(getFocusedId()).toBe('node-2');

            // Transition → Another Transition
            setFocusedNodeId('node-3');
            expect(isFocused()).toBe(true);
            expect(getFocusedId()).toBe('node-3');

            // Final → Unfocus
            setFocusedNodeId(null);
            expect(isFocused()).toBe(false);
            expect(getFocusedId()).toBeNull();
        });

        it('should handle rapid focus/unfocus cycles', () => {
            for (let i = 0; i < 5; i++) {
                // Focus
                setFocusedNodeId(`node-${i}`);
                expect(isFocused()).toBe(true);
                expect(getFocusedId()).toBe(`node-${i}`);

                // Unfocus
                setFocusedNodeId(null);
                expect(isFocused()).toBe(false);
                expect(getFocusedId()).toBeNull();
            }
        });
    });
});
