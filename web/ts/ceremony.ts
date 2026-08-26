/**
 * The provider ceremony, drawn inside whatever glyph asks for it. Every step
 * needing a secret happens on the node; this builds the form the node
 * describes and waits for the binding to appear.
 */

import { apiFetch } from './client';
import { createButton, type Button } from './components/button';
import { providerMark } from './provider-marks';
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
    wrap.style.color = 'var(--door-text)';
    wrap.textContent = label;

    const input = document.createElement('input');
    input.type = type;
    input.placeholder = placeholder;
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.style.font = 'inherit';
    input.style.fontSize = '12px';
    input.style.padding = '6px 8px';
    input.style.boxSizing = 'border-box';
    input.style.width = '100%';
    input.style.background = 'var(--door-well)';
    input.style.color = 'var(--door-text)';
    input.style.border = '1px solid var(--door-line)';
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
        // The height animation clips against this, so what is arriving is not
        // already drawn outside the box it is arriving into.
        form.style.overflow = 'hidden';

        const choice = document.createElement('div');
        choice.style.display = 'flex';
        choice.style.gap = '6px';
        choice.style.flexWrap = 'wrap';
        choice.style.justifyContent = 'center';

        const fields = document.createElement('div');
        fields.className = 'door-fields';
        fields.style.display = 'flex';
        fields.style.flexDirection = 'column';
        fields.style.gap = '6px';

        const carry = createButton({
            label: 'Continue',
            onClick: () => spend(),
            variant: 'ghost',
            className: 'door-continue',
        });
        const go = carry.element;

        form.append(choice, fields, go);
        host.append(form);

        // Nobody has chosen anything yet, so nothing is asked for yet. Landing
        // on the first provider's fields presents a decision that was never
        // made as one that was.
        let chosen: ProviderDescription | null = null;
        const tabs: Button[] = [];

        for (const provider of providers) {
            const mark = providerMark(provider.id);
            const tab = createButton({
                label: provider.label,
                onClick: () => { chosen = provider; paint(); },
                variant: 'ghost',
                className: 'door-provider',
                mark,
                // A provider nobody has drawn a mark for still has to be
                // pressable, so it falls back to wearing its name.
                markOnly: Boolean(mark),
            });
            tabs.push(tab);
            choice.append(tab.element);
        }

        let hostField: Field | null = null;
        let identifierField: Field | null = null;
        let secretField: Field | null = null;

        // What is asked for changes size when the choice changes. Measuring
        // either side of the swap lets the form travel between the two instead
        // of the rest of the door jumping to keep up.
        function paint() {
            const before = form.getBoundingClientRect().height;
            fill();
            const after = form.getBoundingClientRect().height;
            if (!before || !after || before === after) return;
            if (typeof form.animate !== 'function') return;
            form.animate(
                [{ height: `${before}px` }, { height: `${after}px` }],
                { duration: 190, easing: 'ease-out' },
            );
        }

        function fill() {
            for (let i = 0; i < tabs.length; i++) {
                tabs[i].element.classList.toggle('door-provider-picked', providers[i].id === chosen?.id);
            }
            fields.replaceChildren();
            hostField = identifierField = secretField = null;

            // Until a provider is picked there is nothing to fill in and
            // nothing to continue to.
            go.style.display = chosen ? '' : 'none';
            if (!chosen) return;

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
                    carry.setDisabled(false);
                    reject(new Error(`no account was linked within ${CEREMONY_TIMEOUT_MS / 60000} minutes`));
                }
            }, POLL_INTERVAL_MS);
        }

        // Throwing from here is how the Button shows what went wrong, so this
        // reports by throwing rather than by writing to the status line.
        async function spend(): Promise<void> {
            // Hidden until something is picked, so this is unreachable — but the
            // handler is what spends the credential, and it names who it is
            // spending it on rather than trusting that.
            const picked = chosen;
            if (!picked) return;

            const typedHost = hostField?.input.value.trim() ?? '';
            if (hostField && typedHost) {
                localStorage.setItem(REMEMBERED_HOST_PREFIX + picked.id, typedHost);
            }

            // The binding is a claim about this browser's key, so there is
            // nothing to link until laye holds one. Sending empty makes the
            // node answer 400 and reads as the provider having refused.
            if (!await layeWhenReady() || !peerPubkeyHex()) {
                throw new Error('laye is still starting — the key this link is about does not exist yet');
            }

            say(`Asking ${picked.label}...`);
            const response = await apiFetch('/auth/binding/start', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    provider: picked.id,
                    peer_pubkey_hex: peerPubkeyHex(),
                    host: typedHost,
                    identifier: identifierField?.input.value.trim() ?? '',
                    secret: secretField?.input.value ?? '',
                }),
            });
            if (!response.ok) {
                const detail = await response.json().catch(() => ({ error: response.statusText }));
                throw new Error(detail.error ?? `${picked.label} refused (${response.status})`);
            }

            const body = await response.json() as { authorize_url?: string } & SignedBinding;
            if (body.authorize_url) {
                window.open(body.authorize_url, '_blank', 'width=520,height=640');
                say(`Authorize with ${picked.label} in the window that opened`);
                watch();
                return;
            }
            land(body);
        }
    });
}
