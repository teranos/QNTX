/**
 * Signing in, and signing back out, at the door.
 */

// One press. laye proves the key this browser holds, the authenticator proves
// the device, and neither asks for an instance, a name to log in with, or a
// password.

// The door has two faces. Shut, it is a fingerprint. Open, it is who you are
// and the two ways back out: ending this session, or having the device forget
// you altogether.

import { apiFetch } from './client';
import { login as layeLogin, LayeLoginRefused, type LayeAdmission } from './laye';
import { fetchProviders, renderCeremony } from './ceremony';
import { doorHost, showDoor, stepThrough, fingerprint, pressable, skippable, say, step, stumbled } from './door';
import { enrolPasskey, assertPasskey, forgetPasskey, cancelled } from './passkey';
import { profile } from './arrival';
import { showConnectCode } from './connect';

// One door at a time. Every 401 asks for one, and a second would be drawn over
// the first with both waiting on the same press.
let standing: Promise<void> | null = null;

/** Whether a failed login is the node saying this device speaks for no account
 *  it lists — the one question the ceremony answers. */
export function needsCeremony(e: unknown): boolean {
    return e instanceof LayeLoginRefused && e.status === 403;
}

/** Whether this browser already holds a session the node honours. */
export async function signedIn(): Promise<boolean> {
    try {
        const response = await apiFetch('/auth/status');
        if (!response.ok) return false;
        const { identity } = await response.json() as { identity?: string };
        return Boolean(identity);
    } catch {
        return false;
    }
}

/**
 * The half of admission laye cannot do. An account with no device enrols one
 * now, because the first login is the setup rather than a step to come back to.
 */
export async function standOnADevice(admission: LayeAdmission): Promise<void> {
    if (admission.next === 'enrol') {
        say('set up this device as your passkey');
        await enrolPasskey(say);
        step('this device is now a passkey');
        return;
    }
    say('confirm with your passkey');
    await assertPasskey(say);
    step('signed in');
}

/**
 * Draws the shut door and resolves once this browser holds a session. It does
 * not resolve any other way: the door is the gate.
 */
export function openDoor(): Promise<void> {
    if (standing) return standing;
    standing = new Promise((resolve) => {
        const host = doorHost();
        shut();
        showDoor();

        function shut() {
            host.replaceChildren();
            host.append(fingerprint(() => { void press(); }));
            host.append(skippable('link an account instead', () => { void ceremony(); }));
            say('sign in');
        }

        async function press() {
            host.replaceChildren();
            say('signing in...');
            try {
                await standOnADevice(await layeLogin());
                through();
                return;
            } catch (e) {
                if (cancelled(e)) say('cancelled');
                else if (!needsCeremony(e)) { stumbled('signing in', e); shut(); return; }
                else { say('this browser speaks for no account this node lists'); await ceremony(); return; }
                shut();
                return;
            }
        }

        // The last resort, and the only place anything is typed. A browser the
        // node has never seen has nothing else to offer it.
        async function ceremony() {
            host.replaceChildren();
            try {
                const providers = await fetchProviders();
                if (providers.length === 0) {
                    throw new Error('this node offers no identity providers');
                }
                await renderCeremony(host, providers, say);
                say('signing in...');
                await standOnADevice(await layeLogin());
                through();
            } catch (e) {
                stumbled('linking an account', e);
                host.replaceChildren();
                host.append(pressable('back', () => { shut(); }));
            }
        }

        function through() {
            stepThrough();
            standing = null;
            resolve();
        }
    });
    return standing;
}

/**
 * The open door: who the node thinks you are, and the two ways back out. This
 * is where logging out lives, because logging out is walking back through it.
 */
export function standAtTheDoor(): void {
    const host = doorHost();
    draw();
    showDoor();

    async function draw() {
        host.replaceChildren();
        host.append(pressable('connect a device', () => { showConnectCode(host, () => { void draw(); }); }));
        host.append(pressable('log out', () => { void logOut(); }));
        host.append(pressable('forget this device', () => { void forget(); }));
        host.append(skippable('stay signed in', () => { stepThrough(); }));

        try {
            const who = await profile();
            say(`signed in as ${who.name}`);
        } catch {
            say('signed in');
        }
    }

    // Only the node can end a session. Showing signed-out on a proxy error
    // leaves the cookie live and says otherwise.
    async function logOut() {
        host.replaceChildren();
        say('logging out...');
        try {
            const response = await apiFetch('/auth/logout', { method: 'POST' });
            if (!response.ok) {
                throw new Error(`the node answered ${response.status} ${response.statusText}; you are still signed in`);
            }
            step('logged out');
            void openDoor();
        } catch (e) {
            stumbled('logging out', e);
            void draw();
        }
    }

    // The session ends either way; what this adds is the credential going with
    // it, so the next arrival on this device is a stranger.
    async function forget() {
        host.replaceChildren();
        say('touch your passkey to have this device forget you');
        try {
            await forgetPasskey(say);
            step('this device has forgotten you');
            void openDoor();
        } catch (e) {
            if (cancelled(e)) say('cancelled');
            else stumbled('forgetting this device', e);
            void draw();
        }
    }
}

/** The way back to the door once you are through it. */
export function mountDoorLatch(): void {
    const controls = document.querySelector('#system-drawer-header .controls');
    if (!controls || document.querySelector('.door-latch')) return;

    const latch = document.createElement('button');
    latch.className = 'door-latch';
    latch.type = 'button';
    latch.title = 'The door';
    latch.setAttribute('aria-label', 'The door');
    latch.innerHTML = `<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M13.14 21C10.81 19.54 9.25 16.95 9.25 14c0-1.52 1.23-2.75 2.75-2.75s2.75 1.23 2.75 2.75c0 1.52 1.23 2.75 2.75 2.75s2.75-1.23 2.75-2.75C20.25 9.44 16.55 5.75 12 5.75S3.76 9.44 3.76 14c0 1.02.11 2 .32 2.95M8.49 20.3C7.24 18.51 6.5 16.34 6.5 14c0-3.04 2.46-5.5 5.5-5.5s5.5 2.46 5.5 5.5M19.67 6.48C17.8 4.35 15.06 3 12 3S6.2 4.35 4.33 6.48"/></svg>`;
    latch.addEventListener('click', event => {
        event.stopPropagation();
        standAtTheDoor();
    });
    controls.prepend(latch);
}
