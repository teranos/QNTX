/**
 * Auth Glyph — biometric authentication as a window glyph.
 *
 * Added to tray when backend requires authentication (401).
 * Opens as a window. WebAuthn ceremony runs inline — no redirect, no new tab.
 * On success, reports authenticated state and removes itself.
 */

import { apiFetch, backendUrl, connectivity } from '../../client';
import { login as layeLogin, did as layeDID, bindings as layeBindings, whenReady as layeWhenReady, ownerDID as layeOwnerDID, ownerSign as layeOwnerSign, admittedIdentity as layeAdmittedIdentity, refreshAdmittedIdentity as layeRefreshAdmitted, LayeLoginRefused, type LayeAdmission } from '../../laye';
import { fetchProviders, renderCeremony } from '../../ceremony';
import { copyable } from '../../copyable';
import { log, SEG } from '../../logger';
import { glyphRun } from '@qntx/glyphs';
import type { Glyph } from '@qntx/glyphs';

const AUTH_GLYPH_ID = 'auth';

/**
 * Whether a failed login is the node saying this device speaks for no account
 * it lists — the one question the ceremony answers. Exported so the rule has
 * a test rather than living inline where nothing can reach it.
 */
export function needsCeremony(e: unknown): boolean {
    return e instanceof LayeLoginRefused && e.status === 403;
}

function bufferDecode(value: string): ArrayBuffer {
    const s = value.split('-').join('+').split('_').join('/');
    const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4));
    const raw = atob(s + pad);
    const arr = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
    return arr.buffer;
}

/**
 * Domain separator for the PRF. Fixed forever: change it and every enrolled
 * passkey derives a different key and stops being the credential it was.
 */
const PRF_SALT = new TextEncoder().encode('qntx/passkey-owner/v1');

/**
 * The 32 bytes the authenticator derives for this credential, or null when it
 * will not. Most platforms answer `enabled` on create and only evaluate on a
 * subsequent get, which is the second prompt.
 */
async function prfSeed(credential: PublicKeyCredential): Promise<Uint8Array | null> {
    const onCreate = (credential.getClientExtensionResults() as any).prf;
    if (onCreate?.results?.first) {
        return new Uint8Array(onCreate.results.first);
    }
    if (!onCreate?.enabled) {
        return null;
    }

    const asserted = await navigator.credentials.get({
        publicKey: {
            challenge: crypto.getRandomValues(new Uint8Array(32)),
            allowCredentials: [{ id: credential.rawId, type: 'public-key' }],
            extensions: { prf: { eval: { first: PRF_SALT } } } as any,
        },
    }) as PublicKeyCredential | null;
    if (asserted === null) {
        return null;
    }

    const onGet = (asserted.getClientExtensionResults() as any).prf;
    return onGet?.results?.first ? new Uint8Array(onGet.results.first) : null;
}

