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

import { log, SEG } from '../logger';

export type ConnectivityState = 'online' | 'degraded' | 'offline';

export type FailureSource = 'http' | 'ws';

export interface Failure {
    source: FailureSource;
    url: string;
    reason: string;
    at: number;
}

type ConnectivityCallback = (state: ConnectivityState) => void;
type AuthCallback = (authenticated: boolean) => void;
type FailureCallback = (failure: Failure) => void;

export interface ConnectivityManager {
    readonly state: ConnectivityState;
    readonly authenticated: boolean;
    readonly lastFailure: Failure | null;
    readonly failures: readonly Failure[];
    subscribe(callback: ConnectivityCallback): () => void;
    subscribeAuth(callback: AuthCallback): () => void;
    subscribeFailures(callback: FailureCallback): () => void;
}

export class ConnectivityManagerImpl implements ConnectivityManager {
    private _backendUrl: () => string;
    private _state: ConnectivityState = 'online';
    private _authenticated: boolean = true; // assume authenticated until told otherwise
    private callbacks: Set<ConnectivityCallback> = new Set();
    private authCallbacks: Set<AuthCallback> = new Set();
    private failureCallbacks: Set<FailureCallback> = new Set();
    private _failures: Failure[] = [];
    private readonly FAILURE_RING_SIZE = 5;
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

    constructor(backendUrl: () => string) {
        this._backendUrl = backendUrl;
        this.init();
    }

    get state(): ConnectivityState {
        return this._state;
    }

    get authenticated(): boolean {
        return this._authenticated;
    }

    get lastFailure(): Failure | null {
        return this._failures.length > 0 ? this._failures[this._failures.length - 1] : null;
    }

    get failures(): readonly Failure[] {
        return this._failures;
    }

    private recordFailure(failure: Failure): void {
        this._failures.push(failure);
        if (this._failures.length > this.FAILURE_RING_SIZE) {
            this._failures.shift();
        }
        this.failureCallbacks.forEach(cb => {
            try { cb(failure); } catch (e) { log.error(SEG.UI, '[Connectivity] Failure callback error:', e); }
        });
    }

    subscribeFailures(callback: FailureCallback): () => void {
        this.failureCallbacks.add(callback);
        return () => { this.failureCallbacks.delete(callback); };
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

        // When the tab becomes visible and we're unauthenticated,
        // probe the backend — the user may have authenticated in another tab.
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible' && !this._authenticated) {
                fetch(this._backendUrl() + '/auth/status', { credentials: 'include' }).then(res => {
                    if (res.status !== 401) {
                        this.reportAuthenticated();
                    }
                }).catch(() => { /* server unreachable, ignore */ });
            }
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
     * Called when backend returns 401 — node requires authentication.
     * Does NOT redirect. WASM keeps running. UI surfaces a login prompt.
     */
    reportUnauthenticated(): void {
        if (this._authenticated) {
            this._authenticated = false;
            log.info(SEG.UI, '[Connectivity] Backend requires authentication');
            this.authCallbacks.forEach(cb => {
                try { cb(false); } catch (e) { log.error(SEG.UI, '[Connectivity] Auth callback error:', e); }
            });
        }
    }

    /**
     * Called after successful authentication to restore full connectivity.
     */
    reportAuthenticated(): void {
        if (!this._authenticated) {
            this._authenticated = true;
            log.info(SEG.UI, '[Connectivity] Authenticated');
            this.authCallbacks.forEach(cb => {
                try { cb(true); } catch (e) { log.error(SEG.UI, '[Connectivity] Auth callback error:', e); }
            });
        }
    }

    /**
     * Subscribe to authentication state changes.
     */
    subscribeAuth(callback: AuthCallback): () => void {
        this.authCallbacks.add(callback);
        callback(this._authenticated);
        return () => { this.authCallbacks.delete(callback); };
    }

    /**
     * Called by apiFetch on network-level failure (fetch TypeError).
     * url = full backend URL. error = the thrown value.
     */
    reportHttpFailure(url: string, error: unknown): void {
        const reason = error instanceof Error
            ? `${error.name}: ${error.message}`
            : String(error);
        this.recordFailure({ source: 'http', url, reason, at: Date.now() });
        this.consecutiveHttpFailures++;
        if (this.consecutiveHttpFailures >= this.FAILURE_THRESHOLD && this.httpHealthy) {
            this.httpHealthy = false;
            log.warn(SEG.UI, `[Connectivity] HTTP unreachable after ${this.consecutiveHttpFailures} consecutive failures`);
            this.updateState();
        }
    }

    /**
     * Called by the WebSocket manager on connect error or abnormal close.
     * url = ws[s]:// URL. reason = human-readable string ("connection error", "1006 (no reason)", etc.).
     */
    reportWsFailure(url: string, reason: string): void {
        this.recordFailure({ source: 'ws', url, reason, at: Date.now() });
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
            fetch(this._backendUrl() + '/health').then(
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

// Singleton — created here (leaf module) to avoid circular dependency via client.ts
import { backendUrl } from './url';
export const connectivity = new ConnectivityManagerImpl(backendUrl);
