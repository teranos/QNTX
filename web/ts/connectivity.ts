/**
 * Connectivity Detection System
 *
 * Monitors browser network state, WebSocket connection, and HTTP reachability
 * to determine overall connectivity to QNTX backend.
 *
 * Three states:
 *   online   — browser online, WS connected, HTTP healthy
 *   degraded — browser online, WS connected, HTTP unreachable (network-level failures)
 *   offline  — browser offline OR WS disconnected
 *
 * Only network-level fetch failures (TypeError) count toward degraded.
 * A 4xx/5xx response means the server responded — HTTP is healthy.
 */

import { log, SEG } from './logger';

export type ConnectivityState = 'online' | 'degraded' | 'offline';

type ConnectivityCallback = (state: ConnectivityState) => void;

export interface ConnectivityManager {
    readonly state: ConnectivityState;
    subscribe(callback: ConnectivityCallback): () => void;
}

/** Resolve backend base URL without importing api.ts (avoids import cycle) */
function getBackendUrl(): string {
    return (typeof window !== 'undefined' && (window as any).__BACKEND_URL__) || (typeof window !== 'undefined' ? window.location.origin : '');
}

class ConnectivityManagerImpl implements ConnectivityManager {
    private _state: ConnectivityState = 'online';
    private callbacks: Set<ConnectivityCallback> = new Set();
    private debounceTimer: number | null = null;
    private pendingState: ConnectivityState | null = null;

    // Track browser, WebSocket, and HTTP state
    private browserOnline: boolean = typeof navigator !== 'undefined' ? navigator.onLine : true;
    private wsConnected: boolean = false;
    private httpHealthy: boolean = true;
    private consecutiveHttpFailures: number = 0;
    private recoveryTimer: number | null = null;

    // Thresholds
    private readonly DEBOUNCE_MS = 300;
    private readonly FAILURE_THRESHOLD = 3;
    private readonly RECOVERY_INTERVAL_MS = 15_000;

    constructor() {
        this.init();
    }

    get state(): ConnectivityState {
        return this._state;
    }

    private init(): void {
        // Guard against non-browser environments (e.g., test runners)
        if (typeof window === 'undefined') {
            return;
        }

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
            // Fresh connection → reset HTTP health (stale failures from before disconnect)
            if (connected) {
                this.consecutiveHttpFailures = 0;
                this.httpHealthy = true;
            }
            this.updateState();
        }
    }

    /**
     * Called by apiFetch on successful response (any HTTP status)
     */
    reportHttpSuccess(): void {
        this.consecutiveHttpFailures = 0;
        if (!this.httpHealthy) {
            this.httpHealthy = true;
            log.info(SEG.UI, '[Connectivity] HTTP recovered');
            this.updateState();
        }
    }

    /**
     * Called by apiFetch on network-level failure (fetch TypeError)
     */
    reportHttpFailure(): void {
        this.consecutiveHttpFailures++;
        if (this.consecutiveHttpFailures >= this.FAILURE_THRESHOLD && this.httpHealthy) {
            this.httpHealthy = false;
            log.warn(SEG.UI, `[Connectivity] HTTP unreachable after ${this.consecutiveHttpFailures} consecutive failures`);
            this.updateState();
        }
    }

    private updateState(): void {
        let newState: ConnectivityState;
        if (!this.browserOnline || !this.wsConnected) {
            newState = 'offline';
        } else if (!this.httpHealthy) {
            newState = 'degraded';
        } else {
            newState = 'online';
        }

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

                // Manage recovery timer only when state is committed
                if (this._state === 'degraded') {
                    this.startRecoveryTimer();
                } else {
                    this.stopRecoveryTimer();
                }

                this.notifyCallbacks();
            }
            this.debounceTimer = null;
            this.pendingState = null;
        }, this.DEBOUNCE_MS);
    }

    /**
     * Ping /health every 15s while degraded to detect HTTP recovery
     */
    private startRecoveryTimer(): void {
        if (this.recoveryTimer !== null) return; // already running

        log.debug(SEG.UI, '[Connectivity] Starting HTTP recovery timer');
        this.recoveryTimer = window.setInterval(() => {
            fetch(getBackendUrl() + '/health').then(
                () => { this.reportHttpSuccess(); },
                () => { /* still unreachable, stay degraded */ }
            );
        }, this.RECOVERY_INTERVAL_MS);
    }

    private stopRecoveryTimer(): void {
        if (this.recoveryTimer !== null) {
            clearInterval(this.recoveryTimer);
            this.recoveryTimer = null;
        }
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
