/**
 * Approvals element — what waits on the human (ADR-052).
 *
 * Tim: what the node says becomes a card, with its checks. Spike: a refusal is
 * said under the card, and a check still running keeps the buttons shut.
 * Jenny: Merge is sent to the node in its words; merged, the card settles.
 */

import { describe, test, expect, mock, afterEach, beforeAll } from 'bun:test';

// The node's answers are swapped per test; the defaults are test-setup.ts's own,
// so every other file importing the client sees what it always saw.
const noAnswer = () => Promise.resolve(new Response());
let answer: (path: string, init?: RequestInit) => Promise<Response> = noAnswer;
let listed: unknown = { approvals: [] };
mock.module('./client', () => ({
    connectivity: {
        get state() { return 'online' as const; },
        get authenticated() { return true; },
        subscribe: () => () => {},
        subscribeAuth: () => () => {},
        reportReachable: () => {},
        reportHttpFailure: () => {},
        reportUnauthenticated: () => {},
        reportAuthenticated: () => {},
        setWebSocketConnected: () => {},
    },
    apiFetch: (path: string, init?: RequestInit) => answer(path, init),
    apiJson: () => Promise.resolve(listed),
    backendUrl: () => 'http://localhost',
    backendWsUrl: () => 'ws://localhost',
    backendPath: (path: string) => 'http://localhost' + path,
    sendMessage: () => false,
    connectWebSocket: () => {},
    registerHandler: () => {},
    unregisterHandler: () => {},
}));

const { approvalsRoll, cardOf, minutesAgo } = await import('./approvals-element');
type Approval = Parameters<typeof cardOf>[0];

const realm = () => globalThis.window as unknown as { HTMLCanvasElement: typeof HTMLCanvasElement };

beforeAll(() => {
    // The test DOM draws nothing: a press breaks nothing off and says so quietly.
    realm().HTMLCanvasElement.prototype.getContext = (() => null) as unknown as HTMLCanvasElement['getContext'];
});

afterEach(() => {
    answer = noAnswer;
    listed = { approvals: [] };
    document.body.innerHTML = '';
});

const pr29 = (more: Partial<Approval> = {}): Approval => ({
    subject: 'teranos/elements#29',
    title: 'A panel on a phone reads at a phone’s size',
    link: 'https://github.com/teranos/elements/pull/29',
    repo: 'teranos/elements',
    pull: 29,
    sha: 'abe92a9bcc6cdc2a8308777f6b455fa16ddd44e6',
    base: 'main',
    asked_at: '2026-10-10T15:37:09Z',
    checks: [{ name: 'TypeScript', state: 'done', link: '' }, { name: 'Browser', state: 'done', link: '' }],
    option: '',
    said: '',
    waits: false,
    ...more,
});

const button = (root: HTMLElement, label: string) =>
    Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.querySelector('span')?.textContent === label)!;
const note = (btn: HTMLButtonElement) => btn.querySelector<HTMLElement>('[data-note]')!.textContent;

describe('Tim: what the node says is a card', () => {
    test('the card carries the title, the link, the age and the checks', () => {
        const card = cardOf(pr29(), document.createElement('div'), Date.parse('2026-10-10T15:52:09Z'));
        expect(card.title).toBe('A panel on a phone reads at a phone’s size');
        expect(card.link).toEqual({ label: 'GitHub', href: 'https://github.com/teranos/elements/pull/29' });
        expect(card.arrivedMinutesAgo).toBe(15);
        expect(card.checks).toEqual([{ name: 'TypeScript', state: 'done' }, { name: 'Browser', state: 'done' }]);
        expect(card.options.map((o) => o.label)).toEqual(['Merge', 'Don’t merge']);
        expect(card.merges).toEqual({ label: 'main CI' });
    });

    test('a moment that does not read, or is to come, is now', () => {
        expect(minutesAgo('')).toBe(0);
        expect(minutesAgo('2099-01-01T00:00:00Z')).toBe(0);
    });

    test('the roll draws one card per approval the node lists, and says the head it waits at', async () => {
        listed = { approvals: [pr29(), pr29({ subject: 'teranos/elements#31', title: 'Version 1.14.0', sha: '9ff39ff0', pull: 31 })] };
        const { body, load } = approvalsRoll();
        document.body.appendChild(body);
        await load();
        const titles = Array.from(body.querySelectorAll('[data-title]')).map((t) => t.textContent);
        expect(titles).toContain('A panel on a phone reads at a phone’s size');
        expect(titles).toContain('Version 1.14.0');
        expect(body.textContent).toContain('at abe92a9');
        // Read again, the same cards stay: nothing is drawn twice.
        await load();
        expect(body.querySelectorAll('[data-title]').length).toBe(2);
    });
});

