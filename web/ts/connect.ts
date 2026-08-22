/**
 * Connect device: admitting a second device from one that is already in.
 */

// No provider, no instance, nothing typed. The device that is already admitted
// puts a code on its screen; the one arriving photographs it and is asked for a
// finger (ADR-032).

// The code is worth something for as long as a photograph of a screen is, which
// is minutes. What it becomes is worth thirty days.

import { apiFetch } from './client';
import { whenReady as layeWhenReady, did as layeDID } from './laye';
import { doorHost, showDoor, stepThrough, pressable, skippable, say, step, stumbled } from './door';
import { enrolPasskey, cancelled } from './passkey';

const TICKET_MARK = '#connect=';

interface Code {
    ticket: string;
    expires_in: number;
    level: string;
    grant_days: number;
}

interface Arrival {
    next: string;
    level: string;
    grant_days: number;
}

/** The ticket this page was opened with, or empty. */
export function ticketInURL(): string {
    const hash = window.location.hash;
    if (!hash.startsWith(TICKET_MARK)) return '';
    return hash.slice(TICKET_MARK.length);
}

/** Takes a spent ticket out of the address bar, so a refresh is not a retry. */
function clearTicket(): void {
    const clean = window.location.pathname + window.location.search;
    window.history.replaceState(null, '', clean);
}

/** Where the arriving device should point its browser. */
function codeURL(ticket: string): string {
    return window.location.origin + window.location.pathname + TICKET_MARK + ticket;
}

/** Draws the code on the door of the device that is already signed in. */
export function showConnectCode(host: HTMLElement, back: () => void): void {
    void make();

    async function make() {
        host.replaceChildren();
        say('making a code');
        try {
            const response = await apiFetch('/auth/connect', { method: 'POST' });
            if (!response.ok) {
                const detail = await response.json().catch(() => ({ error: response.statusText }));
                throw new Error(detail.error ?? `this node would not make a code (${response.status})`);
            }
            draw(await response.json() as Code);
        } catch (e) {
            stumbled('making a code', e);
            host.replaceChildren();
            host.append(pressable('try again', () => { void make(); }));
            host.append(skippable('back', back));
        }
    }

    async function draw(code: Code) {
        host.replaceChildren();

        // Imported here so a browser that never connects a device never pays
        // for the encoder.
        const { renderQR } = await import('./qr');
        const frame = document.createElement('div');
        frame.className = 'door-qr';
        frame.append(renderQR(codeURL(code.ticket), 176));
        host.append(frame);
        host.append(skippable('back', back));

        say(`scan within ${code.expires_in} seconds — this device becomes ${code.level} for ${code.grant_days} days`);

        let left = code.expires_in;
        const tick = setInterval(() => {
            left -= 1;
            if (left > 0) {
                say(`scan within ${left} seconds — this device becomes ${code.level} for ${code.grant_days} days`);
                return;
            }
            clearInterval(tick);
            host.replaceChildren();
            host.append(pressable('make another code', () => { void make(); }));
            host.append(skippable('back', back));
            say('that code has run out');
        }, 1000);
    }
}

/**
 * The arriving device. Resolves once this device holds a session, and never
 * any other way — a spent ticket cannot be spent again, so there is nothing to
 * fall back to but the ordinary door.
 */
export function arriveByCode(ticket: string): Promise<boolean> {
    return new Promise((resolve) => {
        const host = doorHost();
        void redeem();
        showDoor();

        async function redeem() {
            host.replaceChildren();
            say('connecting this device');
            try {
                // laye's key is what says which browser arrived. It is not the
                // proof — the ticket is — so a browser without one still gets in.
                await layeWhenReady();

                const response = await apiFetch('/auth/connect/redeem', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ticket, did: layeDID() }),
                });
                clearTicket();
                if (!response.ok) {
                    const detail = await response.json().catch(() => ({ error: response.statusText }));
                    throw new Error(detail.error ?? `that code was refused (${response.status})`);
                }
                offer(await response.json() as Arrival);
            } catch (e) {
                stumbled('connecting this device', e);
                host.replaceChildren();
                host.append(pressable('sign in the usual way', () => { resolve(false); }));
            }
        }

        // What it is about to become, before it commits. Possession of a ticket
        // is delegation, and a scan that granted SUPER silently would be one.
        function offer(arrival: Arrival) {
            host.replaceChildren();
            host.append(pressable('add this device', () => { void enrol(); }));
            host.append(skippable('no', () => { resolve(false); }));
            say(`this device becomes ${arrival.level} for ${arrival.grant_days} days`);
        }

        async function enrol() {
            host.replaceChildren();
            try {
                await enrolPasskey(say);
                step('this device is connected');
                stepThrough();
                resolve(true);
            } catch (e) {
                if (cancelled(e)) say('cancelled');
                else stumbled('adding this device', e);
                host.replaceChildren();
                host.append(pressable('try again', () => { void enrol(); }));
                host.append(skippable('no', () => { resolve(false); }));
            }
        }
    });
}
