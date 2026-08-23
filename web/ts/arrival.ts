/**
 * The arrival: what a person chose to be called, asked once and never required.
 */

// Proving a listed route is what created the User, so this page is a courtesy
// rather than a gate. Skipping it leaves a ROOT User called root, which is what
// they were already called (ADR-031).

import { apiFetch } from './client';
import { field, pressable, skippable, say } from './door';

export interface Profile {
    display_name?: string;
    name: string;
    email_addresses?: string[];
}

/** What this node calls the signed-in User. */
export async function profile(): Promise<Profile> {
    const response = await apiFetch('/auth/user/arrival');
    if (!response.ok) {
        throw new Error(`this node did not say what it calls you (${response.status} ${response.statusText})`);
    }
    return await response.json() as Profile;
}

/** Sends what was typed. Empty fields are a person saying nothing, not an error. */
async function record(displayName: string, email: string): Promise<Profile> {
    const response = await apiFetch('/auth/user/arrive', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ display_name: displayName, email }),
    });
    if (!response.ok) {
        const detail = await response.json().catch(() => ({ error: response.statusText }));
        throw new Error(detail.error ?? `this node would not record it (${response.status})`);
    }
    return await response.json() as Profile;
}

/** Draws the page and resolves once it is answered or skipped. Null is skipped. */
export function renderArrival(host: HTMLElement): Promise<Profile | null> {
    return new Promise((resolve) => {
        const name = field('display name', 'text');
        const email = field('email', 'email');
        const go = pressable('Continue', () => { void submit(); });
        const later = skippable('skip — you are root either way', () => { resolve(null); });

        host.append(name.el, email.el, go, later);
        say('what should this node call you?');
        name.input.focus();

        async function submit() {
            const typedName = name.input.value.trim();
            const typedEmail = email.input.value.trim();
            if (!typedName && !typedEmail) {
                resolve(null);
                return;
            }

            try {
                resolve(await record(typedName, typedEmail));
            } catch (e) {
                say(e instanceof Error ? e.message : String(e), true);
            }
        }

        for (const input of [name.input, email.input]) {
            input.addEventListener('keydown', event => {
                if (event.key === 'Enter') {
                    event.preventDefault();
                    void submit();
                }
            });
        }
    });
}
