/**
 * Handlers Panel - Python handler management
 *
 * Manifests as a panel glyph. Displays handler attestations
 * (predicate=handler) as code cards with syntax highlighting.
 */

import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { docComment } from './handlers-doc';
import { formatInterval } from './pulse/types';
import { log, SEG } from './logger.ts';
import type { Glyph } from '@qntx/glyphs';

interface HandlerAttestation {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    timestamp: string;
    attributes: Record<string, string>;
}

interface Schedule {
    handler_name?: string;
    interval_seconds?: number;
    last_run_at?: string;
    next_run_at?: string;
    state: string;
}

interface Watcher {
    id: string; // "<plugin>-<watcher>", which is where the plugin is named
    action_type: string;
    action_data: string;
    fire_count: number;
    last_fired_at?: string;
    error_count: number;
    last_error?: string;
}

interface ExecutionResult {
    running: boolean;
    success?: boolean;
    stdout?: string;
    error?: string;
    duration_ms?: number;
}

interface HandlerGroup {
    name: string;
    context: string;
    versions: HandlerAttestation[];
    selectedVersion: number; // index into versions array
    // 0 what it is for, 1 what it is, 2 how it does it
    openness: 0 | 1 | 2;
}

// Module-level state
let contentElement: HTMLElement | null = null;
let handlers: HandlerAttestation[] = [];
let groups: HandlerGroup[] = [];
let editorViews: any[] = [];
let codeStore: Map<string, string> = new Map();
let execResults: Map<number, ExecutionResult> = new Map();
let schedules: Schedule[] = [];
let watchers: Watcher[] = [];

// Unreachable schedules and watchers are not the same as none, so the card says
// which it is rather than claiming a handler nothing fires.
let firingFailure = '';

// Why the failure is kept rather than logged and dropped: an empty list and a
// request that never succeeded rendered the same sentence, so "no handlers"
// could mean the node has none or that nobody could ask it.
let fetchFailure = '';

async function fetchHandlers(): Promise<void> {
    try {
        handlers = await apiJson<HandlerAttestation[]>('/api/attestations?predicate=handler&limit=100');
        fetchFailure = '';
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Handlers] Failed to fetch handlers:', error);
        handlers = [];
        fetchFailure = error instanceof Error ? error.message : String(error);
    }
}

// A handler's code says what it wants to be wired to; only these say whether it
// ever went off. A failure here must not read as "nothing fires this".
async function fetchFiring(): Promise<void> {
    try {
        schedules = (await apiJson<{ jobs: Schedule[] }>('/api/pulse/schedules')).jobs || [];
        watchers = await apiJson<Watcher[]>('/api/watchers');
        firingFailure = '';
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Handlers] Failed to read what fires handlers:', error);
        schedules = [];
        watchers = [];
        firingFailure = error instanceof Error ? error.message : String(error);
    }
}

function watchedHandler(actionData: string): string {
    try {
        return JSON.parse(actionData).handler_name || '';
    } catch {
        return '';
    }
}

// A schedule stores PluginHandlerName — "<plugin>/<handler>" — while the
// attestation carries the two apart, as subject and context.
function scheduleOf(g: HandlerGroup): Schedule | undefined {
    const key = `${g.context}/${g.name}`;
    return schedules.find(s => s.handler_name === key);
}

// A watcher stores the bare handler name in its action, and names the plugin in
// its id instead, so matching on the name alone would cross two plugins.
function watcherOf(g: HandlerGroup): Watcher | undefined {
    return watchers.find(w => w.action_type === 'plugin_execute'
        && watchedHandler(w.action_data) === g.name
        && w.id.startsWith(`${g.context}-`));
}

function groupHandlers(): void {
    const map = new Map<string, HandlerAttestation[]>();
    for (const h of handlers) {
        const key = h.subjects[0] || '';
        const list = map.get(key);
        if (list) {
            list.push(h);
        } else {
            map.set(key, [h]);
        }
    }
    groups = [];
    for (const [name, versions] of map) {
        // Sort newest first
        versions.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
        groups.push({
            name,
            context: versions[0].contexts[0] || '',
            versions,
            selectedVersion: 0,
            openness: 0,
        });
    }
}

