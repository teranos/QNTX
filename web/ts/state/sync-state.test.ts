import { describe, test, expect, beforeEach } from 'bun:test';
import { syncStateManager, type GlyphSyncState } from './sync-state';

describe('SyncStateManager', () => {
    const testGlyphId = 'test-glyph-123';

    beforeEach(() => {
        // Clear state before each test
        syncStateManager.clearState(testGlyphId);
    });

    test('tracks glyph sync state transitions', () => {
        // Track all state changes
        const stateChanges: GlyphSyncState[] = [];

        syncStateManager.subscribe(testGlyphId, (state) => {
            stateChanges.push(state);
        });

        // Verify initial state is 'unsynced'
        expect(stateChanges[0]).toBe('unsynced');

        // Simulate API call lifecycle: unsynced → syncing → synced
        syncStateManager.setState(testGlyphId, 'syncing');
        syncStateManager.setState(testGlyphId, 'synced');

        // Verify all transitions were tracked
        expect(stateChanges).toEqual([
            'unsynced',  // Initial state from subscription
            'syncing',   // Before API call
            'synced'     // After successful API response
        ]);

        // Verify final state
        expect(syncStateManager.getState(testGlyphId)).toBe('synced');
    });

    test('handles sync failure correctly', () => {
        const stateChanges: GlyphSyncState[] = [];

        syncStateManager.subscribe(testGlyphId, (state) => {
            stateChanges.push(state);
        });

        // Simulate failed sync: unsynced → syncing → failed
        syncStateManager.setState(testGlyphId, 'syncing');
        syncStateManager.setState(testGlyphId, 'failed');

        expect(stateChanges).toEqual([
            'unsynced',
            'syncing',
            'failed'
        ]);

        expect(syncStateManager.getState(testGlyphId)).toBe('failed');
    });

    test('multiple subscribers receive state updates', () => {
        const subscriber1States: GlyphSyncState[] = [];
        const subscriber2States: GlyphSyncState[] = [];

        syncStateManager.subscribe(testGlyphId, (state) => {
            subscriber1States.push(state);
        });

        syncStateManager.subscribe(testGlyphId, (state) => {
            subscriber2States.push(state);
        });

        syncStateManager.setState(testGlyphId, 'syncing');

        // Both subscribers should receive the update
        expect(subscriber1States).toContain('syncing');
        expect(subscriber2States).toContain('syncing');
    });

    test('unsubscribe prevents future updates', () => {
        const stateChanges: GlyphSyncState[] = [];

        const unsubscribe = syncStateManager.subscribe(testGlyphId, (state) => {
            stateChanges.push(state);
        });

        syncStateManager.setState(testGlyphId, 'syncing');
        expect(stateChanges).toContain('syncing');

        // Unsubscribe and make another change
        unsubscribe();
        syncStateManager.setState(testGlyphId, 'synced');

        // Should not receive 'synced' update after unsubscribing
        expect(stateChanges).not.toContain('synced');
        expect(stateChanges.length).toBe(2); // Only 'unsynced' (initial) and 'syncing'
    });
});
