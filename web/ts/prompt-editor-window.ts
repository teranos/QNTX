/**
 * Prompt Editor Window - Interactive ax→prompt testing
 *
 * Provides a UI for building and testing ax→prompt pipelines:
 * - ax query input to select attestations
 * - Template editor with {{field}} autocomplete
 * - Provider/model selection (OpenRouter/Ollama)
 * - One-shot execution for testing
 * - Result display
 */

import { Window } from './components/window.ts';
import { apiFetch } from './api.ts';
import { AX, SO } from '@generated/sym.js';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';

// Template field autocomplete options
const TEMPLATE_FIELDS = [
    { field: 'subject', description: 'First subject or comma-joined' },
    { field: 'subjects', description: 'All subjects as JSON array' },
    { field: 'predicate', description: 'First predicate or comma-joined' },
    { field: 'predicates', description: 'All predicates as JSON array' },
    { field: 'context', description: 'First context or comma-joined' },
    { field: 'contexts', description: 'All contexts as JSON array' },
    { field: 'actor', description: 'First actor or comma-joined' },
    { field: 'actors', description: 'All actors as JSON array' },
    { field: 'temporal', description: 'ISO8601 timestamp' },
    { field: 'id', description: 'Attestation ID' },
    { field: 'source', description: 'Attestation source' },
    { field: 'attributes', description: 'Full attributes as JSON' },
];

interface PromptResult {
    source_attestation_id: string;
    prompt: string;
    response: string;
    usage?: {
        prompt_tokens: number;
        completion_tokens: number;
        total_tokens: number;
    };
}

interface ExecuteResponse {
    results: PromptResult[];
    attestation_count: number;
    error?: string;
}

class PromptEditorPanel {
    private window: Window;
    private isExecuting: boolean = false;

    constructor() {
        this.window = new Window({
            id: 'prompt-editor-window',
            title: `${AX} ${SO} Prompt Editor`,
            width: '700px',
            height: '600px',
            onShow: () => this.onShow(),
        });

        this.setupContent();
    }

    private setupContent(): void {
        const content = `
            <div class="prompt-editor-content">
                <div class="prompt-editor-section">
                    <label class="prompt-editor-label">
                        ${AX} Ax Query
                        <span class="prompt-editor-hint">Select attestations to process</span>
                    </label>
                    <input
                        type="text"
                        id="prompt-ax-query"
                        class="prompt-editor-input"
                        placeholder="e.g., ALICE speaks english by system"
                    />
                </div>

                <div class="prompt-editor-section">
                    <label class="prompt-editor-label">
                        System Prompt
                        <span class="prompt-editor-hint">Optional instructions for the LLM</span>
                    </label>
                    <textarea
                        id="prompt-system"
                        class="prompt-editor-textarea"
                        rows="2"
                        placeholder="e.g., You are a helpful assistant. Be concise."
                    ></textarea>
                </div>

                <div class="prompt-editor-section">
                    <label class="prompt-editor-label">
                        Template
                        <span class="prompt-editor-hint">Use {{field}} placeholders</span>
                    </label>
                    <div class="prompt-template-container">
                        <textarea
                            id="prompt-template"
                            class="prompt-editor-textarea prompt-template-editor"
                            rows="4"
                            placeholder="e.g., Summarize what {{subject}} {{predicate}} in the context of {{context}}"
                        ></textarea>
                        <div id="prompt-field-suggestions" class="prompt-field-suggestions hidden">
                            ${TEMPLATE_FIELDS.map(f => `
                                <button class="prompt-field-btn" data-field="${f.field}">
                                    <span class="field-name">{{${f.field}}}</span>
                                    <span class="field-desc">${f.description}</span>
                                </button>
                            `).join('')}
                        </div>
                    </div>
                </div>

                <div class="prompt-editor-section prompt-editor-row">
                    <div class="prompt-editor-col">
                        <label class="prompt-editor-label">Provider</label>
                        <select id="prompt-provider" class="prompt-editor-select">
                            <option value="">Default (from config)</option>
                            <option value="openrouter">OpenRouter (Cloud)</option>
                            <option value="local">Ollama (Local)</option>
                        </select>
                    </div>
                    <div class="prompt-editor-col">
                        <label class="prompt-editor-label">Model</label>
                        <input
                            type="text"
                            id="prompt-model"
                            class="prompt-editor-input"
                            placeholder="e.g., openai/gpt-4o-mini"
                        />
                    </div>
                </div>

                <div class="prompt-editor-actions">
                    <button id="prompt-execute-btn" class="prompt-execute-btn">
                        <span class="btn-icon">▶</span>
                        <span class="btn-text">Execute</span>
                    </button>
                    <button id="prompt-preview-btn" class="prompt-preview-btn">
                        Preview Query
                    </button>
                    <span id="prompt-status" class="prompt-status"></span>
                </div>

                <div class="prompt-editor-section">
                    <label class="prompt-editor-label">
                        Results
                        <span id="prompt-result-count" class="prompt-editor-hint"></span>
                    </label>
                    <div id="prompt-results" class="prompt-results">
                        <div class="prompt-results-empty">
                            Execute a prompt to see results here
                        </div>
                    </div>
                </div>
            </div>
        `;

        this.window.setContent(content);
        this.setupEventListeners();
    }

