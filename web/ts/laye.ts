import init, * as laye from '../wasm/laye_p2p.js';

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
