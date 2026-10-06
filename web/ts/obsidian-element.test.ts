/**
 * Obsidian element — a vault's folders, each bound by clicking to a folder of a
 * repository, and what each bound folder is now (ADR-049).
 */

import { describe, test, expect, mock, afterEach } from 'bun:test';

const noAnswer = () => Promise.resolve(new Response());
let answer: (path: string, init?: RequestInit) => Promise<Response> = noAnswer;
let answers: Record<string, unknown> = {};
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
    apiJson: (path: string) => {
        return path in answers ? Promise.resolve(answers[path]) : Promise.reject(new Error(`nothing answers ${path}`));
    },
    backendUrl: () => 'http://localhost',
    backendWsUrl: () => 'ws://localhost',
    backendPath: (path: string) => 'http://localhost' + path,
    sendMessage: () => false,
    connectWebSocket: () => {},
    registerHandler: () => {},
    unregisterHandler: () => {},
}));

const { renderVault, load, createObsidianElement, SETUP_NOT_BUILT } = await import('./obsidian-element');

afterEach(() => {
    answer = noAnswer;
    answers = {};
    document.body.innerHTML = '';
});

const flush = async () => {
    for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0));
};

const reload = () => Promise.resolve();

const vault = { name: 'abcd', path: '/var/lib/obsidian/abcd', folders: [] as string[] };
const dirs = ['ABCD', 'ABCD/lttr', 'Course Material'];

function shown(view = { vault, dirs, states: [] as never[] }) {
    const container = document.createElement('div');
    document.body.appendChild(container);
    renderVault(container, view, reload);
    return container;
}

const row = (container: HTMLElement, place: string) => container.querySelector<HTMLElement>(`.obsidian-row[data-place="${place}"]`)!;
const click = (container: HTMLElement, text: string) => {
    const button = [...container.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent?.startsWith(text));
    if (!button) throw new Error(`no button ${text} among ${[...container.querySelectorAll('button')].map(b => b.textContent).join(', ')}`);
    button.click();
};

describe('setting a vault up is not built yet', () => {
    // 1.0.0 blocker (#1091): said in the element, not left for somebody to find out.
    test('the element says so, and names the issue', () => {
        answers = { '/api/vault': { vaults: [] } };
        const body = createObsidianElement().renderContent();
        expect(body.querySelector('.obsidian-setup-not-built')?.textContent).toBe(SETUP_NOT_BUILT);
        expect(SETUP_NOT_BUILT).toContain('#1091');
        expect(SETUP_NOT_BUILT).toContain('1.0.0 blocker');
    });
});

describe('the vault is its folders', () => {
    test('the top folders show, and a folder opens to show its own', () => {
        const container = shown();
        expect(row(container, 'ABCD')).toBeTruthy();
        expect(row(container, 'Course Material')).toBeTruthy();
        expect(row(container, 'ABCD/lttr')).toBeFalsy();
        row(container, 'ABCD').querySelector<HTMLButtonElement>('.obsidian-twist')!.click();
        expect(row(container, 'ABCD/lttr')).toBeTruthy();
    });

    test('each vault is asked for its folders and their states', async () => {
        answers = {
            '/api/vault': { vaults: [vault] },
            '/api/vault/dirs?name=abcd': { dirs },
            '/api/vault/states?name=abcd': { folders: [] },
        };
        const container = document.createElement('div');
        await load(container);
        expect(container.querySelector('.obsidian-vault-name')?.textContent).toBe('abcd');
        expect(row(container, 'Course Material')).toBeTruthy();
    });

    test('no vault is said, not left blank', async () => {
        answers = { '/api/vault': { vaults: [] } };
        const container = document.createElement('div');
        await load(container);
        expect(container.textContent).toContain('The node keeps no vault yet.');
    });
});

