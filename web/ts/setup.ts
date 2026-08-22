/**
 * First-time setup, drawn in the loader.
 */

// A node with root identities listed and no User belongs to nobody. That is
// not an auth state, so no auth glyph opens for it — the loader simply does
// not finish, the way it does not finish for a node that is not answering.

// What the node publishes is how it can be entered, never by whom. Pressing a
// method is what sends you to the provider; the browser learns the instance by
// arriving at it, not by being told beforehand.

import { apiFetch } from './client';
import { peerPubkeyHex, whenReady as layeWhenReady, login as layeLogin, collectedBinding, acceptBinding } from './laye';
import { log, SEG } from './logger';

export interface SetupMethod {
    provider: string;
    label: string;
}

export interface SetupState {
    claimed: boolean;
    governed: boolean;
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

/** The loader's own voice, so setup reads as more of what it was already saying. */
function say(message: string): void {
    const status = document.getElementById('loading-status');
    if (status) status.textContent = message;
}

/** A line in the loader's log panel, same as every other step. */
function step(message: string): void {
    if (window.logLoaderStep) window.logLoaderStep(message);
}

function methodLine(label: string): HTMLElement {
    const line = document.createElement('div');
    line.textContent = label;
    line.style.fontSize = '14px';
    line.style.color = '#888';
    line.style.padding = '6px 0';
    line.style.cursor = 'pointer';
    line.style.transition = 'color 0.15s ease';
    line.addEventListener('mouseenter', () => { line.style.color = '#ccc'; });
    line.addEventListener('mouseleave', () => { line.style.color = '#888'; });
    return line;
}

/**
 * Waits for the binding the provider consent produces. The redirect severs
 * window.opener, so the result is fetched rather than told.
 */
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
 * Renders the ways in and resolves once one has been proven and the node has
 * a User. It does not resolve any other way: the loader is the gate.
 */
export function claimNode(state: SetupState): Promise<void> {
    return new Promise((resolve) => {
        const logs = document.getElementById('loader-logs');
        const host = document.createElement('div');
        host.style.display = 'flex';
        host.style.flexDirection = 'column';
        host.style.alignItems = 'center';
        host.style.marginBottom = '16px';
        logs?.parentElement?.insertBefore(host, logs);

        say('this node belongs to nobody yet');

        for (const method of state.methods ?? []) {
            const line = methodLine(method.label);
            line.addEventListener('click', () => { void claim(method); });
            host.append(line);
        }

        async function claim(method: SetupMethod) {
            host.replaceChildren();
            try {
                if (!await layeWhenReady() || !peerPubkeyHex()) {
                    throw new Error('laye is still starting — this browser has no key yet');
                }

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

                // Proving a listed route is what creates the User (ADR-031).
                // Nothing after this is a condition of the node being owned.
                say('claiming this node');
                await layeLogin();
                step('this node has a ROOT User');
                resolve();
            } catch (e) {
                say(e instanceof Error ? e.message : String(e));
                host.replaceChildren();
                for (const again of state.methods ?? []) {
                    const line = methodLine(again.label);
                    line.addEventListener('click', () => { void claim(again); });
                    host.append(line);
                }
            }
        }
    });
}
