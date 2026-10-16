/**
 * GitHub Element — the node's GitHub (ADR-043).
 */

// "to disable it on the Node entirely"
// "to see which namespaces have it enabled and if their auth is correct, and wheter is comes from them setting an access token or via OAuth (Oauth path doesnt exist yet, but will)"

// Plain window, reached from ⍟ by ROOT, like Mail. One section per sigil of the
// github signum: the node's switch, each namespace's token, and at the bottom
// the Actions runner. Asked on open and after each press; nothing polls.

import type { Element } from '@teranos/elements';
import { createTable, tray } from '@teranos/elements';
import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { Button } from './components/button';
import { refusal } from './self-person';
import { log, SEG } from './logger';
import { stacked } from './table-cell';
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
    /** What of the runner's directory could not be read, and why. */
    unread: string[];
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
    /** Whether ROOT generated the App's webhook secret, which opens the webhook's path. */
    webhook: boolean;
    webhook_path: string;
    /** The whole URL to paste into the App's webhook settings. */
    webhook_url: string;
}

/** What generating the App's webhook secret gives, this once. */
export interface GitHubWebhook {
    secret: string;
    url: string;
}

export const ELEMENT_ID = 'github-element';

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
    if (source === 'system') return 'system namespace';
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
export function renderWebhook(container: HTMLElement, status: GitHubStatus, reload: () => Promise<void>): void {
    const active = status.webhook;
    container.innerHTML = '';
    const s = section('Webhook');
    s.appendChild(row('Webhook:', pill(active ? 'open' : 'none', active)));

    // The URL is the node's to say, so it is pasted and never typed.
    const url = document.createElement('code');
    url.className = 'github-webhook-url';
    url.textContent = status.webhook_url;
    s.appendChild(row('URL:', url));

    const path = document.createElement('input');
    path.type = 'text';
    path.className = 'input github-webhook-path';
    path.value = status.webhook_path;
    path.autocomplete = 'off';
    path.spellcheck = false;
    s.appendChild(row('Path:', path));

    const shown = document.createElement('div');
    const actions = document.createElement('div');
    actions.className = 'element-actions';
    const move = new Button({
        label: 'Set the path',
        variant: 'ghost',
        onClick: async () => {
            await send('/api/github/webhook/path', { path: path.value.trim() });
            await reload();
        },
    });
    actions.appendChild(move.element);
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
            const madeURL = document.createElement('code');
            madeURL.textContent = made.url;
            again.appendChild(row('URL:', madeURL));
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

    // "TABLES BEING MAX 4 col": the namespace carries where its token came
    // from, and the login who minted it.
    const table = createTable('github-namespaces', ['Namespace', 'Login', 'Auth', 'Rate'], namespaces.map(ns => {
        const rate = cell(ns.rate ? `${ns.rate.remaining} / ${ns.rate.limit}` : '—');
        if (ns.rate) rate.title = `resets ${fmt(ns.rate.reset)} UTC`;
        return {
            cells: [
                stacked(ns.namespace, sourceInWords(ns.source)),
                stacked(ns.login || '—', `minted by ${ns.minted_by}`),
                auth(ns),
                rate,
            ] as const,
        };
    }));
    s.appendChild(table);
    container.appendChild(s);
}

// "ROOT will determine what GitHub integration is active there, either system or have the namespace take responsibility"
/** Exported for tests: per namespace the node serves, which GitHub it spends, and the press that changes it. */
export function renderFrom(container: HTMLElement, served: string[], namespaces: GitHubNamespace[], reload: () => Promise<void>): void {
    container.innerHTML = '';
    const s = section('From');
    const sources = new Map(namespaces.map(ns => [ns.namespace, ns.source]));
    for (const name of served.filter(n => n !== 'system')) {
        const fromSystem = sources.get(name) === 'system';
        const press = new Button({
            label: fromSystem ? 'Its own GitHub' : 'System’s GitHub',
            variant: fromSystem ? 'ghost' : 'primary',
            onClick: async () => {
                await send('/api/github/from', { namespace: name, from: fromSystem ? 'namespace' : 'system' });
                await reload();
            },
        });
        s.appendChild(row(`${name}:`, press.element));
    }
    container.appendChild(s);
}

/** The builds the runner delivered and the node took, newest as the runner kept them. */
function takenTable(taken: TakenBuild[]): HTMLElement {
    if (taken.length === 0) return said('No build taken yet.');
    // "TABLES BEING MAX 4 col": the plugin carries the archive it came in.
    const table = createTable('github-taken', ['When', 'Plugin', 'Digest', 'Changed'], taken.map(t => ({
        cells: [
            cell(fmt(t.at), 'element-time'),
            stacked(t.plugin, t.archive),
            cell(t.digest),
            cell(t.changed ? 'yes' : t.older ? 'no, older than installed' : 'no'),
        ] as const,
    })));
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
        // What the stats above leave out, because it could not be read.
        for (const why of st.unread) s.appendChild(errorText(why));
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
export async function load(node: HTMLElement, namespaces: HTMLElement, actions: HTMLElement, webhook?: HTMLElement, from?: HTMLElement): Promise<void> {
    const reload = () => load(node, namespaces, actions, webhook, from);
    try {
        const status = await apiJson<GitHubStatus>('/api/github');
        renderNode(node, status, reload);
        if (webhook) renderWebhook(webhook, status, reload);
        renderNamespaces(namespaces, status.namespaces);
        renderActions(actions, status.runner, reload);
        if (from) await loadFrom(from, status.namespaces, reload);
    } catch (err: unknown) {
        refused(node, err);
        namespaces.innerHTML = '';
        actions.innerHTML = '';
        if (webhook) webhook.innerHTML = '';
        if (from) from.innerHTML = '';
    }
}

// Who this is for: whoever reaches these (audience.ts).
export const needs = ['/api/github'] as const;

/** The From section, from the namespaces the node serves; a node that keeps none says why there. */
async function loadFrom(from: HTMLElement, namespaces: GitHubNamespace[], reload: () => Promise<void>): Promise<void> {
    const response = await apiFetch('/api/namespaces');
    if (!response.ok) {
        from.innerHTML = '';
        from.appendChild(errorText(`the node did not say its namespaces: ${await refusal(response)}`));
        return;
    }
    const listed = await response.json() as { namespaces: { name: string }[] };
    renderFrom(from, listed.namespaces.map(ns => ns.name), namespaces, reload);
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
            const from = document.createElement('div');
            const actions = document.createElement('div');
            node.appendChild(said('Loading the node’s GitHub…'));
            content.appendChild(node);
            content.appendChild(webhook);
            content.appendChild(namespaces);
            content.appendChild(from);
            content.appendChild(actions);

            void load(node, namespaces, actions, webhook, from);
            return content;
        },
    };
}

/** Opens the GitHub element. Called from ⍟, for ROOT. */
export function openGitHubElement(): void {
    tray.open(ELEMENT_ID);
}
