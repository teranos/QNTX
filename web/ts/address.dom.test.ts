/**
 * @jest-environment jsdom
 *
 * Where this tab is, in its address.
 */

import { describe, test, expect, beforeEach, afterEach, mock } from 'bun:test';

const USE_JSDOM = process.env.USE_JSDOM === '1';

// Where the node has this person standing, and every step asked of it.
let standingOnNode = 'default';
let refuse = '';
const steps: string[] = [];
const warned: string[] = [];

mock.module('./client', () => ({
    apiFetch: async (path: string, init?: RequestInit) => {
        if (path === '/i/standing' && init?.method === 'POST') {
            const { namespace } = JSON.parse(String(init.body)) as { namespace: string };
            steps.push(namespace);
            if (namespace === refuse) return new Response('namespace is switched off', { status: 409 });
            standingOnNode = namespace;
        }
        return new Response(JSON.stringify({ namespace: standingOnNode }), { status: 200 });
    },
    connectivity: {
        get state() { return 'online'; },
        subscribe: () => () => {},
        subscribeAuth: () => () => {},
        subscribeFailures: () => () => {},
        reportReachable: () => {},
    },
}));
mock.module('./toast', () => ({
    toast: { warning: (m: string) => { warned.push(m); }, error: () => {}, success: () => {}, info: () => {} },
}));

const { addressed, addressOf, entitle, returnHere } = await import('./address.ts');
const { setStanding } = await import('./standing.ts');

describe('the address', () => {
    test('names a namespace and a canvas', () => {
        expect(addressed('?ns=Clean&canvas=CV-BOB')).toEqual({ ns: 'Clean', canvas: 'CV-BOB' });
        expect(addressed('?ns=Clean')).toEqual({ ns: 'Clean', canvas: '' });
    });

    test('no namespace is no address', () => {
        expect(addressed('')).toBeNull();
        expect(addressed('?canvas=CV-BOB')).toBeNull();
    });

    test('keeps what else it carries', () => {
        expect(addressOf('Clean', 'CV-BOB', 'http://node/?brow')).toBe('http://node/?brow=&ns=Clean&canvas=CV-BOB');
        expect(addressOf('SBVH', '', 'http://node/?ns=Clean&canvas=CV-BOB')).toBe('http://node/?ns=SBVH');
    });
});

describe('the tab\'s name', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('is where it is', () => {
        entitle('Clean');
        expect(document.title).toBe('Clean — QNTX');
        entitle('Clean', 'bob\'s');
        expect(document.title).toBe('Clean · bob\'s — QNTX');
        entitle('');
        expect(document.title).toBe('QNTX');
    });
});

describe('a tab looked at again', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        standingOnNode = 'default';
        refuse = '';
        steps.length = 0;
        warned.length = 0;
        setStanding('default');
    });

    afterEach(() => setStanding(''));

    test('where it already stands, nothing is asked', async () => {
        await returnHere();
        expect(steps).toEqual([]);
    });

    test('another tab stepped elsewhere: it stands where it was built for again', async () => {
        standingOnNode = 'SBVH';
        await returnHere();
        expect(steps).toEqual(['default']);
        expect(standingOnNode).toBe('default');
        expect(warned).toEqual([]);
    });

    test('refused, it says so rather than drawing a namespace its writes do not land in', async () => {
        standingOnNode = 'SBVH';
        refuse = 'default';
        await returnHere();
        expect(steps).toEqual(['default']);
        expect(warned[0]).toContain('SBVH');
    });
});
