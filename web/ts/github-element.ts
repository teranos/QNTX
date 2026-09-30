/**
 * GitHub Element — the node's GitHub (ADR-043).
 */

// "to disable it on the Node entirely"
// "to see which namespaces have it enabled and if their auth is correct, and wheter is comes from them setting an access token or via OAuth (Oauth path doesnt exist yet, but will)"

// Plain window, reached from ⍟ by ROOT, like Mail. One section per sigil of the
// github signum: the node's switch, each namespace's token, and at the bottom
// the Actions runner. Asked on open and after each press; nothing polls.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { Button } from './components/button';
import { refusal } from './self-person';
import { log, SEG } from './logger';
import { renderSparklines, windowOf } from './components/sparkline';

/** GitHub's core rate limit for one namespace's token. */
export interface GitHubRate {
    limit: number;
    remaining: number;
    reset: string;
}

/** One namespace's GitHub, as /api/github gives it. */
export interface GitHubNamespace {
    namespace: string;
    source: string;
    login?: string;
    minted_by: string;
    revoked: boolean;
    auth_ok: boolean;
    auth_error?: string;
    rate?: GitHubRate;
}

/** A plugin build the runner delivered and the node took. */
export interface TakenBuild {
    plugin: string;
    archive: string;
    digest: string;
    at: string;
    changed: boolean;
    /** Landed before the installed build, so not installed over it. */
    older?: boolean;
}

/** The runner as its directory says it is now. */
export interface RunnerStats {
    path: string;
    name: string;
    github_url: string;
    /** One per repository the runner has checked out, by name. */
    workspaces: string[];
    /** When each job the runner ran was last written, oldest first. */
    jobs: string[];
    taken: TakenBuild[];
}

/** The runner as the Actions section shows it. */
export interface GitHubRunner {
    path: string;
    enabled: boolean;
    error?: string;
    stats?: RunnerStats;
}

export interface GitHubStatus {
    enabled: boolean;
    namespaces: GitHubNamespace[];
    runner: GitHubRunner;
    /** Whether ROOT generated the App's webhook secret, which opens /github/push. */
    webhook: boolean;
}

/** What generating the App's webhook secret gives, this once. */
export interface GitHubWebhook {
    secret: string;
    path: string;
}

const ELEMENT_ID = 'github-element';

// server/node_records.go DefaultRunnerPath; the node sends it too, this is for a path it left empty.
const DEFAULT_RUNNER_PATH = '/opt/actions-runner';

/** A time the node sent, to the second, in UTC. */
function fmt(at: string | undefined): string {
    if (!at) return '—';
    return at.slice(0, 19).split('T').join(' ');
}

function row(label: string, value: string | HTMLElement): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-row';
    const l = document.createElement('span');
    l.className = 'label';
    l.textContent = label;
    const v = document.createElement('span');
    v.className = 'element-value';
    if (typeof value === 'string') {
        v.textContent = value;
    } else {
        v.appendChild(value);
    }
    div.appendChild(l);
    div.appendChild(v);
    return div;
}

function section(title: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-section';
    const h = document.createElement('h3');
    h.className = 'element-section-title';
    h.textContent = title;
    div.appendChild(h);
    return div;
}

function cell(text: string, className = ''): HTMLTableCellElement {
    const td = document.createElement('td');
    td.className = className;
    td.textContent = text;
    return td;
}

function said(text: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-loading';
    div.textContent = text;
    return div;
}

function pill(text: string, on: boolean): HTMLSpanElement {
    const span = document.createElement('span');
    span.className = `element-pill ${on ? 'element-pill-on' : 'element-pill-off'}`;
    span.textContent = text;
    return span;
}

function errorText(text: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-error';
    div.textContent = text;
    return div;
}

/** Ask one github sigil. A refusal is thrown in the node's words, for the button to show. */
async function send(path: string, body: Record<string, string>): Promise<void> {
    const response = await apiFetch(path, jsonBody('POST', body));
    if (!response.ok) throw new Error(await refusal(response));
}

/** Where a namespace's token came from, in words. */
export function sourceInWords(source: string): string {
    if (source === 'oauth') return 'OAuth';
    if (source === 'access_token') return 'access token';
    return source;
}

