/**
 * Plugin panel — adding a plugin by its repository URL, configuring it while it
 * does not run, and enabling it (ADR-043).
 */

// "I log in to QNTX, open the plugin element, press +, enter a repository URL and confirm. The plugin starts disabled; I edit its config and enable it, and I don't think about it anymore."
// "i expect it to be a bigger + button as part of the list, like an empty plugin ready to become something."
// "it should have been two stage, first stage is check if its even possible, and 2nd is to commit to adding it for real"

import { describe, test, expect, mock, beforeEach, afterEach } from 'bun:test';

// The node, as the panel asks it. The defaults are test-setup.ts's own, so every
// other file importing the client sees what it always saw.
type Answer = (path: string, init?: RequestInit) => Promise<Response>;
const noAnswer: Answer = () => Promise.resolve(new Response());
let answer: Answer = noAnswer;
let answerJson: (path: string) => Promise<unknown> = () => Promise.resolve({});
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
    apiJson: (path: string) => answerJson(path),
    backendUrl: () => 'http://localhost',
    backendWsUrl: () => 'ws://localhost',
    backendPath: (path: string) => 'http://localhost' + path,
    sendMessage: () => false,
    connectWebSocket: () => {},
    registerHandler: () => {},
    unregisterHandler: () => {},
}));

const { createPluginElement } = await import('./plugin-panel');

const REPO = 'https://github.com/teranos/garden';

interface Row { name: string; version: string; description: string; healthy: boolean; probed?: boolean; state: string; pausable: boolean; repo?: string; enabled?: boolean; message?: string }

// What the node holds, and every request the panel made of it.
let held: Row[];
let asked: { path: string; method: string; body: unknown }[];

// What the list answers besides its rows, when a test says so.
let listed: () => Promise<unknown> = () => Promise.resolve({ plugins: held });

function node(extra: Record<string, (body: unknown) => Response> = {}): void {
    listed = () => Promise.resolve({ plugins: held });
    answerJson = (path: string) => {
        if (path === '/api/plugins') return listed();
        if (path === '/health') return Promise.resolve({ status: 'ok', version: 'test', commit: '', build_time: '', clients: 0, verbosity: 0, owner: '' });
        return Promise.resolve({});
    };
    answer = (path: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET';
        const body = init?.body ? JSON.parse(String(init.body)) : null;
        asked.push({ path, method, body });
        const key = `${method} ${path}`;
        if (extra[key]) return Promise.resolve(extra[key](body));
        return Promise.resolve(new Response('{}', { status: 200 }));
    };
}

function added(body: unknown): Response {
    const repo = (body as { repo: string }).repo;
    held.push({ name: 'garden', version: '', description: '', healthy: false, state: 'disabled', pausable: false, repo, enabled: false });
    return new Response(JSON.stringify({ name: 'garden', repo, enabled: false, config: {} }), { status: 200 });
}

function resolves(body: unknown): Response {
    const repo = (body as { repo: string }).repo;
    return new Response(JSON.stringify({ name: 'garden', repo, release: 'garden-v1.0.0', asset: 'qntx-garden-plugin-1.0.0-linux-amd64.tar.gz' }), { status: 200 });
}

const flush = async () => {
    for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0));
};

async function openPanel(): Promise<HTMLElement> {
    const content = createPluginElement().renderContent!() as HTMLElement;
    document.body.appendChild(content);
    await flush();
    return content;
}

function press(content: HTMLElement, selector: string): void {
    const button = content.querySelector<HTMLElement>(selector);
    if (!button) throw new Error(`nothing to press at ${selector}`);
    button.click();
}

// The empty card opens into the field, and a field left open by an earlier panel stays open.
async function openAddField(content: HTMLElement): Promise<void> {
    if (content.querySelector('.plugin-add-repo')) return;
    press(content, '.plugin-add-card-closed');
    await flush();
}

function type(content: HTMLElement, selector: string, value: string): void {
    const input = content.querySelector<HTMLInputElement>(selector);
    if (!input) throw new Error(`nothing to type in at ${selector}`);
    input.value = value;
    input.dispatchEvent(new window.Event('input', { bubbles: true }));
}

beforeEach(() => {
    held = [];
    asked = [];
    // The expanded card streams its log; there is no node to stream from here.
    (globalThis as Record<string, unknown>).EventSource = class { close() {} onmessage = null; onerror = null; };
});

afterEach(() => {
    answer = noAnswer;
    answerJson = () => Promise.resolve({});
    // A panel out of the document stops its own refresh.
    document.body.innerHTML = '';
});

