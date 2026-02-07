/**
 * Sync State Tracking for Canvas Glyphs
 *
 * Tracks the synchronization state of individual glyphs with the backend.
 * Provides per-glyph state and subscription mechanism for visual components
 * to react to sync state changes.
 */

import { log, SEG } from '../logger';

export type GlyphSyncState =
    | 'unsynced'    // Never sent to backend, or local changes not yet synced
    | 'syncing'     // Request in flight
    | 'synced'      // Confirmed by backend
    | 'failed';     // Sync attempt failed

type SyncStateCallback = (state: GlyphSyncState) => void;

export interface SyncStateManager {
    getState(glyphId: string): GlyphSyncState;
    setState(glyphId: string, state: GlyphSyncState): void;
    subscribe(glyphId: string, callback: SyncStateCallback): () => void;
    clearState(glyphId: string): void;
}

class SyncStateManagerImpl implements SyncStateManager {
    // Map of glyph ID to current sync state
    private states: Map<string, GlyphSyncState> = new Map();

    // Map of glyph ID to set of callbacks
    private callbacks: Map<string, Set<SyncStateCallback>> = new Map();

    getState(glyphId: string): GlyphSyncState {
        return this.states.get(glyphId) || 'unsynced';
    }

    setState(glyphId: string, state: GlyphSyncState): void {
        const oldState = this.states.get(glyphId);

        if (oldState === state) {
            // No change, don't notify
            return;
        }

        this.states.set(glyphId, state);
        log.debug(SEG.GLYPH, `[SyncState] Glyph ${glyphId}: ${oldState || 'unsynced'} → ${state}`);

        // Notify all callbacks for this glyph
        const glyphCallbacks = this.callbacks.get(glyphId);
        if (glyphCallbacks) {
            glyphCallbacks.forEach(callback => {
                try {
                    callback(state);
                } catch (error) {
                    log.error(SEG.GLYPH, `[SyncState] Error in callback for glyph ${glyphId}:`, error);
                }
            });
        }
    }

    subscribe(glyphId: string, callback: SyncStateCallback): () => void {
        let glyphCallbacks = this.callbacks.get(glyphId);
        if (!glyphCallbacks) {
            glyphCallbacks = new Set();
            this.callbacks.set(glyphId, glyphCallbacks);
        }

        glyphCallbacks.add(callback);

        // Immediately call with current state
        const currentState = this.getState(glyphId);
        try {
            callback(currentState);
        } catch (error) {
            log.error(SEG.GLYPH, `[SyncState] Error in initial callback for glyph ${glyphId}:`, error);
        }

        // Return unsubscribe function
        return () => {
            const callbacks = this.callbacks.get(glyphId);
            if (callbacks) {
                callbacks.delete(callback);
                // Clean up empty sets
                if (callbacks.size === 0) {
                    this.callbacks.delete(glyphId);
                }
            }
        };
    }

    clearState(glyphId: string): void {
        this.states.delete(glyphId);
        // Note: Don't clear callbacks - let components unsubscribe explicitly
        log.debug(SEG.GLYPH, `[SyncState] Cleared state for glyph ${glyphId}`);
    }
}

// Singleton instance
export const syncStateManager = new SyncStateManagerImpl();
