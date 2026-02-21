/**
 * Auth Glyph — biometric authentication as a window glyph.
 *
 * Added to tray when backend requires authentication (401).
 * Opens as a window. WebAuthn ceremony runs inline — no redirect, no new tab.
 * On success, reports authenticated state and removes itself.
 */

import { apiFetch } from '../../api';
import { connectivityManager } from '../../connectivity';
import { log, SEG } from '../../logger';
import { glyphRun } from './run';
import type { Glyph } from './glyph';

const AUTH_GLYPH_ID = 'auth';

function bufferDecode(value: string): ArrayBuffer {
    const s = value.replace(/-/g, '+').replace(/_/g, '/');
    const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4));
    const raw = atob(s + pad);
    const arr = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
    return arr.buffer;
}

function bufferEncode(buffer: ArrayBuffer): string {
    const bytes = new Uint8Array(buffer);
    let s = '';
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

function renderAuthContent(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'glyph-content';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.alignItems = 'center';
    container.style.gap = '16px';
    container.style.padding = '16px';
    container.style.fontFamily = '-apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif';

    const subtitle = document.createElement('p');
    subtitle.style.color = 'var(--text-secondary)';
    subtitle.style.fontSize = '13px';
    subtitle.style.margin = '0';
    subtitle.textContent = 'Checking...';

    const btn = document.createElement('button');
    btn.style.background = '#3a3a5c';
    btn.style.color = 'var(--text-on-dark)';
    btn.style.border = '1px solid #4a4a6a';
    btn.style.padding = '8px 24px';
    btn.style.fontSize = '13px';
    btn.style.borderRadius = '6px';
    btn.style.cursor = 'pointer';
    btn.disabled = true;
    btn.textContent = 'Authenticate';

    const status = document.createElement('p');
    status.style.fontSize = '12px';
    status.style.color = 'var(--text-secondary)';
    status.style.margin = '0';
    status.style.minHeight = '1.2em';

    container.append(subtitle, btn, status);

    let mode: 'register' | 'login' | null = null;

    async function checkStatus() {
        try {
            const res = await apiFetch('/auth/status');
            const data = await res.json();
            if (data.registered) {
                mode = 'login';
                subtitle.textContent = 'Biometric authentication required';
                btn.textContent = 'Authenticate';
            } else {
                mode = 'register';
                subtitle.textContent = 'Register your biometric credential';
                btn.textContent = 'Register';
            }
            btn.disabled = false;
        } catch (e) {
            subtitle.textContent = 'Cannot reach server';
            status.textContent = e instanceof Error ? e.message : String(e);
            status.style.color = '#e06060';
        }
    }

    async function register() {
        btn.disabled = true;
        status.textContent = 'Starting registration...';
        status.style.color = 'var(--text-secondary)';
        try {
            const beginRes = await apiFetch('/auth/register/begin', { method: 'POST' });
            if (!beginRes.ok) throw new Error((await beginRes.json()).error);
            const options = await beginRes.json();

            options.publicKey.challenge = bufferDecode(options.publicKey.challenge);
            options.publicKey.user.id = bufferDecode(options.publicKey.user.id);
            if (options.publicKey.excludeCredentials) {
                options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(
                    (c: any) => ({ ...c, id: bufferDecode(c.id) })
                );
            }

            status.textContent = 'Waiting for biometric...';
            const credential = await navigator.credentials.create(options) as PublicKeyCredential;
            const attestationResponse = credential.response as AuthenticatorAttestationResponse;

            const finishRes = await apiFetch('/auth/register/finish', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    id: credential.id,
                    rawId: bufferEncode(credential.rawId),
                    type: credential.type,
                    response: {
                        attestationObject: bufferEncode(attestationResponse.attestationObject),
                        clientDataJSON: bufferEncode(attestationResponse.clientDataJSON),
                    },
                }),
            });
            if (!finishRes.ok) throw new Error((await finishRes.json()).error);

            onSuccess();
        } catch (e: any) {
            status.textContent = e.name === 'NotAllowedError' ? 'Cancelled' : e.message;
            status.style.color = '#e06060';
            btn.disabled = false;
        }
    }

    async function login() {
        btn.disabled = true;
        status.textContent = 'Starting authentication...';
        status.style.color = 'var(--text-secondary)';
        try {
            const beginRes = await apiFetch('/auth/login/begin', { method: 'POST' });
            if (!beginRes.ok) throw new Error((await beginRes.json()).error);
            const options = await beginRes.json();

            options.publicKey.challenge = bufferDecode(options.publicKey.challenge);
            if (options.publicKey.allowCredentials) {
                options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(
                    (c: any) => ({ ...c, id: bufferDecode(c.id) })
                );
            }

            status.textContent = 'Waiting for biometric...';
            const assertion = await navigator.credentials.get(options) as PublicKeyCredential;
            const assertionResponse = assertion.response as AuthenticatorAssertionResponse;

            const finishRes = await apiFetch('/auth/login/finish', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    id: assertion.id,
                    rawId: bufferEncode(assertion.rawId),
                    type: assertion.type,
                    response: {
                        authenticatorData: bufferEncode(assertionResponse.authenticatorData),
                        clientDataJSON: bufferEncode(assertionResponse.clientDataJSON),
                        signature: bufferEncode(assertionResponse.signature),
                        userHandle: assertionResponse.userHandle
                            ? bufferEncode(assertionResponse.userHandle) : '',
                    },
                }),
            });
            if (!finishRes.ok) throw new Error((await finishRes.json()).error);

            onSuccess();
        } catch (e: any) {
            status.textContent = e.name === 'NotAllowedError' ? 'Cancelled' : e.message;
            status.style.color = '#e06060';
            btn.disabled = false;
        }
    }

    function onSuccess() {
        status.textContent = 'Authenticated';
        status.style.color = '#2ecc71';
        connectivityManager.reportAuthenticated();
        // Remove from tray after a short pause
        setTimeout(() => glyphRun.remove(AUTH_GLYPH_ID), 600);
    }

    btn.addEventListener('click', () => {
        if (mode === 'register') register();
        else if (mode === 'login') login();
    });

    btn.addEventListener('mouseenter', () => { btn.style.background = '#4a4a6a'; });
    btn.addEventListener('mouseleave', () => { btn.style.background = '#3a3a5c'; });

    checkStatus();
    return container;
}

/**
 * Add the auth glyph to the tray. No-op if already present.
 */
export function spawnAuthGlyph(): void {
    if (glyphRun.has(AUTH_GLYPH_ID)) {
        glyphRun.openGlyph(AUTH_GLYPH_ID);
        return;
    }

    const glyph: Glyph = {
        id: AUTH_GLYPH_ID,
        title: 'Authenticate',
        renderContent: renderAuthContent,
        initialWidth: '280px',
        initialHeight: '160px',
        onClose: () => {
            log.debug(SEG.GLYPH, '[AuthGlyph] Closed');
        },
    };

    glyphRun.add(glyph);
    glyphRun.openGlyph(AUTH_GLYPH_ID);
}