describe('a folder is bound by clicking', () => {
    test('owner, repository, folder, and two presses of Confirm', async () => {
        answers = {
            '/api/vault/owners': { owners: [{ login: 'abcd-nl', type: 'Organization', installation: 7 }, { login: 'abcd', type: 'User', installation: 8 }] },
            '/api/vault/repos?installation=7': { repos: ['abcd-nl/clean'] },
            '/api/vault/subdirs?repo=abcd-nl%2Fclean&path=': { dirs: ['cdr', 'docs'] },
            '/api/vault/subdirs?repo=abcd-nl%2Fclean&path=docs': { dirs: ['docs/adr'] },
        };
        let sent: unknown = null;
        answer = (path, init) => {
            sent = { path, body: JSON.parse(String(init?.body)) };
            return Promise.resolve(new Response('{}'));
        };
        const container = shown();

        row(container, 'Course Material').querySelector<HTMLButtonElement>('.obsidian-name')!.click();
        click(container, 'Bind to repository md folder');
        await flush();
        expect(container.querySelector('.obsidian-list')?.textContent).toContain('abcd-nl  org');
        expect(container.querySelector('.obsidian-list')?.textContent).toContain('abcd  user');
        click(container, 'abcd-nl');
        await flush();
        click(container, 'clean');
        await flush();
        // A folder is chosen by its name, or opened by ▸ to choose inside it.
        const docs = container.querySelectorAll('.obsidian-subdir')[1];
        expect(docs.firstElementChild?.textContent).toBe('▸');
        docs.querySelector<HTMLButtonElement>('.obsidian-into')!.click();
        await flush();
        click(container, 'adr');
        expect(container.querySelector('.obsidian-panel')?.textContent).toContain('abcd-nl/clean@main:docs/adr=Course Material');

        click(container, 'Confirm');
        await flush();
        expect(sent).toBeNull();
        click(container, 'Confirm again to bind');
        await flush();
        expect(sent).toEqual({ path: '/api/vault/bind', body: { name: 'abcd', place: 'Course Material', repo: 'abcd-nl/clean', path: 'docs/adr' } });
    });

    test('a folder inside a bound one is not offered for binding, and says why', () => {
        const container = shown({ vault: { ...vault, folders: ['abcd-nl/clean@main:cdr=ABCD'] }, dirs, states: [
            { folder: 'abcd-nl/clean@main:cdr=ABCD', place: 'ABCD', state: 'active', why: '' },
        ] as never[] });
        row(container, 'ABCD').querySelector<HTMLButtonElement>('.obsidian-twist')!.click();
        row(container, 'ABCD/lttr').querySelector<HTMLButtonElement>('.obsidian-name')!.click();
        expect(container.querySelector('.obsidian-refused')?.textContent).toBe('ABCD and ABCD/lttr are one place in the vault, or one holds the other');
        expect(container.textContent).not.toContain('Bind to repository md folder');
    });
});

describe('a binding is always in sight', () => {
    // "and if there is a binding, i expect it to not be collapsed and hidden, all per dir bdingings to repo need to be visible at all times"
    test('the folders holding a bound one are open, and stay open', () => {
        const container = shown({ vault: { ...vault, folders: ['abcd-nl/clean@main:cdr=ABCD/lttr'] }, dirs, states: [
            { folder: 'abcd-nl/clean@main:cdr=ABCD/lttr', place: 'ABCD/lttr', state: 'active', why: '' },
        ] as never[] });
        expect(row(container, 'ABCD/lttr')).toBeTruthy();
        const twist = row(container, 'ABCD').querySelector<HTMLButtonElement>('.obsidian-twist')!;
        expect(twist.textContent).toBe('▾');
        expect(twist.disabled).toBe(true);
        twist.click();
        expect(row(container, 'ABCD/lttr')).toBeTruthy();
    });
});

