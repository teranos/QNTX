/**
 * Handlers Panel - Python handler management
 *
 * Manifests as a panel glyph. Displays handler attestations
 * (predicate=handler) as code cards with syntax highlighting.
 */

import { apiFetch } from './api';
import { escapeHtml } from './html-utils';
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
}

// Module-level state
let contentElement: HTMLElement | null = null;
let handlers: HandlerAttestation[] = [];
let groups: HandlerGroup[] = [];
let editorViews: any[] = [];
let codeStore: Map<string, string> = new Map();
let execResults: Map<number, ExecutionResult> = new Map();

async function fetchHandlers(): Promise<void> {
    try {
        const response = await apiFetch('/api/attestations?predicate=handler&limit=100');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${await response.text()}`);
        }
        handlers = await response.json();
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Handlers] Failed to fetch handlers:', error);
        handlers = [];
    }
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
        const response = await apiFetch('/api/python/execute', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: code }),
        });
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

function renderCards(): string {
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

        return `<div class="handlers-card" data-group="${i}">
            <div class="handlers-card-header">
                <span class="handlers-card-label">${label}</span>
                ${dateHtml}
                <button class="handlers-play-btn" data-action="execute" data-index="${i}" title="Execute handler">▶</button>
            </div>
            <div class="handlers-card-editor" id="${editorId}"></div>
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
                <h2>Handlers</h2>
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
                <h2>Handlers</h2>
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
