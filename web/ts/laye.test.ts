import { describe, expect, test, mock, beforeEach } from 'bun:test';

/**
 * The frontend is served from a CDN and the backend lives on another host, so
 * a relative URL reaches the SPA fallback and returns index.html. Parsing that
 * as JSON is the error the deploy produced.
 */

const apiFetchCalls: string[] = [];

mock.module('./client', () => ({
    apiFetch: (path: string) => {
        apiFetchCalls.push(path);
        return Promise.resolve(new Response(JSON.stringify({ challenge: 'c', did: 'did:key:z1' }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
        }));
    },
}));

mock.module('./logger.ts', () => ({
    log: { info: () => {}, error: () => {}, warn: () => {}, debug: () => {} },
    SEG: { WASM: 'wasm' },
}));

mock.module('../wasm/laye_p2p.js', () => ({
    default: () => Promise.resolve(),
    init: () => Promise.resolve(),
    did: () => 'did:key:z1',
    sign: () => new Uint8Array([1, 2, 3]),
    bindings: () => '[]',
    errors: () => '[]',
}));

describe('laye login reaches the backend, not the CDN', () => {
    beforeEach(() => {
        apiFetchCalls.length = 0;
    });

    test('every auth call goes through apiFetch', async () => {
        const { login } = await import('./laye.ts');
        await login();

        expect(apiFetchCalls).toContain('/auth/laye/challenge');
        expect(apiFetchCalls).toContain('/auth/laye/verify');
    });

    test('no auth call is an absolute or protocol-relative URL', async () => {
        const { login } = await import('./laye.ts');
        await login();

        for (const path of apiFetchCalls) {
            expect(path.startsWith('/')).toBe(true);
            expect(path.startsWith('//')).toBe(false);
            expect(path.includes('://')).toBe(false);
        }
    });
});
