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

// Module-level state
let contentElement: HTMLElement | null = null;
let handlers: HandlerAttestation[] = [];
let showCreateForm = false;
let editorViews: any[] = [];
let codeStore: Map<string, string> = new Map();

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

async function createHandler(name: string, code: string, context: string): Promise<void> {
    const response = await apiFetch('/api/attestations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            subjects: [name],
            predicates: ['handler'],
            contexts: context ? [context] : [],
            actors: ['user'],
            attributes: { code },
        }),
    });
    if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${await response.text()}`);
    }
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
    if (handlers.length === 0) {
        return `<div class="handlers-empty">No handlers found</div>`;
    }

    codeStore.clear();
    const cards = handlers.map((h, i) => {
        const name = escapeHtml(h.subjects[0] || '(unnamed)');
        const context = h.contexts[0] ? escapeHtml(h.contexts[0]) : '';
        const code = h.attributes?.code || '';
        const editorId = `handler-editor-${i}`;
        codeStore.set(editorId, code);
        const label = context ? `${name} <span class="handlers-card-context">${context}</span>` : name;

        return `<div class="handlers-card">
            <div class="handlers-card-header">${label}</div>
            <div class="handlers-card-editor" id="${editorId}"></div>
        </div>`;
    }).join('');

    return `<div class="handlers-grid">${cards}</div>`;
}

function renderCreateForm(): string {
    if (!showCreateForm) {
        return `<button class="handlers-create-btn" data-action="show-form">+ New Handler</button>`;
    }

    return `<div class="handlers-form">
        <div class="handlers-form-row">
            <label>Name</label>
            <input type="text" id="handler-name" placeholder="my-handler" autocomplete="off" />
        </div>
        <div class="handlers-form-row">
            <label>Context</label>
            <input type="text" id="handler-context" placeholder="icpy" value="icpy" autocomplete="off" />
        </div>
        <div class="handlers-form-row">
            <label>Code</label>
            <textarea id="handler-code" rows="6" placeholder="print('hello')"></textarea>
        </div>
        <div class="handlers-form-actions">
            <button class="handlers-btn handlers-btn-primary" data-action="create">Create</button>
            <button class="handlers-btn handlers-btn-secondary" data-action="cancel">Cancel</button>
        </div>
        <div id="handler-form-error" class="handlers-form-error"></div>
    </div>`;
}

function render(): void {
    if (!contentElement) return;
    destroyEditors();
    contentElement.innerHTML = `
        <div class="handlers-panel">
            <div class="handlers-header">
                <h2>Handlers</h2>
                <span class="handlers-count">${handlers.length}</span>
            </div>
            ${renderCreateForm()}
            ${renderCards()}
        </div>
    `;
    mountEditors();
}

function attachEventDelegation(el: HTMLElement): void {
    el.addEventListener('click', async (e) => {
        const target = e.target as HTMLElement;
        const action = target.closest<HTMLElement>('[data-action]')?.dataset.action;
        if (!action) return;

        if (action === 'show-form') {
            showCreateForm = true;
            render();
            return;
        }

        if (action === 'cancel') {
            showCreateForm = false;
            render();
            return;
        }

        if (action === 'create') {
            const nameInput = el.querySelector<HTMLInputElement>('#handler-name');
            const contextInput = el.querySelector<HTMLInputElement>('#handler-context');
            const codeInput = el.querySelector<HTMLTextAreaElement>('#handler-code');
            const errorDiv = el.querySelector<HTMLElement>('#handler-form-error');

            const name = nameInput?.value.trim() || '';
            const context = contextInput?.value.trim() || '';
            const code = codeInput?.value || '';

            if (!name) {
                if (errorDiv) errorDiv.textContent = 'Name is required';
                return;
            }
            if (!code) {
                if (errorDiv) errorDiv.textContent = 'Code is required';
                return;
            }

            try {
                await createHandler(name, code, context);
                showCreateForm = false;
                await fetchHandlers();
                render();
            } catch (error: unknown) {
                const msg = error instanceof Error ? error.message : String(error);
                if (errorDiv) errorDiv.textContent = msg;
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

            const cleanupInterval = setInterval(() => {
                if (!contentElement?.isConnected) {
                    clearInterval(cleanupInterval);
                    destroyEditors();
                    contentElement = null;
                    showCreateForm = false;
                }
            }, 2000);

            return content;
        },
    };
}
