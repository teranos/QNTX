import init, * as laye from '../wasm/laye_p2p.js';
import { log, SEG } from './logger.ts';
import { apiFetch } from './client';

export interface BindingClaim {
    peer_pubkey_hex: string;
    provider: string;
    canonical_id: string;
    handle: string | null;
    issued_at: number;
}

export interface SignedBinding {
    claim: BindingClaim;
    signature_hex: string;
    signer_pubkey_hex: string;
}

const BOOTSTRAP = [
    '/dns4/relaye.sbvh.nl/tcp/443/wss/p2p/12D3KooWC6UBnnmhhv3BAfYKyW1bFBD4GtC5waiEgQWJCb7Hbqaf',
];

let initPromise: Promise<void> | null = null;

export async function initialize(): Promise<void> {
    if (initPromise) {
        return initPromise;
    }

    initPromise = (async () => {
        try {
            await init();
        } catch (error: unknown) {
            const wasmUrl = new URL('laye_p2p_bg.wasm', import.meta.url).href;
            let httpStatus = 'unknown';
            try {
                const response = await fetch(wasmUrl);
                httpStatus = `${response.status} ${response.statusText}`;
            } catch (fetchError: unknown) {
                httpStatus = fetchError instanceof Error ? fetchError.message : 'fetch failed';
            }
            throw new Error([
                'Failed to initialize laye WASM module',
                `  Attempted URL: ${wasmUrl}`,
                `  HTTP Status: ${httpStatus}`,
                `  Original error: ${error instanceof Error ? error.message : String(error)}`,
            ].join('\n'));
        }

        await laye.init(JSON.stringify({
            bootstrap_addrs: BOOTSTRAP,
            topics: [],
            identify_protocol: '/qntx/1.0.0',
            overlay: false,
        }));

        log.info(SEG.WASM, `[laye] ${laye.did()} — ${laye.bindings().length} binding(s)`);
    })();

    return initPromise;
}

/** did:key for this browser. Empty until initialize resolves. */
export function did(): string {
    return laye.did();
}

/** Sign with the key that never leaves this tab. */
export function sign(bytes: Uint8Array): Uint8Array {
    return laye.sign(bytes);
}

/** External identities bound to this key. */
export function bindings(): SignedBinding[] {
    return JSON.parse(laye.bindings());
}

/** Open a provider ceremony. The binding arrives in bindings(). */
export function link(): void {
    laye.link();
}

/** laye runs with overlay off, so its typed errors surface here. */
export function errors(): unknown[] {
    return JSON.parse(laye.errors());
}

function base64url(bytes: Uint8Array): string {
    let binary = '';
    for (const b of bytes) {
        binary += String.fromCharCode(b);
    }
    const padded = btoa(binary).split('+').join('-').split('/').join('_');
    let end = padded.length;
    while (end > 0 && padded[end - 1] === '=') {
        end--;
    }
    return padded.slice(0, end);
}

/**
 * Log into QNTX as the identity laye holds. The server issues a challenge,
 * laye signs it, and the key stays here — the server only ever sees a DID
 * and a signature over something it chose.
 */
export async function login(): Promise<string> {
    await initialize();

    const challengeResponse = await apiFetch('/auth/laye/challenge');
    if (!challengeResponse.ok) {
        throw new Error(`laye login: challenge request returned ${challengeResponse.status} ${challengeResponse.statusText}`);
    }
    const { challenge } = await challengeResponse.json() as { challenge: string };

    const signature = sign(new TextEncoder().encode(challenge));
    if (signature.length === 0) {
        throw new Error(`laye login: signing produced nothing — ${JSON.stringify(errors())}`);
    }

    const verifyResponse = await apiFetch('/auth/laye/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ did: did(), challenge, signature: base64url(signature) }),
    });
    if (!verifyResponse.ok) {
        const detail = await verifyResponse.text();
        throw new Error(`laye login refused (${verifyResponse.status}): ${detail}`);
    }

    const verified = await verifyResponse.json() as { did: string };
    log.info(SEG.WASM, `[laye] logged in as ${verified.did}`);
    return verified.did;
}
