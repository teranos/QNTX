/**
 * Obsidian element — the vaults the node keeps, and the folders each holds (ADR-049).
 */

import { describe, test, expect, mock, afterEach } from 'bun:test';

const noAnswer = () => Promise.resolve(new Response());
let answer: (path: string, init?: RequestInit) => Promise<Response> = noAnswer;
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
    apiJson: () => Promise.resolve({}),
    backendUrl: () => 'http://localhost',
    backendWsUrl: () => 'ws://localhost',
    backendPath: (path: string) => 'http://localhost' + path,
    sendMessage: () => false,
    connectWebSocket: () => {},
    registerHandler: () => {},
    unregisterHandler: () => {},
}));

const { renderVaults, renderNewVault } = await import('./obsidian-element');

afterEach(() => {
    answer = noAnswer;
    document.body.innerHTML = '';
});

const flush = async () => {
    for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0));
};

const reload = () => Promise.resolve();

describe('ROOT names the folders a vault holds', () => {
    test('a vault shows its path and its folders, one per line', () => {
        const container = document.createElement('div');
        renderVaults(container, [{ name: 'notes', path: '/var/lib/obsidian/notes', folders: ['abcd-nl/clean@main:cdr=ABCD/clean/cdr', 'abcd-nl/clean@main:docs=ABCD/clean/docs'] }], reload);
        expect(container.querySelector('.element-section-title')?.textContent).toBe('notes');
        expect(container.querySelector<HTMLInputElement>('.obsidian-path')?.value).toBe('/var/lib/obsidian/notes');
        expect(container.querySelector<HTMLTextAreaElement>('.obsidian-folders')?.value).toBe('abcd-nl/clean@main:cdr=ABCD/clean/cdr\nabcd-nl/clean@main:docs=ABCD/clean/docs');
    });

    test('no vault is said, not left blank', () => {
        const container = document.createElement('div');
        renderVaults(container, [], reload);
        expect(container.textContent).toContain('The node keeps no vault yet.');
    });

    test('saving sends the vault whole, its folders apart by spaces', async () => {
        let sent: unknown = null;
        answer = (path, init) => {
            sent = { path, body: JSON.parse(String(init?.body)) };
            return Promise.resolve(new Response('{}'));
        };
        const container = document.createElement('div');
        document.body.appendChild(container);
        renderVaults(container, [{ name: 'notes', path: '/var/lib/obsidian/notes', folders: [] }], reload);
        container.querySelector<HTMLTextAreaElement>('.obsidian-folders')!.value = 'abcd-nl/clean@main:cdr=ABCD/clean/cdr\nabcd-nl/clean@main:docs=ABCD/clean/docs';
        container.querySelector<HTMLButtonElement>('.element-actions button')!.click();
        await flush();
        expect(sent).toEqual({ path: '/api/vault', body: { name: 'notes', path: '/var/lib/obsidian/notes', folders: 'abcd-nl/clean@main:cdr=ABCD/clean/cdr abcd-nl/clean@main:docs=ABCD/clean/docs' } });
    });

    test('a folder the node refuses shows the node\'s words beside the button', async () => {
        answer = () => Promise.resolve(new Response(JSON.stringify({ id: 'ERR-1', error: '"abcd-nl/clean@main" names no file: owner/repo@branch:path', timestamp: 0 }), { status: 400 }));
        const container = document.createElement('div');
        document.body.appendChild(container);
        renderNewVault(container, reload);
        container.querySelector<HTMLInputElement>('.obsidian-name')!.value = 'notes';
        container.querySelector<HTMLInputElement>('.obsidian-path')!.value = '/var/lib/obsidian/notes';
        container.querySelector<HTMLTextAreaElement>('.obsidian-folders')!.value = 'abcd-nl/clean@main';
        container.querySelector<HTMLButtonElement>('.element-actions button')!.click();
        await flush();
        expect(container.querySelector('.qntx-btn-error-box')?.textContent).toContain('names no file');
    });
});
