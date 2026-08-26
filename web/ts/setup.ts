/**
 * First-time setup, drawn on the door.
 */

// A node with root identities listed and no User belongs to nobody. That is not
// an auth state, so no auth glyph opens for it — the scrim lifts onto a door
// nobody has a key to yet.

// What the node publishes is how it can be entered, never by whom. Pressing a
// method is what sends you to the provider; the browser learns the instance by
// arriving at it, not by being told beforehand.

import { apiFetch } from './client';
import { peerPubkeyHex, whenReady as layeWhenReady, login as layeLogin, did as layeDID, collectedBinding, acceptBinding, type HalfAdmission } from './laye';
import { doorHost, doorStand, showDoor, stepThrough, hazard, engageDoor, pressable, fingerprint, say, step, stumbled } from './door';
import { providerMark } from './provider-marks';
import { renderArrival } from './arrival';
import { standOnADevice, abandonDoor } from './signin';
import { log, SEG } from './logger';

export interface SetupMethod {
    provider: string;
    label: string;
}

export interface SetupState {
    claimed: boolean;
    /** Absent on a claimed node, which says nothing about how it is configured. */
    governed?: boolean;
    methods?: SetupMethod[];
}

const POLL_INTERVAL_MS = 2000;
const CLAIM_TIMEOUT_MS = 600000;

/** What this node says about being owned. */
export async function setupState(): Promise<SetupState> {
    const response = await apiFetch('/setup');
    if (!response.ok) {
        throw new Error(`this node did not say whether it has an owner (${response.status} ${response.statusText})`);
    }
    return await response.json() as SetupState;
}

/** Waits for the binding the provider consent produces. The redirect severs
 *  window.opener, so the result is fetched rather than told. */
async function collectBinding(): Promise<void> {
    let waited = 0;
    for (;;) {
        await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS));
        waited += POLL_INTERVAL_MS;

        let binding = null;
        try {
            binding = await collectedBinding();
        } catch (error: unknown) {
            log.warn(SEG.UI, '[Setup] could not read the result:', error);
        }
        if (binding) {
            acceptBinding(binding);
            return;
        }
        if (waited >= CLAIM_TIMEOUT_MS) {
            throw new Error('nobody proved an identity in time');
        }
    }
}

/**
 * Runs the whole first-time setup and resolves once this node has an owner and
 * this browser holds their session. It does not resolve any other way.
 */
export function claimNode(state: SetupState): Promise<void> {
    return new Promise((resolve) => {
        const host = doorHost();
        const stand = doorStand();
        // A 401 can beat the gate here and leave a shut door standing that this
        // is about to draw over. Its promise would outlive what drew it.
        abandonDoor();
        engageDoor(true);
        // A node nobody owns is a state it will never be in again, and the tape
        // says so before any of the words do.
        hazard(true);
        offer();
        showDoor();

        function offer() {
            host.replaceChildren();
            for (const method of state.methods ?? []) {
                host.append(pressable(
                    method.label,
                    () => { void claim(method); },
                    providerMark(method.provider),
                ));
            }
            say('this node belongs to nobody yet');
        }

        async function claim(method: SetupMethod) {
            host.replaceChildren();
            let admission: HalfAdmission;
            try {
                admission = await prove(method);
            } catch (e) {
                stumbled(`claiming with ${method.label}`, e);
                offer();
                return;
            }

            // Proving a listed route is what created the User (ADR-031), so
            // nothing after this is a condition of the node being owned.
            await device(admission);
            await named();
            hazard(false);
            engageDoor(false);
            stepThrough();
            resolve();
        }

        async function prove(method: SetupMethod): Promise<HalfAdmission> {
            step(`claiming with ${method.label}`);
            if (!await layeWhenReady() || !peerPubkeyHex()) {
                throw new Error('laye is still starting — this browser has no key yet');
            }
            step(`this browser is ${layeDID()}`);

            say(`asking ${method.label}`);
            const response = await apiFetch('/setup/claim', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    provider: method.provider,
                    peer_pubkey_hex: peerPubkeyHex(),
                }),
            });
            if (!response.ok) {
                const detail = await response.json().catch(() => ({ error: response.statusText }));
                throw new Error(detail.error ?? `${method.label} refused (${response.status})`);
            }

            const { authorize_url: authorizeURL } = await response.json() as { authorize_url: string };
            window.open(authorizeURL, '_blank', 'width=520,height=640');
            say(`authorize with ${method.label} in the window that opened`);

            await collectBinding();
            step('identity proven');

            say('claiming this node');
            const admission = await layeLogin();
            step(`admitted as ${admission.admitted_as}`);
            step(`ROOT User ${admission.user || 'unnamed'} exists, called ${admission.name || 'root'}`);
            return admission;
        }

        /** Page three: what to call them. Skipping it leaves them called root.
         *  It comes after the device because a display_name is settled once,
         *  and an admission nobody finished must not leave a permanent mark. */
        async function named() {
            host.replaceChildren();
            try {
                const profile = await renderArrival(host);
                if (profile) say(`this node will call you ${profile.name}`);
            } catch (e) {
                stumbled('recording what to call you', e);
            }
        }

        /** Page two: the device. A root identity stands on one (ADR-030), and
         *  it is what turns a half-admission into a session. */
        async function device(proven: HalfAdmission) {
            // Proving already logged in. The second login said nothing the
            // first had not, and cost a challenge and a verify where the auth
            // rate limit had least room.
            let admission = proven;
            for (;;) {
                // A browser refuses navigator.credentials to a document that
                // was not just pressed and does not hold focus, and the
                // provider popup is still holding both when this page arrives.
                await new Promise<void>(pressed => {
                    host.replaceChildren();
                    stand.replaceChildren();
                    stand.append(fingerprint(() => pressed()));
                    say(admission.next === 'enrol'
                        ? 'press to set this device up as your passkey'
                        : 'press to confirm with your passkey');
                });

                try {
                    await standOnADevice(admission);
                    return;
                } catch (e) {
                    stumbled('setting up this device', e);
                    // The half-admission was spent by the attempt that failed,
                    // so the press after this needs one of its own.
                    admission = await layeLogin();
                }
            }
        }
    });
}
