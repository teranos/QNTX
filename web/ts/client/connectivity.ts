/**
 * Connectivity Detection System
 *
 * Monitors browser network state, WebSocket connection, and HTTP reachability
 * to determine overall connectivity to QNTX backend.
 *
 * Three states:
 *   online   — browser online, WS connected, node reachable
 *   degraded — browser online, WS connected, node unreachable
 *   offline  — browser offline OR WS disconnected
 *
 * These are about reach, not about whether the node works. A 5xx means it was
 * reached and could not answer; whether it works is /health, read at startup.
 */

import { log, SEG } from '../logger';
import { delayAfterAttempt, ladderStated } from '../reconnect';

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
    // How many times it has asked, and when it started. 0 when connected.
    readonly reachAttempts: number;
    readonly reachingSince: number;
    subscribe(callback: ConnectivityCallback): () => void;
    subscribeAuth(callback: AuthCallback): () => void;
    subscribeFailures(callback: FailureCallback): () => void;
}

/**
 * A DOM that never says whether it is online is not a DOM reporting offline.
 * Checking only that navigator exists let an absent onLine read as false.
 */
export function browserStartsOnline(): boolean {
    if (typeof navigator === 'undefined') {
        return true;
    }
    return typeof navigator.onLine === 'boolean' ? navigator.onLine : true;
}

export class ConnectivityManagerImpl implements ConnectivityManager {
    private _backendUrl: () => string;
    private _state: ConnectivityState = 'online';
    // Nobody until the node names them. Starting at true meant every tab began
    // by claiming an identity it had not asked about.
    private _authenticated: boolean = false;
    private callbacks: Set<ConnectivityCallback> = new Set();
    private authCallbacks: Set<AuthCallback> = new Set();
    private failureCallbacks: Set<FailureCallback> = new Set();
    private _failures: Failure[] = [];
    private readonly FAILURE_RING_SIZE = 5;
    private debounceTimer: number | null = null;
    private pendingState: ConnectivityState | null = null;

    // Track browser, WebSocket, and HTTP state
    private browserOnline: boolean = browserStartsOnline();
    private wsConnected: boolean = false;
    private httpHealthy: boolean = true;
    private consecutiveHttpFailures: number = 0;
    private recoveryTimer: number | null = null;

    // Counted, not estimated: how many times it has asked and when it started.
    private attempts: number = 0;
    private tryingSince: number = 0;

    // Thresholds
    private readonly DEBOUNCE_MS = 300;
    private readonly FAILURE_THRESHOLD = 3;

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

    get reachAttempts(): number {
        return this.attempts;
    }

    get reachingSince(): number {
        return this.tryingSince;
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
                this.probeIdentity();
            }
        });

        // Nobody until asked, so ask — otherwise a signed-in tab would sit as
        // nobody until it happened to be hidden and shown again.
        this.probeIdentity();

        // Initial state based on browser
        this.updateState();
    }

    /**
     * Ask the node who this is. /auth/status names the identity, so that name is
     * the answer — a status that is merely not 401 answers a different question.
     */
    private probeIdentity(): void {
        fetch(this._backendUrl() + '/auth/status', { credentials: 'include' })
            .then(res => res.ok ? res.json() : null)
            .then(said => {
                if (said && said.identity) this.reportAuthenticated();
            })
            .catch(() => { /* unreachable; nobody is still the answer */ });
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
     * Called by apiFetch when a response arrived, whatever its status. That the
     * node answered is the fact; that it answered well is a different question.
     */
    reportReachable(): void {
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
     * Ask /health on the backoff ladder while unreachable. A fixed interval
     * asked a dead node the same question forever; this slows down and keeps
     * count, and the count is what the chip states.
     */
    private startRecoveryTimer(): void {
        if (this.recoveryTimer !== null) return; // already running

        if (this.tryingSince === 0) {
            this.tryingSince = Date.now();
            this.attempts = 0;
        }
        log.debug(SEG.UI, `[Connectivity] Retrying on the ladder: ${ladderStated()}`);
        this.scheduleNextAttempt();
    }

    private scheduleNextAttempt(): void {
        const wait = delayAfterAttempt(this.attempts + 1);
        this.recoveryTimer = window.setTimeout(() => {
            this.recoveryTimer = null;
            this.attempts++;

            // fetch only rejects on a network error, so a 503 resolves. /health
            // answers 503 when the operational store is unreadable, and taking
            // that as recovery is how a down node reports itself back online.
            fetch(this._backendUrl() + '/health').then(
                response => {
                    if (response.ok) {
                        this.reportReachable();
                        return;
                    }
                    this.scheduleNextAttempt();
                },
                () => { this.scheduleNextAttempt(); }
            );
        }, wait);
    }

    private stopRecoveryTimer(): void {
        if (this.recoveryTimer !== null) {
            clearTimeout(this.recoveryTimer);
            this.recoveryTimer = null;
        }
        this.attempts = 0;
        this.tryingSince = 0;
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
