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
let ready = false;

/**
 * Whose signature on a binding counts, from auth.binding_signers. laye's own
 * verify() reads the signing key out of the message, so without this list any
 * peer signs "I am @someone" and every receiver believes it.
 */
async function trustedSigners(): Promise<string[]> {
    try {
        const response = await apiFetch('/auth/status');
        if (!response.ok) {
            log.warn(SEG.WASM, `[laye] /auth/status returned ${response.status} — trusting no binding signer`);
            return [];
        }
        const { binding_signers } = await response.json() as { binding_signers?: string[] };
        return binding_signers ?? [];
    } catch (error: unknown) {
        log.warn(SEG.WASM, '[laye] could not read auth.binding_signers — trusting none:', error);
        return [];
    }
}

/**
 * Whether laye can be called. Every accessor below reads the wasm module
 * directly, and wasm-bindgen throws rather than returning empty when the
 * module has not been instantiated yet.
 */
export function isReady(): boolean {
    return ready;
}

/**
 * Resolves once init has finished, successfully or not. A caller that renders
 * from laye needs somewhere to be told, or it renders once against a module
 * that was not there yet and never looks again.
 */
export async function whenReady(): Promise<boolean> {
    try {
        // Starting it here rather than returning false removes the ordering
        // dependency on whoever else calls initialize.
        await initialize();
    } catch {
        return false;
    }
    return ready;
}

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
            binding_signers: await trustedSigners(),
        }));

        ready = true;
        log.info(SEG.WASM, `[laye] ${laye.did()} — ${laye.bindings().length} binding(s)`);
    })();

    return initPromise;
}

/** did:key for this browser. Empty until initialize resolves. */
export function did(): string {
    return ready ? laye.did() : '';
}

/** Sign with the key that never leaves this tab. */
export function sign(bytes: Uint8Array): Uint8Array {
    return laye.sign(bytes);
}

/** External identities bound to this key. */
export function bindings(): SignedBinding[] {
    return ready ? JSON.parse(laye.bindings()) : [];
}

/**
 * The key a binding is about, as hex. This is what the node signs over, so the
 * ceremony names it and the result is looked up by it.
 */
export function peerPubkeyHex(): string {
    return ready ? laye.self_peer_id() : '';
}

/** Take a binding this node signed and keep it, as if the ceremony handed it over. */
export function acceptBinding(binding: SignedBinding): void {
    laye.accept_binding(JSON.stringify(binding));
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
/**
 * What this node signed for the ceremony this browser started, whether or not
 * the popup managed to hand it back. A cross-origin OAuth redirect severs
 * window.opener, so the tab collects the result rather than being told.
 */
export async function collectedBinding(): Promise<SignedBinding | null> {
    // The ceremony cookie says which ceremony, so nothing is named in the URL
    // and a binding is collected once — the node forgets it on read.
    const response = await apiFetch('/auth/binding/result');
    if (!response.ok) {
        return null;
    }
    return await response.json() as SignedBinding;
}

export async function login(): Promise<string> {
    await initialize();

    const challengeResponse = await apiFetch('/auth/laye/challenge');
    if (!challengeResponse.ok) {
        throw new Error(`laye login: challenge request returned ${challengeResponse.status} ${challengeResponse.statusText}`);
    }
    const { challenge } = await challengeResponse.json() as { challenge: string };

    // laye's own copy, plus whatever the ceremony left with this node.
    const held = bindings();
    const collected = await collectedBinding();
    if (collected && !held.some(b => b.claim.canonical_id === collected.claim.canonical_id)) {
        held.push(collected);
        // laye persists what it holds to IndexedDB, so handing it over is
        // what stops the next restart costing another ceremony.
        acceptBinding(collected);
    }

    const signature = sign(new TextEncoder().encode(challenge));
    if (signature.length === 0) {
        throw new Error(`laye login: signing produced nothing — ${JSON.stringify(errors())}`);
    }

    const verifyResponse = await apiFetch('/auth/laye/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // The bindings ride along: the server decides which signer it trusts,
        // so presenting them is not the same as being believed.
        body: JSON.stringify({ did: did(), challenge, signature: base64url(signature), bindings: held }),
    });
    if (!verifyResponse.ok) {
        const detail = await verifyResponse.text();
        throw new Error(`laye login refused (${verifyResponse.status}): ${detail}`);
    }

    const verified = await verifyResponse.json() as { did: string };
    log.info(SEG.WASM, `[laye] logged in as ${verified.did}`);
    return verified.did;
}