    private setupEventListeners(): void {
        const windowEl = this.window.getElement();

        // Execute button
        const executeBtn = windowEl.querySelector('#prompt-execute-btn');
        executeBtn?.addEventListener('click', () => this.executePrompt());

        // Preview button
        const previewBtn = windowEl.querySelector('#prompt-preview-btn');
        previewBtn?.addEventListener('click', () => this.previewQuery());

        // Template field insertion
        const fieldBtns = windowEl.querySelectorAll('.prompt-field-btn');
        fieldBtns.forEach(btn => {
            btn.addEventListener('click', (e) => {
                const field = (e.currentTarget as HTMLElement).dataset.field;
                if (field) {
                    this.insertField(field);
                }
            });
        });

        // Template textarea focus for showing suggestions
        const templateInput = windowEl.querySelector('#prompt-template');
        const suggestions = windowEl.querySelector('#prompt-field-suggestions');

        templateInput?.addEventListener('focus', () => {
            suggestions?.classList.remove('hidden');
        });

        // Keep suggestions visible when clicking on them
        suggestions?.addEventListener('mousedown', (e) => {
            e.preventDefault();
        });

        // Hide suggestions when clicking outside
        document.addEventListener('click', (e) => {
            const target = e.target as HTMLElement;
            if (!target.closest('.prompt-template-container')) {
                suggestions?.classList.add('hidden');
            }
        });

        // Keyboard shortcuts
        windowEl.addEventListener('keydown', (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault();
                this.executePrompt();
            }
        });
    }

    private insertField(field: string): void {
        const windowEl = this.window.getElement();
        const templateInput = windowEl.querySelector<HTMLTextAreaElement>('#prompt-template');
        if (!templateInput) return;

        const start = templateInput.selectionStart;
        const end = templateInput.selectionEnd;
        const text = templateInput.value;
        const insertion = `{{${field}}}`;

        templateInput.value = text.substring(0, start) + insertion + text.substring(end);
        templateInput.selectionStart = templateInput.selectionEnd = start + insertion.length;
        templateInput.focus();
    }

    private async previewQuery(): Promise<void> {
        const windowEl = this.window.getElement();
        const axQuery = windowEl.querySelector<HTMLInputElement>('#prompt-ax-query')?.value || '';

        if (!axQuery.trim()) {
            this.updateStatus('Enter an ax query first', 'warning');
            return;
        }

        this.updateStatus('Previewing query...', 'info');

        try {
            const response = await apiFetch('/api/prompt/preview', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ax_query: axQuery }),
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data = await response.json();
            this.updateStatus(`Found ${data.attestation_count} attestation(s)`, 'success');

            // Show preview in results
            const resultsEl = windowEl.querySelector('#prompt-results');
            if (resultsEl && data.attestations?.length > 0) {
                resultsEl.innerHTML = data.attestations.map((as: any) => `
                    <div class="prompt-result-item prompt-preview-item">
                        <div class="result-header">
                            <span class="result-id">${as.id}</span>
                        </div>
                        <div class="result-preview">
                            <strong>Subject:</strong> ${as.subjects?.join(', ') || '_'}<br>
                            <strong>Predicate:</strong> ${as.predicates?.join(', ') || '_'}<br>
                            <strong>Context:</strong> ${as.contexts?.join(', ') || '_'}<br>
                            <strong>Actor:</strong> ${as.actors?.join(', ') || '_'}
                        </div>
                    </div>
                `).join('');
            }
        } catch (error) {
            handleError(error, 'Preview failed', { context: SEG.AX, silent: true });
            this.updateStatus(`Preview failed: ${error instanceof Error ? error.message : 'Unknown error'}`, 'error');
        }
    }

    private async executePrompt(): Promise<void> {
        if (this.isExecuting) return;

        const windowEl = this.window.getElement();
        const axQuery = windowEl.querySelector<HTMLInputElement>('#prompt-ax-query')?.value || '';
        const systemPrompt = windowEl.querySelector<HTMLTextAreaElement>('#prompt-system')?.value || '';
        const template = windowEl.querySelector<HTMLTextAreaElement>('#prompt-template')?.value || '';
        const provider = windowEl.querySelector<HTMLSelectElement>('#prompt-provider')?.value || '';
        const model = windowEl.querySelector<HTMLInputElement>('#prompt-model')?.value || '';

        // Validation
        if (!axQuery.trim()) {
            this.updateStatus('Enter an ax query', 'warning');
            return;
        }
        if (!template.trim()) {
            this.updateStatus('Enter a template', 'warning');
            return;
        }

        this.isExecuting = true;
        this.setExecuting(true);
        this.updateStatus('Executing prompt...', 'info');

        try {
            const response = await apiFetch('/api/prompt/execute', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    ax_query: axQuery,
                    template: template,
                    system_prompt: systemPrompt,
                    provider: provider || undefined,
                    model: model || undefined,
                }),
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data: ExecuteResponse = await response.json();
            this.displayResults(data);
            this.updateStatus(`Processed ${data.attestation_count} attestation(s)`, 'success');

        } catch (error) {
            handleError(error, 'Prompt execution failed', { context: SEG.AX, silent: true });
            this.updateStatus(`Failed: ${error instanceof Error ? error.message : 'Unknown error'}`, 'error');
        } finally {
            this.isExecuting = false;
            this.setExecuting(false);
        }
    }

    private displayResults(data: ExecuteResponse): void {
        const windowEl = this.window.getElement();
        const resultsEl = windowEl.querySelector('#prompt-results');
        const countEl = windowEl.querySelector('#prompt-result-count');

        if (countEl) {
            countEl.textContent = `${data.results?.length || 0} result(s)`;
        }

        if (!resultsEl) return;

        if (!data.results || data.results.length === 0) {
            resultsEl.innerHTML = '<div class="prompt-results-empty">No results</div>';
            return;
        }

        resultsEl.innerHTML = data.results.map((result, i) => `
            <div class="prompt-result-item">
                <div class="result-header">
                    <span class="result-index">#${i + 1}</span>
                    <span class="result-id">${result.source_attestation_id}</span>
                    ${result.usage ? `
                        <span class="result-tokens">${result.usage.total_tokens} tokens</span>
                    ` : ''}
                </div>
                <details class="result-prompt-details">
                    <summary>Prompt</summary>
                    <pre class="result-prompt">${this.escapeHtml(result.prompt)}</pre>
                </details>
                <div class="result-response">
                    <div class="result-response-label">Response:</div>
                    <div class="result-response-content">${this.escapeHtml(result.response)}</div>
                </div>
            </div>
        `).join('');
    }

    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    private setExecuting(executing: boolean): void {
        const windowEl = this.window.getElement();
        const btn = windowEl.querySelector('#prompt-execute-btn');
        const btnText = btn?.querySelector('.btn-text');
        const btnIcon = btn?.querySelector('.btn-icon');

        if (executing) {
            btn?.classList.add('executing');
            if (btnText) btnText.textContent = 'Executing...';
            if (btnIcon) btnIcon.textContent = '⏳';
        } else {
            btn?.classList.remove('executing');
            if (btnText) btnText.textContent = 'Execute';
            if (btnIcon) btnIcon.textContent = '▶';
        }
    }

    private updateStatus(message: string, type: 'info' | 'success' | 'warning' | 'error'): void {
        const windowEl = this.window.getElement();
        const statusEl = windowEl.querySelector('#prompt-status');
        if (statusEl) {
            statusEl.textContent = message;
            statusEl.className = `prompt-status status-${type}`;

            // Auto-clear success/info messages
            if (type === 'success' || type === 'info') {
                setTimeout(() => {
                    if (statusEl.textContent === message) {
                        statusEl.textContent = '';
                        statusEl.className = 'prompt-status';
                    }
                }, 5000);
            }
        }
    }

    private async onShow(): Promise<void> {
        log.debug(SEG.AX, 'Prompt editor window shown');
    }

    public toggle(): void {
        this.window.toggle();
    }

    public show(): void {
        this.window.show();
    }

    public hide(): void {
        this.window.hide();
    }
}

// Singleton instance
let promptEditorPanel: PromptEditorPanel | null = null;

export function togglePromptEditor(): void {
    if (!promptEditorPanel) {
        promptEditorPanel = new PromptEditorPanel();
    }
    promptEditorPanel.toggle();
}

export function showPromptEditor(): void {
    if (!promptEditorPanel) {
        promptEditorPanel = new PromptEditorPanel();
    }
    promptEditorPanel.show();
}

export {};
