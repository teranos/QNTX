import { describe, test, expect, beforeEach } from 'bun:test';
import { syncStateManager, type ElementSyncState } from './sync-state';

describe('SyncStateManager', () => {
    const testElementId = 'test-element-123';

    beforeEach(() => {
        // Clear state before each test
        syncStateManager.clearState(testElementId);
    });

    test('tracks element sync state transitions', () => {
        // Track all state changes
        const stateChanges: ElementSyncState[] = [];

        syncStateManager.subscribe(testElementId, (state) => {
            stateChanges.push(state);
        });

        // Verify initial state is 'unsynced'
        expect(stateChanges[0]).toBe('unsynced');

        // Simulate API call lifecycle: unsynced → syncing → synced
        syncStateManager.setState(testElementId, 'syncing');
        syncStateManager.setState(testElementId, 'synced');

        // Verify all transitions were tracked
        expect(stateChanges).toEqual([
            'unsynced',  // Initial state from subscription
            'syncing',   // Before API call
            'synced'     // After successful API response
        ]);

        // Verify final state
        expect(syncStateManager.getState(testElementId)).toBe('synced');
    });

    test('handles sync failure correctly', () => {
        const stateChanges: ElementSyncState[] = [];

        syncStateManager.subscribe(testElementId, (state) => {
            stateChanges.push(state);
        });

        // Simulate failed sync: unsynced → syncing → failed
        syncStateManager.setState(testElementId, 'syncing');
        syncStateManager.setState(testElementId, 'failed');

        expect(stateChanges).toEqual([
            'unsynced',
            'syncing',
            'failed'
        ]);

        expect(syncStateManager.getState(testElementId)).toBe('failed');
    });

    test('multiple subscribers receive state updates', () => {
        const subscriber1States: ElementSyncState[] = [];
        const subscriber2States: ElementSyncState[] = [];

        syncStateManager.subscribe(testElementId, (state) => {
            subscriber1States.push(state);
        });

        syncStateManager.subscribe(testElementId, (state) => {
            subscriber2States.push(state);
        });

        syncStateManager.setState(testElementId, 'syncing');

        // Both subscribers should receive the update
        expect(subscriber1States).toContain('syncing');
        expect(subscriber2States).toContain('syncing');
    });

    test('unsubscribe prevents future updates', () => {
        const stateChanges: ElementSyncState[] = [];

        const unsubscribe = syncStateManager.subscribe(testElementId, (state) => {
            stateChanges.push(state);
        });

        syncStateManager.setState(testElementId, 'syncing');
        expect(stateChanges).toContain('syncing');

        // Unsubscribe and make another change
        unsubscribe();
        syncStateManager.setState(testElementId, 'synced');

        // Should not receive 'synced' update after unsubscribing
        expect(stateChanges).not.toContain('synced');
        expect(stateChanges.length).toBe(2); // Only 'unsynced' (initial) and 'syncing'
    });
});