describe('a bound folder is unbound', () => {
    // "and if expanded, there should be a two stage button to allow me to unbind as well."
    test('opened, it is unbound with two presses', async () => {
        let sent: unknown = null;
        answer = (path, init) => {
            sent = { path, body: JSON.parse(String(init?.body)) };
            return Promise.resolve(new Response('{}'));
        };
        const container = shown({ vault: { ...vault, folders: ['abcd-nl/clean@main:cdr=ABCD'] }, dirs, states: [
            { folder: 'abcd-nl/clean@main:cdr=ABCD', place: 'ABCD', state: 'active', why: '' },
        ] as never[] });
        row(container, 'ABCD').querySelector<HTMLButtonElement>('.obsidian-name')!.click();
        expect(container.querySelector('.obsidian-panel')?.textContent).toContain('abcd-nl/clean@main:cdr=ABCD');

        click(container, 'Unbind');
        await flush();
        expect(sent).toBeNull();
        click(container, 'Confirm again to unbind');
        await flush();
        expect(sent).toEqual({ path: '/api/vault/unbind', body: { name: 'abcd', place: 'ABCD' } });
    });
});

describe('a bound folder is disabled and enabled', () => {
    // "and another button to simply disable it, but the bind is still there, it just doesnt do anything"
    test('one press disables it, and a disabled one is enabled the same way', async () => {
        const sent: unknown[] = [];
        answer = (path, init) => {
            sent.push({ path, body: JSON.parse(String(init?.body)) });
            return Promise.resolve(new Response('{}'));
        };
        const container = shown({ vault: { ...vault, folders: ['abcd-nl/clean@main:cdr=ABCD', 'abcd-nl/clean@main:docs=Course Material'] }, dirs, states: [
            { folder: 'abcd-nl/clean@main:cdr=ABCD', place: 'ABCD', state: 'active', why: '' },
            { folder: 'abcd-nl/clean@main:docs=Course Material', place: 'Course Material', state: 'disabled', why: '' },
        ] as never[] });
        expect(row(container, 'Course Material').classList.contains('obsidian-row-disabled')).toBe(true);
        expect(row(container, 'Course Material').title).toBe('disabled');

        row(container, 'ABCD').querySelector<HTMLButtonElement>('.obsidian-name')!.click();
        click(container, 'Disable');
        await flush();
        row(container, 'Course Material').querySelector<HTMLButtonElement>('.obsidian-name')!.click();
        click(container, 'Enable');
        await flush();
        expect(sent).toEqual([
            { path: '/api/vault/disable', body: { name: 'abcd', place: 'ABCD' } },
            { path: '/api/vault/enable', body: { name: 'abcd', place: 'Course Material' } },
        ]);
    });
});

describe('a bound folder says what it is now', () => {
    // "what should a valid binding show? that its active, green dot,"
    // "let's say color state is part of the row itself, the round dot remains, but has no text, hover shows text or reason."
    test('the row is its state, and its dot says it only when hovered', () => {
        const container = shown({ vault: { ...vault, folders: ['abcd-nl/clean@main:cdr=ABCD', 'abcd-nl/clean@main:gone=Course Material'] }, dirs, states: [
            { folder: 'abcd-nl/clean@main:cdr=ABCD', place: 'ABCD', state: 'active', why: '' },
            { folder: 'abcd-nl/clean@main:gone=Course Material', place: 'Course Material', state: 'invalid', why: 'GitHub GET /repos/abcd-nl/clean/contents/gone answered 404: Not Found' },
        ] as never[] });
        expect(row(container, 'ABCD').classList.contains('obsidian-row-active')).toBe(true);
        expect(row(container, 'ABCD').querySelector('.obsidian-state-active')?.textContent).toBe('');
        expect(row(container, 'ABCD').title).toBe('active');
        expect(row(container, 'ABCD').querySelector('.obsidian-bound')?.textContent).toBe('⇄ abcd-nl/clean@main:cdr');
        expect(row(container, 'Course Material').classList.contains('obsidian-row-invalid')).toBe(true);
        expect(row(container, 'Course Material').querySelector('.obsidian-state-invalid')?.textContent).toBe('');
        expect(row(container, 'Course Material').title).toBe('Invalid: GitHub GET /repos/abcd-nl/clean/contents/gone answered 404: Not Found');
        expect(container.querySelector('.obsidian-record')?.textContent).toContain('abcd-nl/clean@main:gone=Course Material');
    });
});
