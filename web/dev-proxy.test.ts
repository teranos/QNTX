import { describe, test, expect } from 'bun:test';
import { resolveBackend, resolveCredential, backendHeaders, dropSetCookie, backendWsUrl, isBackendPath } from './dev-proxy';

describe('resolveBackend', () => {
    test('a port on this machine is still the default', () => {
        const b = resolveBackend({}, 8770);
        expect(b.url).toBe('http://localhost:8770');
        expect(b.isRemote).toBe(false);
    });

    test('QNTX_BACKEND names a whole backend', () => {
        const b = resolveBackend({ QNTX_BACKEND: 'https://q.sbvh.nl' }, 8770);
        expect(b.url).toBe('https://q.sbvh.nl');
        expect(b.isRemote).toBe(true);
    });

    test('a loopback QNTX_BACKEND is not remote', () => {
        expect(resolveBackend({ QNTX_BACKEND: 'http://127.0.0.1:9000' }, 8770).isRemote).toBe(false);
    });

    test('a trailing slash would double the one in the path', () => {
        expect(resolveBackend({ QNTX_BACKEND: 'https://q.sbvh.nl/' }, 8770).url).toBe('https://q.sbvh.nl');
    });
});

describe('backendHeaders', () => {
    test('the node is told the origin it allows, not the dev server', () => {
        const h = backendHeaders(new Headers({ Origin: 'http://localhost:8820' }), 'https://q.sbvh.nl', {});
        expect(h.get('Origin')).toBe('https://q.sbvh.nl');
    });

    test('a token is what the relay presents', () => {
        const h = backendHeaders(new Headers(), 'https://q.sbvh.nl', { token: 'tok_7' });
        expect(h.get('Authorization')).toBe('Bearer tok_7');
        expect(h.get('Cookie')).toBeNull();
    });

    test('a session works where no token was minted', () => {
        const h = backendHeaders(new Headers(), 'https://q.sbvh.nl', { session: 'abc123' });
        expect(h.get('Cookie')).toBe('qntx_session=abc123');
        expect(h.get('Authorization')).toBeNull();
    });

    test('a token outranks a session, because it says whose it is', () => {
        const h = backendHeaders(new Headers(), 'https://q.sbvh.nl', { token: 'tok_7', session: 'abc123' });
        expect(h.get('Authorization')).toBe('Bearer tok_7');
        expect(h.get('Cookie')).toBeNull();
    });

    test('no credential means none invented', () => {
        const h = backendHeaders(new Headers(), 'http://localhost:8770', {});
        expect(h.get('Cookie')).toBeNull();
        expect(h.get('Authorization')).toBeNull();
    });

    test('what the browser sent never reaches the node as a credential', () => {
        const incoming = new Headers({ Cookie: 'qntx_session=stale', Authorization: 'Bearer stale' });
        const h = backendHeaders(incoming, 'https://q.sbvh.nl', { token: 'fresh' });
        expect(h.get('Authorization')).toBe('Bearer fresh');
        expect(h.get('Cookie')).toBeNull();
    });

    test('a browser credential is stripped even when the relay has none', () => {
        const incoming = new Headers({ Cookie: 'qntx_session=stale' });
        expect(backendHeaders(incoming, 'https://q.sbvh.nl', {}).get('Cookie')).toBeNull();
    });

    test('Host belongs to the hop, not the message', () => {
        const h = backendHeaders(new Headers({ Host: 'localhost:8820' }), 'https://q.sbvh.nl', {});
        expect(h.get('Host')).toBeNull();
    });
});

describe('resolveCredential', () => {
    test('QNTX_TOKEN is the token', () => {
        expect(resolveCredential({ QNTX_TOKEN: 'tok_7' })).toEqual({ token: 'tok_7' });
    });

    test('QNTX_SESSION is the session', () => {
        expect(resolveCredential({ QNTX_SESSION: 'abc' })).toEqual({ session: 'abc' });
    });

    test('neither is empty, not a guess', () => {
        expect(resolveCredential({})).toEqual({});
    });
});

describe('dropSetCookie', () => {
    test('the browser is never handed the node’s cookie', () => {
        const r = new Response('ok', { headers: { 'Set-Cookie': 'qntx_session=x; Secure; HttpOnly' } });
        expect(dropSetCookie(r).headers.getSetCookie()).toEqual([]);
    });

    test('everything else survives', () => {
        const r = new Response('ok', {
            status: 201,
            headers: { 'Set-Cookie': 'qntx_session=x; Secure', 'Content-Type': 'application/json' },
        });
        const out = dropSetCookie(r);
        expect(out.status).toBe(201);
        expect(out.headers.get('Content-Type')).toBe('application/json');
    });
});

describe('isBackendPath', () => {
    test('the API and its sockets', () => {
        expect(isBackendPath('/api/version')).toBe(true);
        expect(isBackendPath('/ws')).toBe(true);
        expect(isBackendPath('/ws/llm')).toBe(true);
        expect(isBackendPath('/lsp')).toBe(true);
    });

    test('auth is the node’s, or the page reads as logged out', () => {
        expect(isBackendPath('/auth/status')).toBe(true);
        expect(isBackendPath('/auth/login/begin')).toBe(true);
        expect(isBackendPath('/auth/user/arrival')).toBe(true);
    });

    test('the routes a node answers before anyone has logged in', () => {
        expect(isBackendPath('/setup')).toBe(true);
        expect(isBackendPath('/setup/claim')).toBe(true);
        expect(isBackendPath('/health')).toBe(true);
        expect(isBackendPath('/.well-known/did.json')).toBe(true);
        expect(isBackendPath('/logs/download')).toBe(true);
    });

    test('the bundle is this server’s, and is what is being tested', () => {
        expect(isBackendPath('/')).toBe(false);
        expect(isBackendPath('/js/main.js')).toBe(false);
        expect(isBackendPath('/css/tokens.css')).toBe(false);
        expect(isBackendPath('/qntx.jpg')).toBe(false);
        expect(isBackendPath('/__dev_reload__')).toBe(false);
    });

    test('a prefix is a segment, not a substring', () => {
        expect(isBackendPath('/authors')).toBe(false);
        expect(isBackendPath('/apifoo')).toBe(false);
        expect(isBackendPath('/healthz')).toBe(false);
    });
});

describe('backendWsUrl', () => {
    test('https carries the socket too', () => {
        expect(backendWsUrl('https://q.sbvh.nl', '/ws')).toBe('wss://q.sbvh.nl/ws');
    });

    test('plain http stays plain', () => {
        expect(backendWsUrl('http://localhost:8770', '/ws/llm')).toBe('ws://localhost:8770/ws/llm');
    });

    test('the query rides along', () => {
        expect(backendWsUrl('https://q.sbvh.nl', '/ws?id=7')).toBe('wss://q.sbvh.nl/ws?id=7');
    });
});