/** Exported for tests: whether GitHub is on for the node, and the switch. */
export function renderNode(container: HTMLElement, status: GitHubStatus, reload: () => Promise<void>): void {
    container.innerHTML = '';
    const s = section('Node');
    s.appendChild(row('GitHub:', pill(status.enabled ? 'on' : 'off', status.enabled)));
    const actions = document.createElement('div');
    actions.className = 'element-actions';
    const flip = status.enabled
        ? new Button({
            label: 'Disable GitHub on the node',
            variant: 'danger',
            confirmation: { label: 'Disable for every namespace' },
            onClick: async () => {
                await send('/api/github/node', { enabled: 'false' });
                await reload();
            },
        })
        : new Button({
            label: 'Enable GitHub on the node',
            variant: 'primary',
            onClick: async () => {
                await send('/api/github/node', { enabled: 'true' });
                await reload();
            },
        });
    actions.appendChild(flip.element);
    s.appendChild(actions);
    container.appendChild(s);
}

/** Exported for tests: the App's webhook, and generating its secret. */
export function renderWebhook(container: HTMLElement, active: boolean, reload: () => Promise<void>): void {
    container.innerHTML = '';
    const s = section('Webhook');
    s.appendChild(row('Webhook:', pill(active ? 'open' : 'none', active)));
    const shown = document.createElement('div');
    const actions = document.createElement('div');
    actions.className = 'element-actions';
    const generate = new Button({
        label: active ? 'Generate a new secret' : 'Generate the secret',
        variant: active ? 'ghost' : 'primary',
        ...(active ? { confirmation: { label: 'Replace the secret GitHub holds' } } : {}),
        onClick: async () => {
            const response = await apiFetch('/api/github/webhook', jsonBody('POST', {}));
            if (!response.ok) throw new Error(await refusal(response));
            const made = await response.json() as GitHubWebhook;
            await reload();
            const again = container.querySelector('.github-webhook-shown');
            if (!again) return;
            again.appendChild(row('Path:', made.path));
            const secret = document.createElement('code');
            secret.textContent = made.secret;
            again.appendChild(row('Secret:', secret));
            again.appendChild(said('Shown this once: set both in the App’s webhook settings.'));
        },
    });
    actions.appendChild(generate.element);
    s.appendChild(actions);
    shown.className = 'github-webhook-shown';
    s.appendChild(shown);
    container.appendChild(s);
}

/** Whether GitHub accepts the token, and what it said when it did not. */
function auth(ns: GitHubNamespace): HTMLTableCellElement {
    const td = document.createElement('td');
    if (ns.revoked) td.appendChild(pill('revoked', false));
    td.appendChild(pill(ns.auth_ok ? 'ok' : 'refused', ns.auth_ok));
    if (!ns.auth_ok && ns.auth_error) td.appendChild(errorText(ns.auth_error));
    return td;
}