function formatDate(timestamp: string): string {
    const d = new Date(timestamp);
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()}`;
}

function formatDateTime(timestamp: string): string {
    const d = new Date(timestamp);
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

async function executeHandler(index: number): Promise<void> {
    const code = codeStore.get(`handler-editor-${index}`);
    if (!code) return;

    execResults.set(index, { running: true });
    renderOutput(index);

    try {
        const response = await apiFetch('/api/python/execute', jsonBody('POST', { content: code }));
        const data = await response.json();
        execResults.set(index, {
            running: false,
            success: data.success,
            stdout: data.stdout || '',
            error: data.error || '',
            duration_ms: data.duration_ms,
        });
    } catch (err: unknown) {
        execResults.set(index, {
            running: false,
            success: false,
            error: err instanceof Error ? err.message : String(err),
        });
    }
    renderOutput(index);
}

function renderOutput(index: number): void {
    if (!contentElement) return;
    const container = contentElement.querySelector<HTMLElement>(`#handler-output-${index}`);
    if (!container) return;

    const result = execResults.get(index);
    if (!result) {
        container.innerHTML = '';
        return;
    }

    if (result.running) {
        container.innerHTML = '<div class="handlers-output-running">Running...</div>';
        return;
    }

    const parts: string[] = [];
    if (result.stdout) {
        parts.push(`<pre class="handlers-output-text">${escapeHtml(result.stdout)}</pre>`);
    }
    if (result.error) {
        parts.push(`<pre class="handlers-output-error">${escapeHtml(result.error)}</pre>`);
    }
    if (result.duration_ms !== undefined) {
        parts.push(`<span class="handlers-output-duration">${result.duration_ms}ms</span>`);
    }
    container.innerHTML = parts.join('');
}

function destroyEditors(): void {
    for (const view of editorViews) {
        view.destroy();
    }
    editorViews = [];
}

async function mountEditors(): Promise<void> {
    if (!contentElement) return;

    const { EditorView } = await import('@codemirror/view');
    const { EditorState } = await import('@codemirror/state');
    const { python } = await import('@codemirror/lang-python');
    const { oneDark } = await import('@codemirror/theme-one-dark');

    const containers = contentElement.querySelectorAll<HTMLElement>('.handlers-card-editor[id]');
    for (const container of containers) {
        const code = codeStore.get(container.id) || '';
        const view = new EditorView({
            state: EditorState.create({
                doc: code,
                extensions: [
                    python(),
                    oneDark,
                    EditorView.lineWrapping,
                    EditorState.readOnly.of(true),
                    EditorView.editable.of(false),
                    EditorView.theme({
                        '&': { fontSize: '12px', maxHeight: '300px' },
                        '.cm-scroller': { overflow: 'auto' },
                        '.cm-gutters': { display: 'none' },
                        '.cm-content': { padding: '8px 0' },
                    }),
                ],
            }),
            parent: container,
        });
        editorViews.push(view);
    }
}

// A handler that says nothing about itself is a fact about it, not a blank.
function doc(code: string): string {
    const text = docComment(code);
    if (text === '') return `<span class="handlers-card-nodoc">no doc comment</span>`;
    return escapeHtml(text);
}

// What fires this, if anything. A handler with neither only runs by hand.
function wiring(g: HandlerGroup): string {
    if (firingFailure !== '') return `<span class="handlers-card-wiring">wiring unknown</span>`;
    const out: string[] = [];
    const s = scheduleOf(g);
    if (s) out.push(`<span class="handlers-card-wiring">every ${formatInterval(s.interval_seconds || 0)}</span>`);
    const w = watcherOf(g);
    if (w) out.push(`<span class="handlers-card-wiring">watch ${w.fire_count}×</span>`);
    return out.join('');
}

function facts(g: HandlerGroup, h: HandlerAttestation): string {
    const rows = [`filed ${formatDateTime(h.timestamp)}`, h.id];
    if (firingFailure !== '') {
        rows.push(`could not read what fires this: ${firingFailure}`);
        return `<div class="handlers-card-facts">${rows.map(r => `<div>${escapeHtml(r)}</div>`).join('')}</div>`;
    }
    const s = scheduleOf(g);
    if (s) rows.push(`schedule ${s.state} — last ran ${s.last_run_at || 'never'}, next ${s.next_run_at || 'unset'}`);
    const w = watcherOf(g);
    if (w) rows.push(`watch fired ${w.fire_count}× — last ${w.last_fired_at || 'never'}, ${w.error_count} errors`);
    if (w?.last_error) rows.push(`last error: ${w.last_error}`);
    if (!s && !w) rows.push('nothing fires this — it runs when you run it');
    return `<div class="handlers-card-facts">${rows.map(r => `<div>${escapeHtml(r)}</div>`).join('')}</div>`;
}

