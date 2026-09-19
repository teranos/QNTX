/**
 * Sync State Tracking for Canvas Elements
 *
 * Tracks the synchronization state of individual elements with the backend.
 * Provides per-element state and subscription mechanism for visual components
 * to react to sync state changes.
 */

import { log, SEG } from '../logger';

export type ElementSyncState =
    | 'unsynced'    // Never sent to backend, or local changes not yet synced
    | 'syncing'     // Request in flight
    | 'synced'      // Confirmed by backend
    | 'failed';     // Sync attempt failed

type SyncStateCallback = (state: ElementSyncState) => void;

export interface SyncStateManager {
    getState(elementId: string): ElementSyncState;
    setState(elementId: string, state: ElementSyncState): void;
    subscribe(elementId: string, callback: SyncStateCallback): () => void;
    clearState(elementId: string): void;
}

class SyncStateManagerImpl implements SyncStateManager {
    // Map of element ID to current sync state
    private states: Map<string, ElementSyncState> = new Map();

    // Map of element ID to set of callbacks
    private callbacks: Map<string, Set<SyncStateCallback>> = new Map();

    getState(elementId: string): ElementSyncState {
        return this.states.get(elementId) || 'unsynced';
    }

    setState(elementId: string, state: ElementSyncState): void {
        const oldState = this.states.get(elementId);

        if (oldState === state) {
            // No change, don't notify
            return;
        }

        this.states.set(elementId, state);
        log.debug(SEG.ELEMENT, `[SyncState] Element ${elementId}: ${oldState || 'unsynced'} → ${state}`);

        // Notify all callbacks for this element
        const elementCallbacks = this.callbacks.get(elementId);
        if (elementCallbacks) {
            elementCallbacks.forEach(callback => {
                try {
                    callback(state);
                } catch (error) {
                    log.error(SEG.ELEMENT, `[SyncState] Error in callback for element ${elementId}:`, error);
                }
            });
        }
    }

    subscribe(elementId: string, callback: SyncStateCallback): () => void {
        let elementCallbacks = this.callbacks.get(elementId);
        if (!elementCallbacks) {
            elementCallbacks = new Set();
            this.callbacks.set(elementId, elementCallbacks);
        }

        elementCallbacks.add(callback);

        // Immediately call with current state
        const currentState = this.getState(elementId);
        try {
            callback(currentState);
        } catch (error) {
            log.error(SEG.ELEMENT, `[SyncState] Error in initial callback for element ${elementId}:`, error);
        }

        // Return unsubscribe function
        return () => {
            const callbacks = this.callbacks.get(elementId);
            if (callbacks) {
                callbacks.delete(callback);
                // Clean up empty sets
                if (callbacks.size === 0) {
                    this.callbacks.delete(elementId);
                }
            }
        };
    }

    clearState(elementId: string): void {
        this.states.delete(elementId);
        // Note: Don't clear callbacks - let components unsubscribe explicitly
        log.debug(SEG.ELEMENT, `[SyncState] Cleared state for element ${elementId}`);
    }
}

// Singleton instance
export const syncStateManager = new SyncStateManagerImpl();