/** Exported for tests: every namespace with a GitHub token. */
export function renderNamespaces(container: HTMLElement, namespaces: GitHubNamespace[]): void {
    container.innerHTML = '';
    const s = section('Namespaces');

    if (namespaces.length === 0) {
        s.appendChild(said('No namespace has a GitHub token.'));
        container.appendChild(s);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table github-namespaces-table';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>Namespace</th><th>Source</th><th>Login</th><th>Minted by</th><th>Auth</th><th>Rate</th></tr>';
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    for (const ns of namespaces) {
        const tr = document.createElement('tr');
        tr.appendChild(cell(ns.namespace));
        tr.appendChild(cell(sourceInWords(ns.source)));
        tr.appendChild(cell(ns.login || '—'));
        tr.appendChild(cell(ns.minted_by));
        tr.appendChild(auth(ns));
        const rate = cell(ns.rate ? `${ns.rate.remaining} / ${ns.rate.limit}` : '—');
        if (ns.rate) rate.title = `resets ${fmt(ns.rate.reset)} UTC`;
        tr.appendChild(rate);
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    s.appendChild(table);
    container.appendChild(s);
}

/** The builds the runner delivered and the node took, newest as the runner kept them. */
function takenTable(taken: TakenBuild[]): HTMLElement {
    if (taken.length === 0) return said('No build taken yet.');
    const table = document.createElement('table');
    table.className = 'element-table github-taken-table';
    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>When</th><th>Plugin</th><th>Archive</th><th>Digest</th><th>Changed</th></tr>';
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    for (const t of taken) {
        const tr = document.createElement('tr');
        tr.appendChild(cell(fmt(t.at), 'element-time'));
        tr.appendChild(cell(t.plugin));
        tr.appendChild(cell(t.archive));
        tr.appendChild(cell(t.digest));
        tr.appendChild(cell(t.changed ? 'yes' : t.older ? 'no, older than installed' : 'no'));
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    return table;
}

// An external link: the underline says it leaves QNTX.
function outside(url: string): string | HTMLElement {
    if (!url) return '—';
    const a = document.createElement('a');
    a.href = url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = url;
    return a;
}

/** Exported for tests: the runner's path, its switch, why it is not watched, and its stats. */
export function renderActions(container: HTMLElement, runner: GitHubRunner, reload: () => Promise<void>): void {
    container.innerHTML = '';
    const s = section('Actions');

    const path = document.createElement('input');
    path.type = 'text';
    path.className = 'input github-runner-path';
    path.value = runner.path || DEFAULT_RUNNER_PATH;
    path.placeholder = DEFAULT_RUNNER_PATH;
    path.autocomplete = 'off';
    path.spellcheck = false;
    s.appendChild(row('Runner path:', path));
    s.appendChild(row('Runner:', pill(runner.enabled ? 'on' : 'off', runner.enabled)));

    const actions = document.createElement('div');
    actions.className = 'element-actions';
    const toggle = new Button({
        label: runner.enabled ? 'Turn the runner off' : 'Turn the runner on',
        variant: runner.enabled ? 'ghost' : 'primary',
        onClick: async () => {
            await send('/api/github/runner', { path: path.value.trim(), enabled: String(!runner.enabled) });
            await reload();
        },
    });
    actions.appendChild(toggle.element);
    s.appendChild(actions);

    if (runner.error) s.appendChild(errorText(runner.error));

    if (runner.enabled && runner.stats) {
        const st = runner.stats;
        s.appendChild(row('Name:', st.name || '—'));
        s.appendChild(row('GitHub:', outside(st.github_url)));
        s.appendChild(row('Workspaces:', st.workspaces.length > 0 ? st.workspaces.join(', ') : 'none'));
        // "so we see it over time"
        const jobs = st.jobs.map(at => Date.parse(at));
        const over = document.createElement('div');
        over.className = 'github-runner-jobs';
        renderSparklines(over, 'Jobs', [{ name: st.name || st.path, times: jobs }], windowOf(jobs));
        s.appendChild(over);
        s.appendChild(takenTable(st.taken));
    }

    container.appendChild(s);
}

/** A section the node did not answer for says what it said instead. */
function refused(container: HTMLElement, err: unknown): void {
    const message = `the node did not say its GitHub: ${err instanceof Error ? err.message : String(err)}`;
    log.error(SEG.UI, `[GitHubElement] ${message}`, err);
    container.innerHTML = '';
    container.appendChild(errorText(message));
}

/** Exported for tests: ask the node once and draw every section from its answer. */
export async function load(node: HTMLElement, namespaces: HTMLElement, actions: HTMLElement, webhook?: HTMLElement): Promise<void> {
    const reload = () => load(node, namespaces, actions, webhook);
    try {
        const status = await apiJson<GitHubStatus>('/api/github');
        renderNode(node, status, reload);
        if (webhook) renderWebhook(webhook, status.webhook, reload);
        renderNamespaces(namespaces, status.namespaces);
        renderActions(actions, status.runner, reload);
    } catch (err: unknown) {
        refused(node, err);
        namespaces.innerHTML = '';
        actions.innerHTML = '';
        if (webhook) webhook.innerHTML = '';
    }
}

export function createGitHubElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'GitHub',
        symbol: '⎇',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'github-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const node = document.createElement('div');
            const webhook = document.createElement('div');
            const namespaces = document.createElement('div');
            const actions = document.createElement('div');
            node.appendChild(said('Loading the node’s GitHub…'));
            content.appendChild(node);
            content.appendChild(webhook);
            content.appendChild(namespaces);
            content.appendChild(actions);

            void load(node, namespaces, actions, webhook);
            return content;
        },
    };
}

/** Opens the GitHub element. Called from ⍟, for ROOT. */
export function openGitHubElement(): void {
    tray.open(ELEMENT_ID);
}
