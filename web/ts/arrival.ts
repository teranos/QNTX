/**
 * The arrival, drawn inside whatever glyph asks for it.
 */

// A User minted by an admission knows every route that reaches it and nothing
// this person chose. Every User has a username and an email, so this is the one
// step of getting in that nobody skips.

import { apiFetch } from './client';

export interface Arrival {
    arrived: boolean;
    username?: string;
}

/** Whether the signed-in User still has to say who they are. */
export async function arrivalStatus(): Promise<Arrival> {
    const response = await apiFetch('/auth/user/arrival');
    if (!response.ok) {
        throw new Error(`this node did not say whether you have arrived (${response.status} ${response.statusText})`);
    }
    return await response.json() as Arrival;
}

function field(label: string, type: string): { el: HTMLElement; input: HTMLInputElement } {
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

/** Resolves with the username once the node records it, and never without one. */
export function renderArrival(
    host: HTMLElement,
    say: (message: string, bad?: boolean) => void,
): Promise<string> {
    return new Promise((resolve) => {
        const form = document.createElement('div');
        form.style.display = 'flex';
        form.style.flexDirection = 'column';
        form.style.gap = '8px';
        form.style.width = '100%';

        const username = field('Username', 'text');
        const email = field('Email', 'email');

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

        form.append(username.el, email.el, go);
        host.append(form);
        username.input.focus();

        async function submit() {
            go.disabled = true;
            try {
                const response = await apiFetch('/auth/user/arrive', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        username: username.input.value.trim(),
                        email: email.input.value.trim(),
                    }),
                });
                if (!response.ok) {
                    const detail = await response.json().catch(() => ({ error: response.statusText }));
                    throw new Error(detail.error ?? `this node would not record it (${response.status})`);
                }

                const arrived = await response.json() as Arrival;
                form.remove();
                resolve(arrived.username ?? username.input.value.trim());
            } catch (e) {
                go.disabled = false;
                say(e instanceof Error ? e.message : String(e), true);
            }
        }

        go.addEventListener('click', () => { void submit(); });
        for (const input of [username.input, email.input]) {
            input.addEventListener('keydown', event => {
                if (event.key === 'Enter') {
                    event.preventDefault();
                    void submit();
                }
            });
        }
    });
}