describe('Tim: the empty card becomes a plugin, and it starts disabled', () => {
    test('press the empty card, enter the repository URL, Check, then Add: the list shows it disabled', async () => {
        node({ 'POST /api/plugins/check': resolves, 'POST /api/plugins': added });
        const content = await openPanel();
        expect(content.querySelector('.plugin-list .plugin-add-card-closed')).not.toBeNull();

        await openAddField(content);
        type(content, '.plugin-add-repo', REPO);
        // Stage one: nothing is added until the repo resolves.
        expect(content.querySelector('.plugin-add-confirm')).toBeNull();
        press(content, '.plugin-add-check');
        await flush();

        expect(asked.find(a => a.path === '/api/plugins/check')?.body).toEqual({ repo: REPO });
        expect(asked.find(a => a.method === 'POST' && a.path === '/api/plugins')).toBeUndefined();
        expect(content.querySelector('.plugin-add-resolved')?.textContent).toContain('garden-v1.0.0');

        // Stage two: added for real.
        press(content, '.plugin-add-confirm');
        await flush();

        expect(asked.find(a => a.method === 'POST' && a.path === '/api/plugins')?.body).toEqual({ repo: REPO });
        const card = content.querySelector<HTMLElement>('.plugin-card[data-plugin="garden"]');
        expect(card).not.toBeNull();
        expect(card!.querySelector('.plugin-state-text')?.textContent).toBe('disabled');
        expect(card!.querySelector('.plugin-repo')?.getAttribute('href')).toBe(REPO);
        expect(card!.querySelector('.plugin-enable-btn')).not.toBeNull();
        expect(card!.querySelector('.plugin-disable-btn')).toBeNull();
        // Not running, so no health is claimed for it.
        expect(card!.querySelector('.plugin-status')).toBeNull();
        expect(content.querySelector('.plugin-add-repo')).toBeNull();
    });
});

describe('Spike: the node refuses', () => {
    test('a repo that does not resolve is refused in the node\'s words, beside Check, and nothing is added', async () => {
        const why = 'no release of teranos/garden publishes qntx-garden-plugin-<version>-linux-amd64.tar.gz (newest seen: )';
        node({
            'POST /api/plugins/check': () => new Response(JSON.stringify({ id: 'ERR-2', error: why, timestamp: 0 }), { status: 400 }),
        });
        const content = await openPanel();

        await openAddField(content);
        type(content, '.plugin-add-repo', REPO);
        press(content, '.plugin-add-check');
        await flush();

        expect(content.querySelector('.qntx-btn-error-box')?.textContent).toBe(why);
        expect(content.querySelector('.plugin-add-confirm')).toBeNull();
        expect(asked.find(a => a.method === 'POST' && a.path === '/api/plugins')).toBeUndefined();
    });

    test('a repo changed after Check is checked again before it can be added', async () => {
        node({ 'POST /api/plugins/check': resolves });
        const content = await openPanel();

        await openAddField(content);
        type(content, '.plugin-add-repo', REPO);
        press(content, '.plugin-add-check');
        await flush();
        expect(content.querySelector('.plugin-add-confirm')).not.toBeNull();

        type(content, '.plugin-add-repo', REPO + '-other');
        expect(content.querySelector('.plugin-add-confirm')).toBeNull();
        expect(content.querySelector('.plugin-add-check')).not.toBeNull();
    });

    test('a plugin already added is refused in the node\'s words, beside Add, and the field stays', async () => {
        held = [{ name: 'garden', version: '', description: '', healthy: false, state: 'disabled', pausable: false, repo: REPO, enabled: false }];
        node({
            'POST /api/plugins/check': resolves,
            'POST /api/plugins': () => new Response(JSON.stringify({ id: 'ERR-1', error: `plugin garden is already added, from ${REPO}`, timestamp: 0 }), { status: 400 }),
        });
        const content = await openPanel();

        await openAddField(content);
        type(content, '.plugin-add-repo', REPO);
        press(content, '.plugin-add-check');
        await flush();
        press(content, '.plugin-add-confirm');
        await flush();

        expect(content.querySelector('.qntx-btn-error-box')?.textContent).toBe(`plugin garden is already added, from ${REPO}`);
        expect(content.querySelector<HTMLInputElement>('.plugin-add-repo')?.value).toBe(REPO);
        expect(content.querySelectorAll('.plugin-card').length).toBe(1);
    });
});

