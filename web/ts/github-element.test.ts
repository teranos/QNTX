/**
 * GitHub element — the node's GitHub (ADR-043).
 */

import { describe, test, expect, mock, afterEach } from 'bun:test';

// The node's answers are swapped per test; the defaults are test-setup.ts's own,
// so every other file importing the client sees what it always saw.
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

const { renderActions, renderNamespaces, renderNode, sourceInWords } = await import('./github-element');
type GitHubNamespace = Parameters<typeof renderNamespaces>[1][number];
type GitHubRunner = Parameters<typeof renderActions>[1];

afterEach(() => {
    answer = noAnswer;
    document.body.innerHTML = '';
});

const flush = async () => {
    for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0));
};

function ns(overrides: Partial<GitHubNamespace> = {}): GitHubNamespace {
    return {
        namespace: 'garden',
        source: 'access_token',
        login: 'abcd',
        minted_by: 'UStim',
        revoked: false,
        auth_ok: true,
        rate: { limit: 5000, remaining: 4999, reset: '2026-09-28T15:00:00Z' },
        ...overrides,
    };
}

function runner(overrides: Partial<GitHubRunner> = {}): GitHubRunner {
    return { path: '/opt/actions-runner', enabled: false, ...overrides };
}

const reload = () => Promise.resolve();

describe('Tim: ROOT reads the node\'s GitHub', () => {
    test('GitHub on for the node says on, and offers switching it off', () => {
        const container = document.createElement('div');
        renderNode(container, { enabled: true, namespaces: [], runner: runner() }, reload);
        expect(container.querySelector('.element-pill')?.textContent).toBe('on');
        expect(container.textContent).toContain('Disable GitHub on the node');
    });

    // "whether it comes from an access token or OAuth"
    test('each namespace says where its token came from, whether GitHub accepts it, and its rate', () => {
        const container = document.createElement('div');
        renderNamespaces(container, [ns(), ns({ namespace: 'market', source: 'oauth', login: 'tim' })]);
        const rows = container.querySelectorAll('tbody tr');
        expect(rows.length).toBe(2);
        expect(rows[0].textContent).toContain('garden');
        expect(rows[0].textContent).toContain('access token');
        expect(rows[0].textContent).toContain('ok');
        expect(rows[0].textContent).toContain('4999 / 5000');
        expect(rows[1].textContent).toContain('OAuth');
        expect(sourceInWords('oauth')).toBe('OAuth');
    });

    test('the runner path is prefilled with /opt/actions-runner when the node names none', () => {
        const container = document.createElement('div');
        renderActions(container, runner({ path: '' }), reload);
        expect(container.querySelector<HTMLInputElement>('.github-runner-path')?.value).toBe('/opt/actions-runner');
        expect(container.textContent).toContain('Turn the runner on');
    });

    test('an enabled runner shows its stats and the builds it took', () => {
        const container = document.createElement('div');
        renderActions(container, runner({
            enabled: true,
            stats: {
                path: '/opt/actions-runner',
                name: 'box-1',
                github_url: 'https://github.com/teranos',
                workspaces: ['datapunt', 'garden'],
                jobs: 7,
                last_job: '2026-09-28T13:04:05Z',
                taken: [{ plugin: 'garden', archive: 'qntx-garden-plugin-0.2.0-linux-amd64.tar.gz', digest: 'ab12', at: '2026-09-28T13:05:00Z', changed: true }],
            },
        }), reload);
        expect(container.textContent).toContain('box-1');
        expect(container.textContent).toContain('datapunt, garden');
        expect(container.textContent).toContain('2026-09-28 13:04:05');
        expect(container.querySelector('a')?.getAttribute('href')).toBe('https://github.com/teranos');
        const taken = container.querySelectorAll('.github-taken-table tbody tr');
        expect(taken.length).toBe(1);
        expect(taken[0].textContent).toContain('qntx-garden-plugin-0.2.0-linux-amd64.tar.gz');
    });
});

describe('Spike: what is wrong is said', () => {
    test('a token GitHub refuses says what GitHub said, and a revoked one says so', () => {
        const container = document.createElement('div');
        renderNamespaces(container, [ns({ auth_ok: false, auth_error: 'Bad credentials', rate: undefined, revoked: true })]);
        const row = container.querySelector('tbody tr')?.textContent ?? '';
        expect(row).toContain('revoked');
        expect(row).toContain('refused');
        expect(row).toContain('Bad credentials');
    });

    test('no namespace with a token is said, not left blank', () => {
        const container = document.createElement('div');
        renderNamespaces(container, []);
        expect(container.textContent).toContain('No namespace has a GitHub token.');
        expect(container.querySelector('table')).toBeNull();
    });

    test('a runner on and not watched says why', () => {
        const container = document.createElement('div');
        renderActions(container, runner({ enabled: true, error: 'no runner at /opt/actions-runner: .runner is missing' }), reload);
        expect(container.querySelector('.element-error')?.textContent).toContain('.runner is missing');
    });
});

describe('Jenny: turning the runner on where there is none', () => {
    // "it errors when there is no runner"
    test('the node\'s refusal shows beside the toggle, and what was sent is the typed path', async () => {
        let sent: unknown = null;
        answer = (path, init) => {
            sent = { path, body: JSON.parse(String(init?.body)) };
            return Promise.resolve(new Response(JSON.stringify({ id: 'ERR-1', error: 'no runner at /srv/none: .runner is missing', timestamp: 0 }), { status: 400 }));
        };
        let reloaded = false;
        const container = document.createElement('div');
        document.body.appendChild(container);
        renderActions(container, runner(), async () => { reloaded = true; });

        const path = container.querySelector<HTMLInputElement>('.github-runner-path')!;
        path.value = '/srv/none';
        container.querySelector<HTMLButtonElement>('.element-actions button')!.click();
        await flush();

        expect(sent).toEqual({ path: '/api/github/runner', body: { path: '/srv/none', enabled: 'true' } });
        expect(container.querySelector('.qntx-btn-error-box')?.textContent).toBe('no runner at /srv/none: .runner is missing');
        expect(reloaded).toBe(false);
    });
});
