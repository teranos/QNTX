/**
 * Tests for ConnectivityManagerImpl state machine.
 *
 * Tests the class directly (not the singleton) so each test gets a fresh instance.
 * The 300ms debounce means state transitions are async — tests wait for settlement.
 */

import { describe, test, expect, mock } from 'bun:test';
import { ConnectivityManagerImpl } from './connectivity';

const DEBOUNCE_WAIT = 350; // > 300ms debounce

/** Create a fresh ConnectivityManagerImpl and stabilize it at 'online' */
function createOnline(): ConnectivityManagerImpl {
    const cm = new ConnectivityManagerImpl(() => 'http://localhost');
    // Constructor starts with wsConnected=false → updateState computes 'offline'.
    // Set WS connected immediately to cancel the pending 'offline' debounce.
    cm.setWebSocketConnected(true);
    return cm;
}

// ── Tim: State transitions online → offline → online ──

describe('Tim: state transitions', () => {
    test('starts online when WS is connected', () => {
        const cm = createOnline();
        expect(cm.state).toBe('online');
    });

    test('transitions to offline when WS disconnects', async () => {
        const cm = createOnline();
        cm.setWebSocketConnected(false);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('offline');
    });

    test('transitions back to online when WS reconnects', async () => {
        const cm = createOnline();

        // Go offline
        cm.setWebSocketConnected(false);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('offline');

        // Come back online
        cm.setWebSocketConnected(true);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('online');
    });

    test('notifies subscriber on state change', async () => {
        const cm = createOnline();
        const states: string[] = [];
        cm.subscribe(s => states.push(s));

        // subscribe fires immediately with current state
        expect(states).toEqual(['online']);

        cm.setWebSocketConnected(false);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(states).toEqual(['online', 'offline']);
    });
});

// ── Spike: State transition edge cases ──

describe('Spike: state transition edge cases', () => {
    test('rapid WS disconnect/reconnect within debounce cancels transition', async () => {
        const cm = createOnline();
        const states: string[] = [];
        cm.subscribe(s => states.push(s));

        // Disconnect then immediately reconnect (within 300ms debounce)
        cm.setWebSocketConnected(false);
        cm.setWebSocketConnected(true);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));

        // Should never have left 'online'
        expect(states).toEqual(['online']);
        expect(cm.state).toBe('online');
    });

    test('unsubscribe stops notifications', async () => {
        const cm = createOnline();
        const states: string[] = [];
        const unsub = cm.subscribe(s => states.push(s));

        unsub();

        cm.setWebSocketConnected(false);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));

        // Only the initial 'online' from subscribe, no 'offline'
        expect(states).toEqual(['online']);
    });
});

// ── Tim: HTTP recovery resets counter ──

describe('Tim: HTTP recovery', () => {
    test('reportHttpSuccess resets failure counter and restores online from degraded', async () => {
        const cm = createOnline();

        // Push into degraded: 3 consecutive HTTP failures
        cm.reportHttpFailure();
        cm.reportHttpFailure();
        cm.reportHttpFailure();
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('degraded');

        // Single success recovers
        cm.reportHttpSuccess();
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('online');
    });
});

// ── Spike: HTTP recovery edge cases ──

describe('Spike: HTTP recovery edge cases', () => {
    test('success mid-count resets counter — 2 failures, 1 success, 2 failures stays online', async () => {
        const cm = createOnline();

        cm.reportHttpFailure();
        cm.reportHttpFailure();
        // Success resets counter
        cm.reportHttpSuccess();
        cm.reportHttpFailure();
        cm.reportHttpFailure();

        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        // Never hit threshold of 3 consecutive
        expect(cm.state).toBe('online');
    });

    test('WS reconnect resets HTTP health — degraded before disconnect, online after reconnect', async () => {
        const cm = createOnline();

        // Go degraded
        cm.reportHttpFailure();
        cm.reportHttpFailure();
        cm.reportHttpFailure();
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('degraded');

        // WS disconnects → offline
        cm.setWebSocketConnected(false);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('offline');

        // WS reconnects → resets HTTP health → online (not degraded)
        cm.setWebSocketConnected(true);
        await new Promise(r => setTimeout(r, DEBOUNCE_WAIT));
        expect(cm.state).toBe('online');
    });
});

// ── Tim: Auth state ──

describe('Tim: auth state', () => {
    test('starts authenticated', () => {
        const cm = createOnline();
        expect(cm.authenticated).toBe(true);
    });

    test('reportUnauthenticated sets authenticated to false and notifies', () => {
        const cm = createOnline();
        const authStates: boolean[] = [];
        cm.subscribeAuth(a => authStates.push(a));

        // subscribeAuth fires immediately
        expect(authStates).toEqual([true]);

        cm.reportUnauthenticated();
        expect(cm.authenticated).toBe(false);
        expect(authStates).toEqual([true, false]);
    });

    test('reportAuthenticated restores auth and notifies', () => {
        const cm = createOnline();
        const authStates: boolean[] = [];
        cm.subscribeAuth(a => authStates.push(a));

        cm.reportUnauthenticated();
        cm.reportAuthenticated();

        expect(cm.authenticated).toBe(true);
        expect(authStates).toEqual([true, false, true]);
    });
});

// ── Spike: Auth state edge cases ──

describe('Spike: auth state edge cases', () => {
    test('duplicate reportUnauthenticated does not fire callback twice', () => {
        const cm = createOnline();
        const authStates: boolean[] = [];
        cm.subscribeAuth(a => authStates.push(a));

        cm.reportUnauthenticated();
        cm.reportUnauthenticated(); // duplicate

        // Only one false notification
        expect(authStates).toEqual([true, false]);
    });

    test('duplicate reportAuthenticated does not fire callback twice', () => {
        const cm = createOnline();
        const authStates: boolean[] = [];
        cm.subscribeAuth(a => authStates.push(a));

        cm.reportUnauthenticated();
        cm.reportAuthenticated();
        cm.reportAuthenticated(); // duplicate

        expect(authStates).toEqual([true, false, true]);
    });

    test('auth callback error does not break other callbacks on state change', () => {
        const cm = createOnline();
        const results: boolean[] = [];

        // Subscribe both before any state change — initial calls are safe
        // (first subscriber gets true, throws; second subscriber gets true, records)
        let throwOnNext = false;
        cm.subscribeAuth(() => { if (throwOnNext) throw new Error('boom'); });
        cm.subscribeAuth(a => { if (throwOnNext) results.push(a); });

        // Now arm the throw and trigger a state change
        throwOnNext = true;
        cm.reportUnauthenticated();

        // Second callback still fires despite first throwing
        expect(results).toEqual([false]);
    });
});