describe('Spike: shut, and refused', () => {
    test('a check still running keeps the buttons shut until the node says it passed', async () => {
        listed = { approvals: [pr29({ checks: [{ name: 'TypeScript', state: 'done', link: '' }, { name: 'Browser', state: 'running', link: '' }] })] };
        const { body, load } = approvalsRoll();
        document.body.appendChild(body);
        await load();
        expect(button(body, 'Merge').disabled).toBe(true);
        expect(note(button(body, 'Merge'))).toBe('checks running');
        listed = { approvals: [pr29()] };
        await load();
        expect(button(body, 'Merge').disabled).toBe(false);
    });

    test('a refusal from the node is said under the card', async () => {
        listed = { approvals: [pr29()] };
        answer = () => Promise.resolve(new Response(JSON.stringify({ error: 'teranos/elements#29 moved: it waits at c0ffee' }), { status: 409 }));
        const { body, load } = approvalsRoll();
        document.body.appendChild(body);
        await load();
        button(body, 'Merge').click();
        await new Promise((r) => setTimeout(r, 10));
        expect(body.querySelector('.element-error')?.textContent).toContain('moved');
    });
});

describe('Jenny: the PR story, through the node', () => {
    test('Merge is sent in the node’s words; waiting is said; force merges and the card settles', async () => {
        listed = { approvals: [pr29()] };
        const sent: { path: string; body: Record<string, string> }[] = [];
        answer = (path, init) => {
            const body = JSON.parse(String(init?.body)) as Record<string, string>;
            sent.push({ path, body });
            const did = body.said === 'force merge' ? { did: 'merged', merge_sha: 'm3rg3d000' } : { did: 'waiting', merge_sha: '' };
            return Promise.resolve(new Response(JSON.stringify({ subject: body.subject, sha: body.sha, option: body.option, said: body.said, ...did }), { status: 200 }));
        };
        const { body, load } = approvalsRoll();
        document.body.appendChild(body);
        await load();

        button(body, 'Merge').click();
        await new Promise((r) => setTimeout(r, 10));
        expect(sent[0]).toEqual({ path: '/api/approvals/answer', body: {
            subject: 'teranos/elements#29', sha: 'abe92a9bcc6cdc2a8308777f6b455fa16ddd44e6', option: 'Merge', said: 'merge when main CI passes',
        } });
        expect(body.textContent).toContain('merges when main CI passes');

        button(body, 'Merge').click();
        await new Promise((r) => setTimeout(r, 10));
        expect(sent[1]!.body.said).toBe('force merge');
        expect(body.textContent).toContain('merged as m3rg3d0');
        expect(button(body, 'Merge').disabled).toBe(true);
    });

    test('gone from the node’s list while a merge waited, the card settles: what stood took effect', async () => {
        listed = { approvals: [pr29()] };
        answer = (_path, init) => {
            const body = JSON.parse(String(init?.body)) as Record<string, string>;
            return Promise.resolve(new Response(JSON.stringify({ ...body, did: 'waiting', merge_sha: '' }), { status: 200 }));
        };
        const { body, load } = approvalsRoll();
        document.body.appendChild(body);
        await load();
        button(body, 'Merge').click();
        await new Promise((r) => setTimeout(r, 10));
        expect(button(body, 'Don’t merge').disabled).toBe(false);
        listed = { approvals: [] };
        await load();
        expect(button(body, 'Merge').disabled).toBe(true);
        expect(note(button(body, 'Merge'))).toBe('merge when main CI passes');
    });
});
