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
import { doorHost, doorStand, showDoor, stepThrough, hazard, engageDoor, doorEngaged, fingerprint, pressable, skippable, say, step, stumbled, mood, verdict, nameYourself } from './door';
import { log, SEG } from './logger';
import { enrolPasskey, assertPasskey, forgetPasskey, cancelled } from './passkey';
import { profile } from './arrival';
import { connectivity } from './client/connectivity';

// Long enough to read the refusal before the door goes back to waiting.
const REFUSAL_MS = 900;

// Long enough for the burst to be the reward rather than a flicker on the way out.
const REWARD_MS = 1600;

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

/** Whether this browser already holds a session the node honours.
 *
 *  /auth/status answers 200 either way — identity absent is the node saying
 *  "signed out". A failed ask is not that answer: reporting signed-out on a
 *  proxy or store error opens a door over a session that is still live, the
 *  same lie logOut below refuses to tell. Could-not-ask throws. */
export async function signedIn(): Promise<boolean> {
    const response = await apiFetch('/auth/status');
    if (!response.ok) {
        throw new Error(`the node did not say whether this session is honoured (${response.status} ${response.statusText})`);
    }
    const { identity } = await response.json() as { identity?: string };
    return Boolean(identity);
}

/**
 * The authenticator is only given to a document that was just pressed. Where a
 * browser will say whether that is still true, it is asked; where it will not,
 * a fresh press is taken. Nothing reaches a passkey without going through here.
 */
async function pressed(next: HalfAdmission['next']): Promise<void> {
    if (navigator.userActivation?.isActive) return;

    const stand = doorStand();
    await new Promise<void>((done) => {
        stand.replaceChildren();
        stand.append(fingerprint(() => done()));
        say(next === 'enrol'
            ? 'press to set this device up as your passkey'
            : 'press to confirm with your passkey');
    });
}

/**
 * The half of admission laye cannot do. An account with no device enrols one
 * now, because the first login is the setup rather than a step to come back to.
 */
export async function standOnADevice(admission: HalfAdmission): Promise<void> {
    await pressed(admission.next);

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
    mood('admitted');
    verdict('yes');
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
        const stand = doorStand();
        // Signing in is not an unusual condition, whatever the door wore last.
        hazard(false);
        shut();
        showDoor();

        function shut() {
            mood('rest');
            stand.replaceChildren();
            host.replaceChildren();
            const print = fingerprint(() => { print.disabled = true; void press(print); });
            stand.append(print);
            say('');
            void offer();
        }

        // The right column, drawn with the door rather than behind a link: the
        // ways in this operator has enabled are all visible at once, and the
        // fingerprint does not depend on any of them.
        async function offer() {
            let providers;
            try {
                providers = await fetchProviders();
            } catch (e) {
                // A node that will not list its providers costs the third
                // column. It does not cost the way in that needs no provider.
                log.warn(SEG.UI, '[Door] could not list what this node accepts:', e);
                return;
            }
            if (providers.length === 0) return;

            try {
                await renderCeremony(host, providers, say);
                host.replaceChildren();
                say('signing in...');
                nameYourself();
                await standOnADevice(await layeLogin());
                await through();
            } catch (e) {
                stumbled('linking an account', e);
                mood('refused');
                verdict('no');
                await new Promise((rest) => setTimeout(rest, REFUSAL_MS));
                shut();
            }
        }

        async function press(print: HTMLButtonElement) {
            host.replaceChildren();
            say('signing in...');
            nameYourself();
            try {
                await standOnADevice(await layeLogin());
                await through();
                return;
            } catch (e) {
                if (cancelled(e)) say('cancelled');
                else if (needsCeremony(e)) say('this browser speaks for no account this node lists');
                else stumbled('signing in', e);
                // Cancelling is not a refusal — the node said nothing, so the
                // fingerprint says nothing. A refusal is held long enough to
                // be read, because shut() builds a fresh white one.
                if (!cancelled(e)) {
                    mood('refused');
                    verdict('no');
                    await new Promise((rest) => setTimeout(rest, REFUSAL_MS));
                }
                print.disabled = false;
                shut();
            }
        }

        async function through() {
            // The burst is the reward for coming in, so the door does not begin
            // closing over it until it has been there long enough to be one.
            await new Promise((seen) => setTimeout(seen, REWARD_MS));
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
    const stand = doorStand();
    // Same reason as first time setup: this draws over whatever was there, so
    // anything waiting on the old face is waiting on nothing.
    abandonDoor();
    engageDoor(true);
    draw();
    showDoor();

    async function draw() {
        stand.replaceChildren();
        host.replaceChildren();
        // The same fingerprint, not pressable, in the same place. Here it is
        // who you are rather than the way in, and the door is the same door.
        const emblem = fingerprint(() => {});
        emblem.disabled = true;
        stand.append(emblem);
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

