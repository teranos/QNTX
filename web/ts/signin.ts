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
import { login as layeLogin, LayeLoginRefused, type HalfAdmission } from './laye';
import { fetchProviders, renderCeremony } from './ceremony';
import { doorHost, showDoor, stepThrough, hazard, engageDoor, doorEngaged, fingerprint, pressable, skippable, say, step, stumbled } from './door';
import { enrolPasskey, assertPasskey, forgetPasskey, cancelled } from './passkey';
import { profile } from './arrival';
import { connectivity } from './client/connectivity';

// One door at a time. Every 401 asks for one, and a second would be drawn over
// the first with both waiting on the same press.
let standing: Promise<void> | null = null;

/**
 * Abandons a door nobody can press any more.
 */

// A door that was drawn and then drawn over never resolves, and the promise it
// left behind makes every later openDoor hand back that dead one and render
// nothing. Whoever takes the panel says so here.
export function abandonDoor(): void {
    standing = null;
}

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
export async function standOnADevice(admission: HalfAdmission): Promise<void> {
    if (admission.next === 'enrol') {
        say('set up this device as your passkey');
        await enrolPasskey(say);
        step('this device is now a passkey');
        admitted();
        return;
    }
    say('confirm with your passkey');
    await assertPasskey(say);
    step('signed in');
    admitted();
}

// Connectivity asks the node who you are once, at startup, and again only when
// the tab is hidden and shown. Signing in after that is something it has to be
// told, or everything waiting on it keeps waiting.

// The namespaces bar is one of those, and the way back to the door lives in it.
function admitted(): void {
    connectivity.reportAuthenticated();
}

/**
 * Draws the shut door and resolves once this browser holds a session. It does
 * not resolve any other way: the door is the gate.
 */
export function openDoor(): Promise<void> {
    // First time setup has the panel. A 401 arriving mid-ceremony must not draw
    // the fingerprint over a claim that is halfway through.
    if (doorEngaged()) return standing ?? Promise.resolve();
    if (standing) return standing;

    engageDoor(true);
    standing = new Promise((resolve) => {
        const host = doorHost();
        // Signing in is not an unusual condition, whatever the door wore last.
        hazard(false);
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
            engageDoor(false);
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
    if (doorEngaged()) return;

    const host = doorHost();
    // Same reason as first time setup: this draws over whatever was there, so
    // anything waiting on the old face is waiting on nothing.
    abandonDoor();
    engageDoor(true);
    draw();
    showDoor();

    async function draw() {
        host.replaceChildren();
        // The same fingerprint, not pressable. Here it is who you are rather
        // than the way in, and the door is the same door either way.
        const emblem = fingerprint(() => {});
        emblem.disabled = true;
        host.append(emblem);
        host.append(pressable('log out', () => { void logOut(); }));
        host.append(pressable('forget this device', () => { void forget(); }));
        host.append(skippable('stay signed in', () => { engageDoor(false); stepThrough(); }));

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
            // Handed straight to the shut face, so the panel changes hands
            // rather than being let go of and grabbed again.
            engageDoor(false);
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
            // Handed straight to the shut face, so the panel changes hands
            // rather than being let go of and grabbed again.
            engageDoor(false);
            void openDoor();
        } catch (e) {
            if (cancelled(e)) say('cancelled');
            else stumbled('forgetting this device', e);
            void draw();
        }
    }
}