describe('Jenny: configured while it does not run, then enabled', () => {
    test('with no schema, a key is added and saved as typed', async () => {
        held = [{ name: 'garden', version: '', description: '', healthy: false, state: 'disabled', pausable: false, repo: REPO, enabled: false }];
        node({
            'GET /api/plugins/garden/config': () => new Response(JSON.stringify({ plugin: 'garden', config: { region: 'eu' }, schema: null, repo: REPO, enabled: false }), { status: 200 }),
        });
        const content = await openPanel();

        content.querySelector<HTMLElement>('.plugin-card[data-plugin="garden"] .plugin-name')!.click();
        await flush();
        expect(content.textContent).toContain('keys are written as typed');
        expect(content.querySelector('.plugin-config-value-display[data-field="region"]')?.textContent).toBe('eu');

        type(content, '.plugin-config-new-key', 'token_name');
        press(content, '.plugin-config-key-add');
        type(content, '.plugin-config-value-new[data-field="token_name"]', 'garden-bot');
        press(content, '.plugin-config-key-remove[data-field="region"]');
        press(content, '.plugin-config-save-btn');
        expect(content.querySelector('.plugin-config-save-btn')?.textContent).toContain('Confirm Save');
        press(content, '.plugin-config-save-btn');
        await flush();

        const put = asked.find(a => a.method === 'PUT' && a.path === '/api/plugins/garden/config');
        expect(put?.body).toEqual({ config: { token_name: 'garden-bot' } });
    });

    test('enabled and running, it can be disabled', async () => {
        held = [{ name: 'garden', version: '', description: '', healthy: false, state: 'disabled', pausable: false, repo: REPO, enabled: false }];
        node({
            'POST /api/plugins/garden/enable': () => {
                held = [{ ...held[0], version: '1.0.0', healthy: true, state: 'running', enabled: true }];
                return new Response(JSON.stringify({ name: 'garden', state: 'running', action: 'enable' }), { status: 200 });
            },
        });
        const content = await openPanel();

        press(content, '.plugin-enable-btn');
        await flush();

        const card = content.querySelector<HTMLElement>('.plugin-card[data-plugin="garden"]')!;
        expect(card.querySelector('.plugin-state-text')?.textContent).toBe('running');
        expect(card.querySelector('.plugin-disable-btn')).not.toBeNull();
        expect(card.querySelector('.plugin-enable-btn')).toBeNull();
    });

    test('enabled after the last probe, it is not probed, not unhealthy', async () => {
        held = [{ name: 'garden', version: '1.0.0', description: '', healthy: false, probed: false, state: 'running', pausable: false, repo: REPO, enabled: true }];
        node();
        const content = await openPanel();

        const card = content.querySelector<HTMLElement>('.plugin-card[data-plugin="garden"]')!;
        expect(card.querySelector('.plugin-status-text')?.textContent).toBe('Not probed');
        expect(content.querySelector('.plugin-health-summary')?.textContent).toBe('All healthy');
    });

    test('enabled and not started, the list says why', async () => {
        const why = 'failed to discover plugin garden: plugin binary not found';
        held = [{ name: 'garden', version: '', description: '', healthy: false, state: 'disabled', pausable: false, repo: REPO, enabled: false }];
        node({
            'POST /api/plugins/garden/enable': () => {
                held = [{ ...held[0], state: 'failed', enabled: true, message: why }];
                return new Response(JSON.stringify({ error: why }), { status: 400 });
            },
        });
        const content = await openPanel();

        press(content, '.plugin-enable-btn');
        await flush();

        const card = content.querySelector<HTMLElement>('.plugin-card[data-plugin="garden"]')!;
        expect(card.querySelector('.plugin-state-text')?.textContent).toBe('failed');
        expect(card.querySelector('.plugin-message-error')?.textContent).toBe(why);
    });
});

describe('Tim: the plugins cannot be read', () => {
    test('the records did not answer, and the element says so in full', async () => {
        const why = 'failed to read the PLUGIN lines in system: ' + 'the store did not answer '.repeat(20);
        node();
        listed = () => Promise.resolve({ plugins: [], records_failure: why });
        const content = await openPanel();

        expect(content.querySelector('.plugin-list-failure')?.textContent).toBe(why);
        expect(content.querySelector('.plugin-add-card')).toBeNull();
    });

    test('the list did not load, and the element says so in full', async () => {
        const why = 'GET /api/plugins answered 500: the node is starting';
        node();
        listed = () => Promise.reject(new Error(why));
        const content = await openPanel();

        expect(content.querySelector('.plugin-list-failure')?.textContent).toContain(why);
        expect(content.querySelector('.plugin-add-card')).toBeNull();
    });
});