function bufferEncode(buffer: ArrayBuffer): string {
    const bytes = new Uint8Array(buffer);
    let s = '';
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s).split('+').join('-').split('/').join('_').split('=').join('');
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
    btn.className = 'auth-fingerprint';
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


    const identity = document.createElement('div');
    identity.style.display = 'flex';
    identity.style.flexDirection = 'column';
    identity.style.alignItems = 'center';
    identity.style.gap = '2px';
    identity.style.padding = '4px 10px';
    identity.style.background = 'rgba(0, 0, 0, 0.3)';
    identity.style.borderRadius = '10px';

    const serverLine = document.createElement('span');
    serverLine.style.fontSize = '11px';
    serverLine.style.color = 'var(--text-on-dark)';
    // build.ts stamps this into index.html. Saying which bundle is on screen
    // is otherwise a curl, and a stale branch preview looks identical to a
    // fresh one.
    // The QNTX sha, not the deploy pipeline's — this line is here to say which
    // code is on screen, and the pipeline is a different question.
    const stamp = (window as any).__QNTX_WEB_BUILD__;
    const build = (stamp?.qntx ?? stamp?.commit)?.slice(0, 8) ?? 'unstamped';
    serverLine.textContent = `${backendUrl()}  ·  ${build}`;

    const didLine = document.createElement('span');
    didLine.style.fontSize = '10px';
    didLine.style.color = 'var(--text-on-dark)';
    didLine.style.fontFamily = 'monospace';
    didLine.style.cursor = 'pointer';
    didLine.style.opacity = '0.7';
    didLine.title = 'Click to copy full DID';
    didLine.style.display = 'none';

    let fullDID = '';
    didLine.addEventListener('click', () => {
        if (fullDID) {
            navigator.clipboard.writeText(fullDID);
            const prev = didLine.textContent;
            didLine.textContent = 'copied';
            setTimeout(() => { didLine.textContent = prev; }, 1000);
        }
    });

    identity.append(serverLine, didLine);

    const status = document.createElement('p');
    status.style.fontSize = '12px';
    status.style.color = 'var(--text-secondary)';
    status.style.margin = '0';
    status.style.minHeight = '1.2em';
    status.style.userSelect = 'text';
    status.style.cursor = 'text';
    status.style.wordBreak = 'break-word';
    status.style.overflowWrap = 'break-word';
    status.style.textAlign = 'center';

    copyable(status);
    copyable(serverLine);

    function secondaryButton(label: string): HTMLButtonElement {
        const b = document.createElement('button');
        b.textContent = label;
        b.style.background = 'transparent';
        b.style.color = 'var(--text-on-dark)';
        b.style.border = '1px solid #5c5488';
        b.style.borderRadius = '6px';
        b.style.padding = '6px 14px';
        b.style.fontSize = '12px';
        b.style.cursor = 'pointer';
        b.style.display = 'none';
        return b;
    }

    // The key laye holds is a second credential, not a second account.
    const layeBtn = secondaryButton('Log in');

    // A passkey is the fast way back in as the account you are already signed
    // in as. Enrolling it is the only moment the two can be tied together.
    const enrolBtn = secondaryButton('Add this device as a passkey');

    // Without this the DID and the linked account read as "you are signed in
    // as this", when logged out they mean "this is what you could sign in as".
    const identityCaption = document.createElement('span');
    identityCaption.style.fontSize = '9px';
    identityCaption.style.color = 'var(--text-on-dark)';
    identityCaption.style.opacity = '0.45';
    identityCaption.style.letterSpacing = '0.04em';
    identityCaption.style.display = 'none';

    const layeDidLine = document.createElement('span');
    layeDidLine.style.fontSize = '10px';
    layeDidLine.style.color = 'var(--text-on-dark)';
    layeDidLine.style.fontFamily = 'monospace';
    layeDidLine.style.opacity = '0.7';
    layeDidLine.style.display = 'none';
    copyable(layeDidLine);

    // What the DID is bound to, if anything. A key with no bindings says
    // "some browser" — the link button is how it comes to say more.
    const bindingsLine = document.createElement('span');
    bindingsLine.style.fontSize = '10px';
    bindingsLine.style.color = 'var(--text-on-dark)';
    bindingsLine.style.opacity = '0.7';
    bindingsLine.style.display = 'none';
    copyable(bindingsLine);

    // Where the ceremony draws itself when there is one to run.
    const ceremony = document.createElement('div');
    ceremony.style.width = '100%';
    ceremony.style.display = 'flex';
    ceremony.style.flexDirection = 'column';

    container.append(btn, identity, layeBtn, enrolBtn, identityCaption, layeDidLine, bindingsLine, ceremony, status);

    function say(message: string, bad = false) {
        status.textContent = message;
        status.style.color = bad ? '#e06060' : 'var(--text-secondary)';
    }

    let mode: 'register' | 'login' | 'authenticated' | null = null;

    async function fetchNodeDID() {
        try {
            const res = await apiFetch('/.well-known/did.json');
            const doc = await res.json();
            if (doc.id) {
                fullDID = doc.id;
                const prefix = fullDID.slice(0, 16);
                const suffix = fullDID.slice(-4);
                didLine.textContent = `${prefix}…${suffix}`;
                didLine.style.display = '';
            }
        } catch {
            // Node DID not available — not critical
        }
    }

    async function checkStatus() {
        try {
            // The node knows whether this browser holds a session. connectivity
            // infers it from whether some other request came back 401, so a
            // dead socket used to render Log in with no way to log out.
            const statusRes = await apiFetch('/auth/status');
            const data = statusRes.ok ? await statusRes.json() : {};
            // Only the node. connectivity.authenticated is set by any non-401
            // response, so it means the box answered, not that you are signed
            // in — ORing it in kept you logged in through a logout.
            const signedIn = Boolean(data.identity);

            if (signedIn) {
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
                enrolBtn.style.display = '';
                fetchNodeDID();
                // Being signed in is when who you are signed in as matters
                // most, and it was the one state that showed nothing.
                await layeRefreshAdmitted();
                showLayeIdentity();
                return;
            }

            if (data.registered) {
                mode = 'login';
            } else {
                mode = 'register';
            }
            btn.disabled = false;
            fetchNodeDID();
            showLayeIdentity();
        } catch (e) {
            status.textContent = e instanceof Error ? e.message : String(e);
            status.style.color = '#e06060';
        }
    }

    // laye's wasm is fetched and bootstrapped after the app starts, so the
    // glyph can open before there is an identity to draw. Asking once leaves
    // the login button hidden for the life of the glyph.
    function showLayeIdentity() {
        const identity = layeDID();
        if (!identity) {
            void layeWhenReady().then(available => {
                if (available && layeDID()) {
                    showLayeIdentity();
                }
            });
            return;
        }
        identityCaption.textContent = mode === 'authenticated'
            ? 'signed in as'
            : 'this device can sign in as';
        identityCaption.style.display = '';

        layeDidLine.textContent = `${identity.slice(0, 16)}…${identity.slice(-4)}`;
        layeDidLine.style.display = '';
        // Signed in already: the identity is worth showing, the way in is not.
        // The fingerprint is the way in. This stays for its disabled state.
        layeBtn.style.display = 'none';
        markRoot(layeDidLine, identity === layeAdmittedIdentity());

        renderBindings();
    }

    // A device holds several identities and only one of them opened the door.
    // Yellow says which, so root is seen rather than assumed.
    function markRoot(line: HTMLElement, isRoot: boolean) {
        line.style.background = isRoot ? 'var(--color-warning)' : '';
        line.style.color = isRoot ? '#000' : 'var(--text-on-dark)';
        line.style.padding = isRoot ? '1px 6px' : '';
        line.style.borderRadius = isRoot ? '999px' : '';
        line.title = isRoot ? 'auth.root_identities admitted this session as this identity' : line.title;
    }

    function renderBindings() {
        const held = layeBindings();
        if (held.length === 0) {
            bindingsLine.textContent = 'no linked account';
            bindingsLine.style.display = '';
            markRoot(bindingsLine, false);
            return;
        }
        const admitted = layeAdmittedIdentity();
        bindingsLine.textContent = held
            .map(b => b.claim.handle ?? `${b.claim.provider}:${b.claim.canonical_id}`)
            .join('  ');
        bindingsLine.style.display = '';
        markRoot(bindingsLine, admitted !== '' && held.some(b => b.claim.canonical_id === admitted));
    }

    // One action. Proving the device key is silent; proving it belongs to an
    // account is the ceremony, and that only happens the first time here.
    // completed and failed are one-shot sweeps. The attribute has to come off
    // afterwards or the button stays unpressable wearing a finished animation.
    function fired(state: 'completed' | 'failed') {
        btn.dataset.executionState = state;
        setTimeout(() => { delete btn.dataset.executionState; }, 500);
    }

    async function loginWithLaye() {
        layeBtn.disabled = true;
        // Pressing it starts an admission it does not itself finish, so it
        // reads as fired rather than merely disabled.
        btn.dataset.executionState = 'running';
        status.style.color = 'var(--text-secondary)';
        try {
            status.textContent = 'Signing in...';
            await standOnADevice(await layeLogin());
            return;
        } catch (e) {
            const refused = needsCeremony(e);
            if (!refused) {
                status.textContent = e instanceof Error ? e.message : String(e);
                status.style.color = '#e06060';
                layeBtn.disabled = false;
                fired('failed');
                return;
            }
        }

        status.textContent = 'This device speaks for no account yet — pick a provider';
        startCeremony();
    }

    // laye proved the key in this tab. A root identity stands on a device, so
    // admission is not finished until one answers — enrolling the first, or
    // asserting the one this account already has.
    async function standOnADevice(admission: LayeAdmission) {
        if (admission.next === 'enrol') {
            status.textContent = 'Set up this device as your passkey';
            await register(layeBtn, false);
            return;
        }
        status.textContent = 'Confirm with your passkey';
        await login();
    }

    // The ceremony is this glyph, not a page. The only window that still opens
    // is the provider's own consent screen, which nothing here controls.
    async function startCeremony() {
        try {
            const providers = await fetchProviders();
            if (providers.length === 0) {
                throw new Error('this node offers no identity providers');
            }
            await renderCeremony(ceremony, providers, say);
            renderBindings();
            say('Signing in...');
            await standOnADevice(await layeLogin());
        } catch (e) {
            say(e instanceof Error ? e.message : String(e), true);
            layeBtn.disabled = false;
            fired('failed');
        }
    }

    async function register(trigger: HTMLButtonElement, alreadyIn: boolean) {
        trigger.disabled = true;
        say('Starting registration...');
        try {
            const beginRes = await apiFetch('/auth/register/begin', { method: 'POST' });
            if (!beginRes.ok) throw new Error((await beginRes.json()).error);
            const options = await beginRes.json();

            // The node signs over the challenge as it sent it, so the string
            // is kept before it is decoded for the authenticator.
            const challengeText: string = options.publicKey.challenge;
            options.publicKey.challenge = bufferDecode(options.publicKey.challenge);
            options.publicKey.extensions = {
                ...(options.publicKey.extensions ?? {}),
                prf: { eval: { first: PRF_SALT } },
            };
            options.publicKey.user.id = bufferDecode(options.publicKey.user.id);
            if (options.publicKey.excludeCredentials) {
                options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(
                    (c: any) => ({ ...c, id: bufferDecode(c.id) })
                );
            }

            say('Waiting for biometric...');
            const credential = await navigator.credentials.create(options) as PublicKeyCredential;
            const attestationResponse = credential.response as AuthenticatorAttestationResponse;

            say('Deriving this device’s key...');
            if (!await layeWhenReady()) {
                throw new Error('laye is still starting — the key cannot be derived yet');
            }
            const seed = await prfSeed(credential);
            if (seed === null) {
                throw new Error('this authenticator cannot derive a key, so the passkey would belong to nobody');
            }
            const proofDID = layeOwnerDID(seed);
            const proofSig = layeOwnerSign(seed, new TextEncoder().encode(challengeText));
            if (!proofDID || proofSig.length === 0) {
                throw new Error('the authenticator answered with something that is not a key');
            }

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
                    user_did: proofDID,
                    user_did_signature: bufferEncode(proofSig.buffer as ArrayBuffer),
                }),
            });
            if (!finishRes.ok) throw new Error((await finishRes.json()).error);

            if (alreadyIn) {
                // Nothing about being signed in changed, so the glyph stays.
                say('This device can now sign you in with a fingerprint');
                status.style.color = '#2ecc71';
                trigger.style.display = 'none';
                return;
            }
            onSuccess();
        } catch (e: any) {
            say(e.name === 'NotAllowedError' ? 'Cancelled' : e.message, true);
            trigger.disabled = false;
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

            const challengeText: string = options.publicKey.challenge;
            options.publicKey.challenge = bufferDecode(options.publicKey.challenge);
            if (options.publicKey.allowCredentials) {
                options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(
                    (c: any) => ({ ...c, id: bufferDecode(c.id) })
                );
            }
            // Enrolment recorded which key this credential belongs to, so
            // login has to prove the same one. Asking for the PRF here is what
            // makes checkOwnerMatches answerable rather than always a mismatch.
            options.publicKey.extensions = {
                ...(options.publicKey.extensions ?? {}),
                prf: { eval: { first: PRF_SALT } },
            };

            status.textContent = 'Waiting for biometric...';
            const assertion = await navigator.credentials.get(options) as PublicKeyCredential;
            const assertionResponse = assertion.response as AuthenticatorAssertionResponse;

            if (!await layeWhenReady()) {
                throw new Error('laye is still starting — the owner key cannot be derived yet');
            }
            const asserted = (assertion.getClientExtensionResults() as any).prf?.results?.first;
            const ownerProof = asserted ? new Uint8Array(asserted) : null;

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
                    ...(ownerProof ? {
                        user_did: layeOwnerDID(ownerProof),
                        user_did_signature: bufferEncode(
                            layeOwnerSign(ownerProof, new TextEncoder().encode(challengeText))
                                .buffer as ArrayBuffer
                        ),
                    } : {}),
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
            // Only the node can end a session. Showing logged-out on a proxy
            // error leaves the cookie live and says otherwise.
            const response = await apiFetch('/auth/logout', { method: 'POST' });
            if (!response.ok) {
                throw new Error(`the node answered ${response.status} ${response.statusText}; you are still signed in`);
            }
            connectivity.reportUnauthenticated();
            setTimeout(() => glyphRun.remove(AUTH_GLYPH_ID), 600);
        } catch (e: any) {
            status.textContent = e.message;
            status.style.color = '#e06060';
            btn.disabled = false;
        }
    }

    function onSuccess() {
        fired('completed');
        status.textContent = 'Authenticated';
        status.style.color = '#2ecc71';
        connectivity.reportAuthenticated();
        // Remove from tray after a short pause
        setTimeout(() => glyphRun.remove(AUTH_GLYPH_ID), 600);
    }

    layeBtn.addEventListener('click', () => { loginWithLaye(); });
    enrolBtn.addEventListener('click', () => { register(enrolBtn, true); });

    // One press, one gesture: the fingerprint runs laye and then asks for the
    // finger. Signing in is never the passkey alone, so it is never two.
    btn.addEventListener('click', () => {
        if (mode === 'authenticated') logout();
        else loginWithLaye();
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
        initialWidth: '360px',
        initialHeight: '460px',
        onClose: () => {
            log.debug(SEG.GLYPH, '[AuthGlyph] Closed');
        },
    };

    glyphRun.add(glyph);
    glyphRun.openGlyph(AUTH_GLYPH_ID);
}
