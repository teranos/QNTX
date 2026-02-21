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

    const btn = document.createElement('button');
    btn.style.background = '#4a4470';
    btn.style.color = 'var(--text-on-dark)';
    btn.style.border = '1px solid #5c5488';
    btn.style.padding = '0';
    btn.style.borderRadius = '50%';
    btn.style.cursor = 'pointer';
    btn.style.width = '68px';
    btn.style.height = '68px';
    btn.style.flexShrink = '0';
    btn.style.display = 'flex';
    btn.style.alignItems = 'center';
    btn.style.justifyContent = 'center';
    btn.style.transition = 'background 0.15s ease';
    btn.disabled = true;
    btn.innerHTML = `<svg viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13.14 21C10.81 19.54 9.25 16.95 9.25 14c0-1.52 1.23-2.75 2.75-2.75s2.75 1.23 2.75 2.75c0 1.52 1.23 2.75 2.75 2.75s2.75-1.23 2.75-2.75C20.25 9.44 16.55 5.75 12 5.75S3.76 9.44 3.76 14c0 1.02.11 2 .32 2.95M8.49 20.3C7.24 18.51 6.5 16.34 6.5 14c0-3.04 2.46-5.5 5.5-5.5s5.5 2.46 5.5 5.5M17.79 19.48c-.1.01-.2.01-.3.01-3.04 0-5.5-2.46-5.5-5.5M19.67 6.48C17.8 4.35 15.06 3 12 3S6.2 4.35 4.33 6.48"/></svg>`;


    const server = document.createElement('p');
    server.style.fontSize = '11px';
    server.style.color = 'var(--text-on-dark)';
    server.style.margin = '0';
    server.style.padding = '3px 10px';
    server.style.background = 'rgba(0, 0, 0, 0.3)';
    server.style.borderRadius = '10px';
    server.textContent = (window as any).__BACKEND_URL__ || window.location.origin;

    const status = document.createElement('p');
    status.style.fontSize = '12px';
    status.style.color = 'var(--text-secondary)';
    status.style.margin = '0';
    status.style.minHeight = '1.2em';

    container.append(btn, server, status);

    let mode: 'register' | 'login' | 'authenticated' | null = null;

    const fingerprintSvg = btn.innerHTML;

    async function checkStatus() {
        try {
            // If already authenticated, show logout UI
            if (connectivityManager.authenticated) {
                mode = 'authenticated';
                btn.innerHTML = '';
                btn.textContent = 'Log out';
                btn.style.borderRadius = '6px';
                btn.style.width = 'auto';
                btn.style.height = 'auto';
                btn.style.padding = '8px 24px';
                btn.style.fontSize = '13px';
                btn.style.background = '#4a4470';
                btn.style.border = '1px solid #5c5488';
                btn.disabled = false;
                return;
            }

            const res = await apiFetch('/auth/status');
            const data = await res.json();
            if (data.registered) {
                mode = 'login';
            } else {
                mode = 'register';
            }
            btn.disabled = false;
        } catch (e) {
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

    async function logout() {
        btn.disabled = true;
        status.textContent = 'Logging out...';
        status.style.color = 'var(--text-secondary)';
        try {
            await apiFetch('/auth/logout', { method: 'POST' });
            connectivityManager.reportUnauthenticated();
            setTimeout(() => glyphRun.remove(AUTH_GLYPH_ID), 600);
        } catch (e: any) {
            status.textContent = e.message;
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
        else if (mode === 'authenticated') logout();
    });

    btn.addEventListener('mouseenter', () => { btn.style.background = '#564e82'; });
    btn.addEventListener('mouseleave', () => { btn.style.background = '#4a4470'; });

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
        title: 'Auth',
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
