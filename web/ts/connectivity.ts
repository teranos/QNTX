/**
 * Connectivity Detection System
 *
 * Monitors both browser network state and WebSocket connection to determine
 * overall connectivity to QNTX backend. Provides debounced state changes to
 * prevent UI flapping during unstable connections.
 */

import { log, SEG } from './logger';

export type ConnectivityState = 'online' | 'offline';

type ConnectivityCallback = (state: ConnectivityState) => void;

export interface ConnectivityManager {
    readonly state: ConnectivityState;
    subscribe(callback: ConnectivityCallback): () => void;
}

class ConnectivityManagerImpl implements ConnectivityManager {
    private _state: ConnectivityState = 'online';
    private callbacks: Set<ConnectivityCallback> = new Set();
    private debounceTimer: number | null = null;
    private pendingState: ConnectivityState | null = null;

    // Track both browser and WebSocket state
    private browserOnline: boolean = navigator.onLine;
    private wsConnected: boolean = false;

    // Debounce duration in milliseconds (equal for both directions)
    private readonly DEBOUNCE_MS = 300;

    constructor() {
        this.init();
    }

    get state(): ConnectivityState {
        return this._state;
    }

    private init(): void {
        // Monitor browser online/offline events
        window.addEventListener('online', () => {
            log.debug(SEG.UI, '[Connectivity] Browser reports online');
            this.browserOnline = true;
            this.updateState();
        });

        window.addEventListener('offline', () => {
            log.debug(SEG.UI, '[Connectivity] Browser reports offline');
            this.browserOnline = false;
            this.updateState();
        });

        // Initial state based on browser
        this.updateState();
    }

    /**
     * Called by WebSocket manager to report connection state
     */
    setWebSocketConnected(connected: boolean): void {
        if (this.wsConnected !== connected) {
            log.debug(SEG.UI, `[Connectivity] WebSocket ${connected ? 'connected' : 'disconnected'}`);
            this.wsConnected = connected;
            this.updateState();
        }
    }

    private updateState(): void {
        // Offline if EITHER browser OR WebSocket reports offline
        // (navigator.onLine can report false positives, WebSocket is ground truth)
        const newState: ConnectivityState = (this.browserOnline && this.wsConnected) ? 'online' : 'offline';

        if (newState === this._state) {
            // No change, cancel any pending transition
            if (this.debounceTimer !== null) {
                clearTimeout(this.debounceTimer);
                this.debounceTimer = null;
                this.pendingState = null;
            }
            return;
        }

        // State change detected - debounce it
        this.pendingState = newState;

        if (this.debounceTimer !== null) {
            clearTimeout(this.debounceTimer);
        }

        this.debounceTimer = window.setTimeout(() => {
            if (this.pendingState !== null && this.pendingState !== this._state) {
                const oldState = this._state;
                this._state = this.pendingState;
                log.info(SEG.UI, `[Connectivity] State changed: ${oldState} → ${this._state}`);
                this.notifyCallbacks();
            }
            this.debounceTimer = null;
            this.pendingState = null;
        }, this.DEBOUNCE_MS);
    }

    subscribe(callback: ConnectivityCallback): () => void {
        this.callbacks.add(callback);

        // Immediately call with current state
        callback(this._state);

        // Return unsubscribe function
        return () => {
            this.callbacks.delete(callback);
        };
    }

    private notifyCallbacks(): void {
        this.callbacks.forEach(callback => {
            try {
                callback(this._state);
            } catch (error) {
                log.error(SEG.UI, '[Connectivity] Error in callback:', error);
            }
        });
    }
}

// Singleton instance
export const connectivityManager = new ConnectivityManagerImpl();