function renderCards(): string {
    if (fetchFailure !== '') {
        return `<div class="handlers-empty">Could not read handlers: ${escapeHtml(fetchFailure)}</div>`;
    }
    if (groups.length === 0) {
        return `<div class="handlers-empty">No handlers found</div>`;
    }

    codeStore.clear();
    execResults.clear();
    const cards = groups.map((g, i) => {
        const h = g.versions[g.selectedVersion];
        const name = escapeHtml(g.name || '(unnamed)');
        const context = g.context ? escapeHtml(g.context) : '';
        const code = h.attributes?.code || '';
        const editorId = `handler-editor-${i}`;
        codeStore.set(editorId, code);
        const label = context ? `${name} <span class="handlers-card-context">${context}</span>` : name;

        let dateHtml: string;
        if (g.versions.length > 1) {
            const options = g.versions.map((v, vi) =>
                `<option value="${vi}"${vi === g.selectedVersion ? ' selected' : ''}>${formatDateTime(v.timestamp)}</option>`
            ).join('');
            dateHtml = `<select class="handlers-version-select" data-group-index="${i}">${options}</select>`;
        } else {
            dateHtml = `<span class="handlers-card-date">${formatDate(h.timestamp)}</span>`;
        }

        return `<div class="handlers-card" data-group="${i}" data-openness="${g.openness}">
            <div class="handlers-card-header">
                <span class="handlers-card-label">${label}</span>
                ${wiring(g)}
                ${dateHtml}
                <button class="handlers-play-btn" data-action="execute" data-index="${i}" title="Execute handler">▶</button>
            </div>
            <div class="handlers-card-doc" data-action="open" data-index="${i}">${doc(code)}</div>
            ${g.openness >= 1 ? facts(g, h) : ''}
            ${g.openness >= 2 ? `<div class="handlers-card-editor" id="${editorId}"></div>` : ''}
            <div class="handlers-card-output" id="handler-output-${i}"></div>
        </div>`;
    }).join('');

    return `<div class="handlers-grid">${cards}</div>`;
}

function render(): void {
    if (!contentElement) return;
    destroyEditors();
    groupHandlers();
    contentElement.innerHTML = `
        <div class="handlers-panel">
            <div class="handlers-header">
                <span class="handlers-count">${groups.length}</span>
            </div>
            ${renderCards()}
        </div>
    `;
    mountEditors();
}

function renderWithGroups(): void {
    if (!contentElement) return;
    destroyEditors();
    contentElement.innerHTML = `
        <div class="handlers-panel">
            <div class="handlers-header">
                <span class="handlers-count">${groups.length}</span>
            </div>
            ${renderCards()}
        </div>
    `;
    mountEditors();
}

function attachEventDelegation(el: HTMLElement): void {
    el.addEventListener('change', (e) => {
        const target = e.target as HTMLElement;
        if (target.classList.contains('handlers-version-select')) {
            const select = target as HTMLSelectElement;
            const groupIndex = parseInt(select.dataset.groupIndex || '', 10);
            if (!isNaN(groupIndex) && groups[groupIndex]) {
                groups[groupIndex].selectedVersion = parseInt(select.value, 10);
                renderWithGroups();
            }
        }
    });

    el.addEventListener('click', async (e) => {
        const target = e.target as HTMLElement;
        const action = target.closest<HTMLElement>('[data-action]')?.dataset.action;
        if (!action) return;

        if (action === 'execute') {
            const index = parseInt(target.closest<HTMLElement>('[data-index]')?.dataset.index || '', 10);
            if (!isNaN(index)) {
                executeHandler(index);
            }
            return;
        }

        // One card opens further rather than opening a second thing, so the
        // question "what is this" and "how does it do it" stay one object.
        if (action === 'open') {
            const index = parseInt(target.closest<HTMLElement>('[data-index]')?.dataset.index || '', 10);
            const g = groups[index];
            if (g) {
                g.openness = ((g.openness + 1) % 3) as 0 | 1 | 2;
                renderWithGroups();
            }
        }
    });
}

export function createHandlersGlyph(): Glyph {
    return {
        id: 'handlers-glyph',
        title: 'Handlers',
        manifestationType: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;

            attachEventDelegation(content);
            render();
            fetchHandlers().then(() => render());
            fetchFiring().then(() => render());

            const cleanupInterval = setInterval(() => {
                if (!contentElement?.isConnected) {
                    clearInterval(cleanupInterval);
                    destroyEditors();
                    contentElement = null;
                }
            }, 2000);

            return content;
        },
    };
}
