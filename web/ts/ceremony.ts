/**
 * The provider ceremony, drawn inside whatever glyph asks for it. Every step
 * needing a secret happens on the node; this builds the form the node
 * describes and waits for the binding to appear.
 */

import { apiFetch } from './client';
import { peerPubkeyHex, acceptBinding, collectedBinding, whenReady as layeWhenReady } from './laye';
import type { SignedBinding } from './laye';
import { log, SEG } from './logger';

export interface ProviderDescription {
    id: string;
    label: string;
    kind: 'redirect' | 'credential';
    host_prompt: string;
    host_placeholder: string;
    host_default: string;
    identifier_prompt: string;
    secret_prompt: string;
}

const POLL_INTERVAL_MS = 2000;
// FIXME: the node owns this deadline and the browser types it out again, so
// the two drift silently. /auth/binding/providers already describes the
// ceremony and could carry the TTL.
const CEREMONY_TIMEOUT_MS = 600000; // bindingFlowTTL, server/auth/sign_binding.go
const REMEMBERED_HOST_PREFIX = 'qntx_ceremony_host_';

export async function fetchProviders(): Promise<ProviderDescription[]> {
    const response = await apiFetch('/auth/binding/providers');
    if (!response.ok) {
        throw new Error(`this node did not list its providers (${response.status} ${response.statusText})`);
    }
    const { providers } = await response.json() as { providers: ProviderDescription[] };
    return providers;
}

interface Field {
    el: HTMLElement;
    input: HTMLInputElement;
}

function field(label: string, placeholder: string, type: string): Field {
    const wrap = document.createElement('label');
    wrap.style.display = 'flex';
    wrap.style.flexDirection = 'column';
    wrap.style.gap = '3px';
    wrap.style.fontSize = '10px';
    wrap.style.opacity = '0.7';
    wrap.style.color = 'var(--text-on-dark)';
    wrap.textContent = label;

    const input = document.createElement('input');
    input.type = type;
    input.placeholder = placeholder;
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.style.font = 'inherit';
    input.style.fontSize = '12px';
    input.style.padding = '6px 8px';
    input.style.borderRadius = '6px';
    input.style.boxSizing = 'border-box';
    input.style.width = '100%';
    input.style.background = 'rgba(255,255,255,.06)';
    input.style.color = 'var(--text-on-dark)';
    input.style.border = '1px solid #5c5488';
    wrap.append(input);

    return { el: wrap, input };
}

/**
 * Renders the ceremony into `host`. Resolves with the binding the node signed,
 * rejects when the person ran out of time.
 */
