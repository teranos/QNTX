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
import { backendPath } from './client/url';
import { holdSession, dropSession } from './client/session';
import { inApp, homeInSheet, APP_DOOR } from './app-door';
import { login as layeLogin, LayeLoginRefused, type HalfAdmission } from './laye';
import { fetchProviders, renderCeremony } from './ceremony';
import { doorHost, doorStand, showDoor, stepThrough, hazard, engageDoor, doorEngaged, fingerprint, tokenMark, relayed, pressable, skippable, say, step, stumbled, mood, verdict, nameYourself } from './door';
import { log, SEG } from './logger';
import { enrolPasskey, assertPasskey, forgetPasskey, cancelled } from './passkey';
import { profile } from './arrival';
import { connectivity } from './client/connectivity';

// Long enough to read the refusal before the door goes back to waiting. Longer
// than the reward below: getting in needs no explanation, being turned away
// does, and the red is the whole of what says so.
const REFUSAL_MS = 2600;

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
 * The half-admission this browser holds, or null. A door sent home with one
 * carried already proved its route in the app; the press here is the passkey
 * and nothing before it. Could-not-ask is null: the press then proves the
 * route the ordinary way, which asks again rather than never finishing.
 */
export async function halfAdmitted(): Promise<HalfAdmission | null> {
    try {
        const response = await apiFetch('/auth/status');
        if (!response.ok) return null;
        const said = await response.json() as { half_admitted?: string; next?: 'enrol' | 'assert' };
        if (!said.half_admitted || !said.next) return null;
        return { did: '', admitted_as: said.half_admitted, next: said.next };
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Door] could not ask whether a half-admission is held:', err);
        return null;
    }
}

/**
 * The authenticator is only given to a document that was just pressed. Where a
 * browser will say whether that is still true, it is asked; where it will not,
 * a fresh press is taken. Nothing reaches a passkey without going through here.
 */
async function pressed(next: HalfAdmission['next'], afresh = false): Promise<void> {
    // A press that is still live is not asked for twice, except where the
    // question changed under it: a device that could not assert is asked
    // whether to enrol, and that is a new question with its own press.
    if (!afresh && navigator.userActivation?.isActive) return;

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
 *
 * An account with devices asserts one. A device holding none of them cannot,
 * and the authenticator says so the same way it says a person declined; the
 * half-admission is still live either way, so the device is offered enrolment
 * and one more press decides. The node never limited how many devices an
 * identity holds (ADR-030); the door did, by only ever asking for the one.
 *
 * "My key, I'm Root. I want to have as many keys or devices as I want."
 */
export async function standOnADevice(admission: HalfAdmission): Promise<void> {
    if (inApp() && await standAtHome(admission)) return;
    await pressed(admission.next);

    if (admission.next === 'enrol') {
        await enrol();
        return;
    }
    say('confirm with your passkey');
    let done;
    try {
        done = await assertPasskey(say);
    } catch (e) {
        if (!cancelled(e)) throw e;
        say('none of your passkeys is on this device — press to set it up as one');
        await pressed('enrol', true);
        await enrol();
        return;
    }
    step('signed in');
    admitted();
    sentBack(done.return);
}

async function enrol(): Promise<void> {
    say('set up this device as your passkey');
    const done = await enrolPasskey(say);
    step('this device is now a passkey');
    admitted();
    sentBack(done.return);
}

/**
 * The app's half of admission. Its page is at a scheme, which is never a
 * passkey origin and gets no cookie back from the node, so the passkey is
 * done at home in the sheet and the session comes back by ticket, held here
 * and presented as a bearer from then on. False where the app has no sheet,
 * and the device is stood on the way a browser does.
 */
async function standAtHome(admission: HalfAdmission): Promise<boolean> {
    say('the passkey is done at home...');
    // What the app proved rides along, so home asks for the passkey and not
    // for the provider a second time.
    const ticket = await homeInSheet(backendPath('/auth/door/home'), admission.pending);
    if (ticket === null) return false;
    say('back from home...');
    const response = await apiFetch('/auth/door/home/result?home=' + encodeURIComponent(ticket)
        + '&door=' + encodeURIComponent(APP_DOOR));
    if (!response.ok) {
        throw new Error(`the node held no session for the ticket home sent back (${response.status} ${response.statusText})`);
    }
    const { session } = await response.json() as { session: string };
    holdSession(session);
    step('signed in');
    admitted();
    return true;
}

// A browser that came from a door is sent back to it with the session it just
// earned (ADR-030). The node names the place; this only goes there.
function sentBack(to: string | undefined): void {
    if (!to) return;
    say('back to where you came from');
    window.location.assign(to);
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
            // A passkey belongs to the node's origin, so pressing a fingerprint
            // here could never complete. The key is what completes instead.
            if (relayed()) {
                const key = tokenMark(() => { key.disabled = true; void turn(key); });
                stand.append(key);
                say('');
                return;
            }
            const print = fingerprint(() => { print.disabled = true; void press(print); });
            stand.append(print);
            say('');
            // Sent home with a route already proven, the press is the passkey,
            // and the door says so rather than looking like a login.
            void halfAdmitted().then((half) => {
                if (!half) return;
                say(half.next === 'enrol'
                    ? 'press to set this device up as your passkey'
                    : 'press to confirm with your passkey');
            });
            void offer();
        }

        // The relay authenticates as itself, so this asks the node whether the
        // token it is carrying is one the node honours. That answer is the login.
        async function turn(key: HTMLButtonElement) {
            host.replaceChildren();
            mood('committed');
            say('asking the node about the token the dev server carries...');
            nameYourself();
            try {
                if (!await signedIn()) {
                    throw new Error('the node does not honour the token the dev server is carrying');
                }
                admitted();
                await through();
                return;
            } catch (e) {
                stumbled('signing in with the relay token', e);
                mood('refused');
                verdict('no');
                await new Promise((rest) => setTimeout(rest, REFUSAL_MS));
                key.disabled = false;
                shut();
            }
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
                // A half-admission already held is the route proven; proving
                // it again with this browser's key would ask for the provider
                // a second time, which is what home did to the app.
                await standOnADevice(await halfAdmitted() ?? await layeLogin());
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
    draw().catch((err: unknown) => log.error(SEG.UI, 'The door failed to draw:', err));
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
        } catch (err) {
            log.warn(SEG.UI, 'Profile unavailable; the door shows signed-in without a name:', err);
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
            dropSession();
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
            dropSession();
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

