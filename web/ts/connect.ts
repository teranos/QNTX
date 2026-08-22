/**
 * Connect device: admitting a second device from one that is already in.
 */

// No provider, no instance, nothing typed. The device that is already admitted
// puts a code on its screen; the one arriving photographs it and is asked for a
// finger (ADR-032).

// The code is worth something for as long as a photograph of a screen is, which
// is minutes. What it becomes is worth thirty days.

// VERIFY: unrun by a person. The camera, the scan and the enrolment after it
// are testable at the next tagged release, and testing them means nuking the
// ROOT User and re-initializing the deployment (server/auth/connect.go).

import { apiFetch } from './client';
import { whenReady as layeWhenReady, did as layeDID } from './laye';
import { doorHost, showDoor, stepThrough, pressable, skippable, say, step, stumbled } from './door';
import { enrolPasskey, cancelled } from './passkey';
import { scanFrame } from './qr-scan';

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

/** Where the app for this node is downloaded, or empty when it offers none. */
export async function appURL(): Promise<string> {
    try {
        const response = await apiFetch('/auth/status');
        if (!response.ok) return '';
        const { app_url } = await response.json() as { app_url?: string };
        return app_url ?? '';
    } catch {
        return '';
    }
}

/** The app, as a code. A phone with no app has nothing to scan a code with, so
 *  this is the one that comes first. */
export async function showAppCode(host: HTMLElement): Promise<boolean> {
    const url = await appURL();
    if (!url) return false;

    const { renderQR } = await import('./qr');
    const frame = document.createElement('div');
    frame.className = 'door-qr door-qr-small';
    frame.append(renderQR(url, 116));

    const caption = document.createElement('div');
    caption.className = 'door-caption';
    caption.textContent = 'get the app';

    host.append(frame, caption);
    return true;
}

/** The ticket this page was opened with, or empty. */
export function ticketInURL(): string {
    const hash = window.location.hash;
    if (!hash.startsWith(TICKET_MARK)) return '';
    return hash.slice(TICKET_MARK.length);
}

/**
 * Whether this device is one that scans rather than one that shows. The bar is
 * already at the top here, under the island, and the camera is the way in.
 */
export function canScan(): boolean {
    return window.matchMedia('(max-width: 768px)').matches
        && typeof navigator.mediaDevices?.getUserMedia === 'function';
}

/** The ticket inside a scanned URL, or empty when the code is not this node's. */
function ticketIn(scanned: string): string {
    const mark = scanned.indexOf(TICKET_MARK);
    if (mark < 0) return '';
    if (!scanned.startsWith(window.location.origin)) return '';
    return scanned.slice(mark + TICKET_MARK.length);
}

// Frames are read at this width. Full sensor resolution is several times the
// work for no more code — the modules are large on a screen held close.
const SCAN_WIDTH = 480;

/**
 * Opens the camera and resolves with the ticket in the first code it reads.
 * Resolves empty when the person walks away from it.
 */
export function scanCode(host: HTMLElement, give: () => void): Promise<string> {
    return new Promise((resolve) => {
        const video = document.createElement('video');
        video.setAttribute('playsinline', '');
        video.muted = true;
        video.className = 'door-camera';

        const canvas = document.createElement('canvas');
        const context = canvas.getContext('2d', { willReadFrequently: true });

        let stream: MediaStream | null = null;
        let looking = true;

        function stop() {
            looking = false;
            for (const track of stream?.getTracks() ?? []) track.stop();
        }

        host.replaceChildren(video, skippable('back', () => { stop(); give(); resolve(''); }));
        say('point this at the code on your other device');

        function look() {
            if (!looking) return;
            if (context === null || video.videoWidth === 0) {
                requestAnimationFrame(look);
                return;
            }

            const scale = Math.min(1, SCAN_WIDTH / video.videoWidth);
            canvas.width = Math.round(video.videoWidth * scale);
            canvas.height = Math.round(video.videoHeight * scale);
            context.drawImage(video, 0, 0, canvas.width, canvas.height);

            const frame = context.getImageData(0, 0, canvas.width, canvas.height);
            const scanned = scanFrame(frame.data, canvas.width, canvas.height);
            if (scanned === null) {
                requestAnimationFrame(look);
                return;
            }

            const ticket = ticketIn(scanned);
            if (!ticket) {
                // A code, but not one of this node's. Saying so beats looking
                // like the camera never saw it.
                say('that code is not this node’s');
                requestAnimationFrame(look);
                return;
            }
            stop();
            resolve(ticket);
        }

        navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
            .then(opened => {
                stream = opened;
                video.srcObject = opened;
                return video.play();
            })
            .then(() => { requestAnimationFrame(look); })
            .catch((e: unknown) => {
                stop();
                stumbled('opening the camera', e);
                host.replaceChildren(pressable('back', () => { give(); resolve(''); }));
            });
    });
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