export function renderCeremony(
    host: HTMLElement,
    providers: ProviderDescription[],
    say: (message: string, bad?: boolean) => void,
): Promise<SignedBinding> {
    return new Promise((resolve, reject) => {
        const form = document.createElement('div');
        form.style.display = 'flex';
        form.style.flexDirection = 'column';
        form.style.gap = '8px';
        form.style.width = '100%';

        const choice = document.createElement('div');
        choice.style.display = 'flex';
        choice.style.gap = '6px';
        choice.style.flexWrap = 'wrap';
        choice.style.justifyContent = 'center';

        const fields = document.createElement('div');
        fields.style.display = 'flex';
        fields.style.flexDirection = 'column';
        fields.style.gap = '6px';

        const go = document.createElement('button');
        go.textContent = 'Continue';
        go.style.font = 'inherit';
        go.style.fontSize = '12px';
        go.style.padding = '7px 10px';
        go.style.borderRadius = '6px';
        go.style.background = '#4a4470';
        go.style.color = 'var(--text-on-dark)';
        go.style.border = '1px solid #5c5488';
        go.style.cursor = 'pointer';

        form.append(choice, fields, go);
        host.append(form);

        let chosen = providers[0];
        const tabs: HTMLButtonElement[] = [];

        for (const provider of providers) {
            const tab = document.createElement('button');
            tab.textContent = provider.label;
            tab.style.font = 'inherit';
            tab.style.fontSize = '11px';
            tab.style.padding = '4px 10px';
            tab.style.borderRadius = '6px';
            tab.style.cursor = 'pointer';
            tab.style.color = 'var(--text-on-dark)';
            tab.style.border = '1px solid #5c5488';
            tab.addEventListener('click', () => { chosen = provider; paint(); });
            tabs.push(tab);
            choice.append(tab);
        }

        let hostField: Field | null = null;
        let identifierField: Field | null = null;
        let secretField: Field | null = null;

        function paint() {
            for (let i = 0; i < tabs.length; i++) {
                tabs[i].style.background = providers[i].id === chosen.id ? '#4a4470' : 'transparent';
            }
            fields.replaceChildren();
            hostField = identifierField = secretField = null;

            if (chosen.identifier_prompt) {
                identifierField = field(chosen.identifier_prompt, '', 'text');
                fields.append(identifierField.el);
            }
            if (chosen.secret_prompt) {
                secretField = field(chosen.secret_prompt, '', 'password');
                fields.append(secretField.el);
            }
            if (chosen.host_prompt) {
                hostField = field(chosen.host_prompt, chosen.host_placeholder, 'text');
                hostField.input.value = localStorage.getItem(REMEMBERED_HOST_PREFIX + chosen.id)
                    ?? chosen.host_default;
                fields.append(hostField.el);
            }
        }

        paint();

        let waited = 0;
        let poll: ReturnType<typeof setInterval> | null = null;

        function stop() {
            if (poll !== null) {
                clearInterval(poll);
                poll = null;
            }
        }

        function land(binding: SignedBinding) {
            stop();
            acceptBinding(binding);
            form.remove();
            resolve(binding);
        }

        // The redirect severs window.opener, so the result is fetched, not told.
        function watch() {
            waited = 0;
            poll = setInterval(async () => {
                waited += POLL_INTERVAL_MS;
                // A restarted node or a dropped connection throws here. Letting
                // it out skips the deadline below, and the ceremony then waits
                // forever on a window that has already been closed.
                let binding: SignedBinding | null = null;
                try {
                    binding = await collectedBinding();
                } catch (error: unknown) {
                    log.warn(SEG.UI, '[Ceremony] could not read the result:', error);
                }
                if (binding) {
                    land(binding);
                    return;
                }
                if (waited >= CEREMONY_TIMEOUT_MS) {
                    stop();
                    go.disabled = false;
                    reject(new Error(`no account was linked within ${CEREMONY_TIMEOUT_MS / 60000} minutes`));
                }
            }, POLL_INTERVAL_MS);
        }

        go.addEventListener('click', async () => {
            go.disabled = true;
            const typedHost = hostField?.input.value.trim() ?? '';
            if (hostField && typedHost) {
                localStorage.setItem(REMEMBERED_HOST_PREFIX + chosen.id, typedHost);
            }

            try {
                // The binding is a claim about this browser's key, so there is
                // nothing to link until laye holds one. Sending empty makes the
                // node answer 400 and reads as the provider having refused.
                if (!await layeWhenReady() || !peerPubkeyHex()) {
                    throw new Error('laye is still starting — the key this link is about does not exist yet');
                }

                say(`Asking ${chosen.label}...`);
                const response = await apiFetch('/auth/binding/start', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        provider: chosen.id,
                        peer_pubkey_hex: peerPubkeyHex(),
                        host: typedHost,
                        identifier: identifierField?.input.value.trim() ?? '',
                        secret: secretField?.input.value ?? '',
                    }),
                });
                if (!response.ok) {
                    const detail = await response.json().catch(() => ({ error: response.statusText }));
                    throw new Error(detail.error ?? `${chosen.label} refused (${response.status})`);
                }

                const body = await response.json() as { authorize_url?: string } & SignedBinding;
                if (body.authorize_url) {
                    window.open(body.authorize_url, '_blank', 'width=520,height=640');
                    say(`Authorize with ${chosen.label} in the window that opened`);
                    watch();
                    return;
                }
                land(body);
            } catch (e) {
                go.disabled = false;
                say(e instanceof Error ? e.message : String(e), true);
            }
        });
    });
}
