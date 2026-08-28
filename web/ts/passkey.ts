/**
 * The passkey: enrolling this device against an account, and asserting it after.
 */

// A root identity stands on a device (ADR-030), so proving the key laye holds
// is half an admission. This is the other half, and it is the same two
// ceremonies wherever they are drawn.

import { apiFetch } from './client';
import { whenReady as layeWhenReady, did as layeDID, ownerDID as layeOwnerDID, ownerSign as layeOwnerSign } from './laye';

// Domain separator for the PRF. Fixed forever: change it and every enrolled
// passkey derives a different key and stops being the credential it was.
const PRF_SALT = new TextEncoder().encode('qntx/passkey-owner/v1');

/** Whoever is drawing this says what is happening. */
export type Say = (message: string, bad?: boolean) => void;

export function bufferDecode(value: string): ArrayBuffer {
    const s = value.split('-').join('+').split('_').join('/');
    const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4));
    const raw = atob(s + pad);
    const arr = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
    return arr.buffer;
}

export function bufferEncode(buffer: ArrayBuffer): string {
    const bytes = new Uint8Array(buffer);
    let s = '';
    for (const b of bytes) s += String.fromCharCode(b);
    return btoa(s).split('+').join('-').split('/').join('_').split('=').join('');
}

/** The 32 bytes the authenticator derives for this credential, or null when it
 *  will not — most platforms only evaluate the PRF on a subsequent get. */
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

/** A cancelled biometric is a person changing their mind, not a failure. */
export function cancelled(e: unknown): boolean {
    return e instanceof Error && e.name === 'NotAllowedError';
}

async function refusal(response: Response): Promise<Error> {
    const detail = await response.json().catch((err: unknown) => ({ error: `${response.statusText} (unreadable body: ${err})` }));
    return new Error(detail.error ?? `the node answered ${response.status} ${response.statusText}`);
}

/** Enrols this device for whoever the node is currently admitting. Throws when
 *  the authenticator will not derive a key: the passkey would belong to nobody. */
export async function enrolPasskey(say: Say): Promise<void> {
    say('Starting registration...');
    const beginRes = await apiFetch('/auth/register/begin', { method: 'POST' });
    if (!beginRes.ok) throw await refusal(beginRes);
    const options = await beginRes.json();

    // The node signs over the challenge as it sent it, so the string is kept
    // before it is decoded for the authenticator.
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
    if (!finishRes.ok) throw await refusal(finishRes);
}

/** Asserts the passkey this device holds, which is what turns a laye admission
 *  into a session. */
export async function assertPasskey(say: Say): Promise<void> {
    await assertTo('/auth/login/begin', '/auth/login/finish', {}, say);
}

/** The same touch, sent somewhere else. Forgetting a device is destructive, so
 *  the node is told which credential to drop by that credential answering. */
export async function forgetPasskey(say: Say): Promise<void> {
    await assertTo('/auth/forget/begin', '/auth/forget', { laye_did: layeDID() }, say);
}

async function assertTo(begin: string, finish: string, also: object, say: Say): Promise<void> {
    say('Starting authentication...');
    const beginRes = await apiFetch(begin, { method: 'POST' });
    if (!beginRes.ok) throw await refusal(beginRes);
    const options = await beginRes.json();

    const challengeText: string = options.publicKey.challenge;
    options.publicKey.challenge = bufferDecode(options.publicKey.challenge);
    if (options.publicKey.allowCredentials) {
        options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(
            (c: any) => ({ ...c, id: bufferDecode(c.id) })
        );
    }
    // Enrolment recorded which key this credential belongs to, so login has to
    // prove the same one. Asking for the PRF here is what makes the node's
    // owner check answerable rather than always a mismatch.
    options.publicKey.extensions = {
        ...(options.publicKey.extensions ?? {}),
        prf: { eval: { first: PRF_SALT } },
    };

    say('Waiting for biometric...');
    const assertion = await navigator.credentials.get(options) as PublicKeyCredential;
    const assertionResponse = assertion.response as AuthenticatorAssertionResponse;

    if (!await layeWhenReady()) {
        throw new Error('laye is still starting — the owner key cannot be derived yet');
    }
    const asserted = (assertion.getClientExtensionResults() as any).prf?.results?.first;
    const ownerProof = asserted ? new Uint8Array(asserted) : null;

    const finishRes = await apiFetch(finish, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            ...also,
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
    if (!finishRes.ok) throw await refusal(finishRes);
}
