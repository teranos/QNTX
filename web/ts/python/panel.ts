/**
 * Python Editor Panel - Python code execution with PyO3-based plugin
 *
 * Provides a code editor for writing and executing Python code via
 * the qntx-python-plugin gRPC service.
 */

import { BasePanel } from '../base-panel.ts';
import { apiFetch } from '../api.ts';
import { createRichErrorState, type RichError } from '../base-panel-error.ts';
import { escapeHtml } from '../html-utils.ts';
import { log, SEG } from '../logger.ts';
import { handleError } from '../error-handler.ts';
import { buttonPlaceholder, hydrateButtons, type Button } from '../components/button.ts';
import type { EditorView } from '@codemirror/view';

// Status type for plugin connection
type PluginStatus = 'connecting' | 'ready' | 'error' | 'unavailable';

// Status configuration
const STATUS_CONFIG: Record<PluginStatus, { message: string; className: string }> = {
    connecting: { message: 'connecting...', className: 'python-status-connecting' },
    ready: { message: 'ready', className: 'python-status-ready' },
    error: { message: 'error', className: 'python-status-error' },
    unavailable: { message: 'unavailable', className: 'python-status-unavailable' }
};

// Execution result from the plugin
interface ExecutionResult {
    success: boolean;
    stdout: string;
    stderr: string;
    result: unknown;
    error: string | null;
    duration_ms: number;
    variables?: Record<string, string>;
}

class PythonEditorPanel extends BasePanel {
    private editor: EditorView | null = null;
    private currentTab: 'editor' | 'output' | null = null;
    private lastOutput: ExecutionResult | null = null;
    private isExecuting: boolean = false;
    private pythonVersion: string = '';

    // Hydrated execute button (confirmation handled by Button component)
    private executeButton: Button | null = null;

    // Event handler references for cleanup
    private executeHandler: ((e: KeyboardEvent) => void) | null = null;
    private closeBtnHandler: (() => void) | null = null;
    private tabClickHandlers: Map<Element, (e: Event) => void> = new Map();

    constructor() {
        super({
            id: 'python-editor-panel',
            classes: ['prose-panel'],
            useOverlay: true,
            closeOnEscape: true
        });
    }

    protected getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon python-editor-icon">py</span>
                    <span class="prose-name">Python</span>
                    <span class="python-version"></span>
                </div>
                <button class="prose-close python-editor-close" aria-label="Close">✕</button>
            </div>
            <div class="python-editor-tabs">
                <button class="python-editor-tab active" data-tab="editor">Editor</button>
                <button class="python-editor-tab" data-tab="output">Output</button>
            </div>
            <div class="prose-body">
                <div class="prose-sidebar" id="tab-sidebar">
                    <!-- Sidebar content dynamically rendered here -->
                </div>
                <div class="prose-content" id="tab-content">
                    <!-- Tab content dynamically rendered here -->
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Initialize maps if not already initialized (field initializers run after super())
        if (!this.tabClickHandlers) {
            this.tabClickHandlers = new Map();
        }

        // Close button
        const closeBtn = this.$('.python-editor-close');
        if (closeBtn) {
            this.closeBtnHandler = () => this.hide();
            closeBtn.addEventListener('click', this.closeBtnHandler);
        }

        // Tab switching
        const tabs = this.panel?.querySelectorAll('.python-editor-tab');
        tabs?.forEach(tab => {
            const handler = (e: Event) => {
                const target = e.target as HTMLElement;
                const tabName = target.dataset.tab as 'editor' | 'output';
                this.switchTab(tabName);
            };
            this.tabClickHandlers.set(tab, handler);
            tab.addEventListener('click', handler);
        });

        // Execute on Cmd/Ctrl+Enter
        this.executeHandler = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && this.isVisible) {
                e.preventDefault();
                this.executeCode();
            }
        };
        document.addEventListener('keydown', this.executeHandler);
    }

    protected async onShow(): Promise<void> {
        // Check plugin status
        await this.checkPluginStatus();

        // Initialize editor tab
        this.switchTab('editor');

        log.debug(SEG.UI, 'Python Editor panel shown');
    }

    protected onHide(): void {
        this.destroyEditor();
        log.debug(SEG.UI, 'Python Editor panel closed');
    }

    protected onDestroy(): void {
        this.destroyEditor();

        // Clean up execute button
        if (this.executeButton) {
            this.executeButton.destroy();
            this.executeButton = null;
        }

        // Clean up keyboard handler
        if (this.executeHandler) {
            document.removeEventListener('keydown', this.executeHandler);
            this.executeHandler = null;
        }

        // Clean up close button handler
        if (this.closeBtnHandler) {
            const closeBtn = this.$('.python-editor-close');
            if (closeBtn) {
                closeBtn.removeEventListener('click', this.closeBtnHandler);
            }
            this.closeBtnHandler = null;
        }

        // Clean up tab click handlers
        this.tabClickHandlers.forEach((handler, element) => {
            element.removeEventListener('click', handler);
        });
        this.tabClickHandlers.clear();
    }

    private async checkPluginStatus(): Promise<void> {
        this.updateStatus('connecting');

        try {
            const response = await apiFetch('/api/python/version');
            if (response.ok) {
                const data = await response.json();
                this.pythonVersion = data.python_version || 'unknown';
                this.updateStatus('ready');

                // Update version display
                const versionEl = this.$('.python-version');
                if (versionEl) {
                    versionEl.textContent = `Python ${this.pythonVersion.split(' ')[0]}`;
                }
            } else {
                this.updateStatus('unavailable');
            }
        } catch (error: unknown) {
            handleError(error, 'Failed to check Python plugin status', { context: SEG.UI, silent: true });
            this.updateStatus('error');
        }
    }

    private async initializeEditor(content: string = ''): Promise<void> {
        log.debug(SEG.UI, 'initializeEditor called with content length:', content.length);

        if (this.editor) {
            this.editor.destroy();
            this.editor = null;
        }

        try {
            log.debug(SEG.UI, 'Loading CodeMirror modules...');
            const { EditorView, keymap } = await import('@codemirror/view');
            const { EditorState } = await import('@codemirror/state');
            const { defaultKeymap } = await import('@codemirror/commands');
            const { oneDark } = await import('@codemirror/theme-one-dark');
            log.debug(SEG.UI, 'CodeMirror core modules loaded');

            // Import Python language support
            let pythonExtension;
            try {
                log.debug(SEG.UI, 'Loading Python language support...');
                const pythonModule = await import('@codemirror/lang-python');
                pythonExtension = pythonModule.python();
                log.debug(SEG.UI, 'Python language support loaded');
            } catch (error: unknown) {
                handleError(error, 'Failed to load Python language support', { context: SEG.UI, silent: true });
                pythonExtension = [];
            }

            log.debug(SEG.UI, 'Looking for container #python-editor-container...');
            const container = this.$('#python-editor-container');
            if (!container) {
                const allContainers = document.querySelectorAll('[id*="python"]');
                log.error(SEG.UI, 'Container not found! Available python-related elements:',
                    Array.from(allContainers).map(el => el.id));
                throw new Error('Editor container not found');
            }
            log.debug(SEG.UI, 'Container found:', container.id, 'classList:', Array.from(container.classList));

            log.debug(SEG.UI, 'Creating EditorView...');
            this.editor = new EditorView({
                state: EditorState.create({
                    doc: content || this.getDefaultCode(),
                    extensions: [
                        keymap.of(defaultKeymap),
                        pythonExtension,
                        oneDark,
                        EditorView.lineWrapping
                        // Note: Confirmation reset handled by Button component timeout
                    ]
                }),
                parent: container
            });

            log.info(SEG.UI, 'Python Editor initialized successfully');
        } catch (error: unknown) {
            handleError(error, 'Failed to initialize Python Editor', { context: SEG.UI, silent: true });
            this.showError(error instanceof Error ? error.message : String(error));
        }
    }

    private getDefaultCode(): string {
        return `# Python code editor
# Press Cmd/Ctrl+Enter to execute

print("Hello from QNTX Python!")

# Example: Calculate something
result = sum(range(1, 11))
print(f"Sum of 1-10: {result}")

# Set _result to return a value
_result = {"message": "Hello", "numbers": [1, 2, 3]}
`;
    }

    /**
     * Execute code via keyboard shortcut - triggers the hydrated button
     */
    async executeCode(): Promise<void> {
        if (!this.editor || this.isExecuting) return;

        // Click the hydrated button to trigger its confirmation flow
        if (this.executeButton) {
            this.executeButton.element.click();
        }
    }

    /**
     * Direct code execution (called by Button after confirmation)
     */
    private async executeCodeDirect(): Promise<void> {
        if (!this.editor) return;

        this.isExecuting = true;

        try {
            const code = this.editor.state.doc.toString();

            const response = await apiFetch('/api/python/execute', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    content: code,
                    capture_variables: true
                })
            });

            if (!response.ok) {
                throw new Error(`Execution failed: ${response.statusText}`);
            }

            const result: ExecutionResult = await response.json();
            this.lastOutput = result;
            this.updateOutput(result);

            // Auto-switch to output tab
            this.switchTab('output');

            log.debug(SEG.UI, 'Python code executed:', result.success ? 'success' : 'error');
        } catch (error: unknown) {
            handleError(error, 'Python execution error', { context: SEG.UI, silent: true });
            this.lastOutput = {
                success: false,
                stdout: '',
                stderr: '',
                result: null,
                error: error instanceof Error ? error.message : String(error),
                duration_ms: 0
            };
            this.updateOutput(this.lastOutput);
            this.switchTab('output');
            throw error; // Re-throw so Button shows error state
        } finally {
            this.isExecuting = false;
        }
    }

    private updateOutput(result: ExecutionResult): void {
        const outputEl = this.$('#python-output-content');
        if (!outputEl) return;

        const statusClass = result.success ? 'output-success' : 'output-error';
        const statusText = result.success ? 'Success' : 'Error';

        let html = `
            <div class="output-header ${statusClass}">
                <span class="output-status">${statusText}</span>
                <span class="output-duration">${result.duration_ms}ms</span>
            </div>
        `;

        if (result.stdout) {
            html += `
                <div class="output-section">
                    <div class="output-label">stdout:</div>
                    <pre class="output-content">${escapeHtml(result.stdout)}</pre>
                </div>
            `;
        }

        if (result.stderr) {
            html += `
                <div class="output-section output-stderr">
                    <div class="output-label">stderr:</div>
                    <pre class="output-content">${escapeHtml(result.stderr)}</pre>
                </div>
            `;
        }

        if (result.error) {
            html += `
                <div class="output-section output-error-section">
                    <div class="output-label">Error:</div>
                    <pre class="output-content output-error-text">${escapeHtml(result.error)}</pre>
                </div>
            `;
        }

        if (result.result !== null && result.result !== undefined) {
            html += `
                <div class="output-section">
                    <div class="output-label">Result:</div>
                    <pre class="output-content output-result">${escapeHtml(JSON.stringify(result.result, null, 2))}</pre>
                </div>
            `;
        }

        if (result.variables && Object.keys(result.variables).length > 0) {
            html += `
                <div class="output-section">
                    <div class="output-label">Variables:</div>
                    <div class="output-variables">
                        ${Object.entries(result.variables).map(([k, v]) =>
                            `<div class="var-item"><span class="var-name">${escapeHtml(k)}</span> = <span class="var-value">${escapeHtml(v)}</span></div>`
                        ).join('')}
                    </div>
                </div>
            `;
        }

        outputEl.innerHTML = html;
    }

    // Note: updateExecuteButton removed - Button component handles its own state

    updateStatus(status: PluginStatus): void {
        const statusEl = this.$('#python-status');
        if (!statusEl) return;

        const config = STATUS_CONFIG[status];

        Object.values(STATUS_CONFIG).forEach(cfg => {
            statusEl.classList.remove(cfg.className);
        });

        statusEl.classList.add(config.className);
        statusEl.textContent = config.message;
    }

    /**
     * Build rich error for Python editor context
     */
    private buildPythonError(error: unknown, context?: string): RichError {
        const errorMessage = error instanceof Error ? error.message : String(error);
        const errorStack = error instanceof Error ? error.stack : undefined;

        // Check for plugin unavailable errors
        if (errorMessage.includes('unavailable') || errorMessage.includes('plugin')) {
            return {
                title: 'Python Plugin Unavailable',
                message: 'The Python execution plugin is not available',
                suggestion: 'Check that the qntx-python-plugin is installed and running. You may need to enable it in your configuration.',
                details: errorStack || errorMessage
            };
        }

        // Check for execution errors
        if (errorMessage.includes('Execution failed') || errorMessage.includes('execute')) {
            return {
                title: 'Execution Failed',
                message: context || 'Failed to execute Python code',
                suggestion: 'Check your code for syntax errors or exceptions. The Python plugin logs may have more details.',
                details: errorStack || errorMessage
            };
        }

        // Check for HTTP errors
        const httpMatch = errorMessage.match(/HTTP\s*(\d{3})/i);
        if (httpMatch) {
            const status = parseInt(httpMatch[1], 10);
            if (status === 404) {
                return {
                    title: 'Endpoint Not Found',
                    message: 'The Python API endpoint is not available',
                    status: 404,
                    suggestion: 'Ensure the QNTX server is running with Python support enabled.',
                    details: errorStack || errorMessage
                };
            }
            if (status === 503) {
                return {
                    title: 'Service Unavailable',
                    message: 'The Python service is temporarily unavailable',
                    status: 503,
                    suggestion: 'The Python plugin may be starting up. Try again in a few seconds.',
                    details: errorStack || errorMessage
                };
            }
            if (status >= 500) {
                return {
                    title: 'Server Error',
                    message: 'The server encountered an error processing Python code',
                    status: status,
                    suggestion: 'Check the server logs for more details.',
                    details: errorStack || errorMessage
                };
            }
        }

        // Check for network errors
        if (errorMessage.includes('NetworkError') || errorMessage.includes('Failed to fetch')) {
            return {
                title: 'Network Error',
                message: 'Unable to connect to the Python service',
                suggestion: 'Check your network connection and ensure the QNTX server is running.',
                details: errorStack || errorMessage
            };
        }

        // Check for editor initialization errors
        if (errorMessage.includes('container not found') || errorMessage.includes('CodeMirror')) {
            return {
                title: 'Editor Initialization Failed',
                message: 'Failed to initialize the Python code editor',
                suggestion: 'Try refreshing the page or closing and reopening the editor.',
                details: errorStack || errorMessage
            };
        }

        // Generic error
        return {
            title: 'Error',
            message: errorMessage,
            suggestion: 'Check the error details for more information.',
            details: errorStack || errorMessage
        };
    }

    private showError(message: string, context?: string): void {
        const container = this.$('#python-editor-container');
        if (container) {
            container.innerHTML = '';
            const richError = this.buildPythonError(new Error(message), context);
            const errorEl = createRichErrorState(richError, async () => {
                // Retry initializing the editor
                await this.initializeEditor();
            });
            errorEl.classList.add('python-editor-error');
            container.appendChild(errorEl);
        }
    }

    destroyEditor(): void {
        if (this.editor) {
            try {
                this.editor.destroy();
            } catch (error: unknown) {
                handleError(error, 'Error destroying Python Editor', { context: SEG.UI, silent: true });
            }
            this.editor = null;
        }
    }

    private getEditorSidebarTemplate(): string {
        return `
            <div class="python-sidebar-content">
                <div class="python-sidebar-section">
                    <h4>Quick Actions</h4>
                    ${buttonPlaceholder('python-execute', 'Run (⌘↵)', 'python-action-btn')}
                    <button id="python-clear-btn" class="python-action-btn secondary">Clear</button>
                </div>
                <div class="python-sidebar-section">
                    <h4>Status</h4>
                    <div class="python-status-row">
                        Plugin: <span id="python-status" class="python-status-connecting">connecting...</span>
                    </div>
                </div>
                <div class="python-sidebar-section">
                    <h4>Examples</h4>
                    <button class="python-example-btn" data-example="hello">Hello World</button>
                    <button class="python-example-btn" data-example="math">Math</button>
                    <button class="python-example-btn" data-example="json">JSON</button>
                </div>
            </div>
        `;
    }

    private getEditorContentTemplate(): string {
        return `
            <div class="python-editor-info">
                <span>🐍 Python Editor</span>
                <span class="python-hint">Press ⌘+Enter to execute</span>
            </div>
            <div class="prose-editor-container">
                <div id="python-editor-container" class="python-editor-container">
                    <!-- CodeMirror editor will be initialized here -->
                </div>
            </div>
        `;
    }

    private getOutputSidebarTemplate(): string {
        return `
            <div class="python-sidebar-content">
                <div class="python-sidebar-section">
                    <h4>Actions</h4>
                    <button id="python-back-btn" class="python-action-btn">← Back to Editor</button>
                    <button id="python-copy-btn" class="python-action-btn secondary">Copy Output</button>
                </div>
            </div>
        `;
    }

    private getOutputContentTemplate(): string {
        return `
            <div id="python-output-content" class="python-output-content">
                <div class="no-output">No output yet. Run some code!</div>
            </div>
        `;
    }

    private bindSidebarEvents(): void {
        // Hydrate execute button with confirmation
        const sidebar = this.$('#tab-sidebar');
        if (sidebar) {
            const buttons = hydrateButtons(sidebar as HTMLElement, {
                'python-execute': {
                    label: 'Run (⌘↵)',
                    onClick: async () => {
                        await this.executeCodeDirect();
                    },
                    variant: 'default',
                    confirmation: {
                        label: 'Confirm Execute',
                        timeout: 5000
                    }
                }
            });
            this.executeButton = buttons['python-execute'] || null;
        }

        // Clear button
        const clearBtn = this.$('#python-clear-btn');
        clearBtn?.addEventListener('click', () => {
            if (this.editor) {
                this.editor.dispatch({
                    changes: { from: 0, to: this.editor.state.doc.length, insert: '' }
                });
            }
        });

        // Example buttons
        const exampleBtns = this.panel?.querySelectorAll('.python-example-btn');
        exampleBtns?.forEach(btn => {
            btn.addEventListener('click', (e) => {
                const example = (e.target as HTMLElement).dataset.example;
                if (example && this.editor) {
                    this.editor.dispatch({
                        changes: { from: 0, to: this.editor.state.doc.length, insert: this.getExampleCode(example) }
                    });
                }
            });
        });

        // Back button (in output tab)
        const backBtn = this.$('#python-back-btn');
        backBtn?.addEventListener('click', () => this.switchTab('editor'));

        // Copy button
        const copyBtn = this.$('#python-copy-btn');
        copyBtn?.addEventListener('click', () => {
            if (this.lastOutput) {
                const text = this.lastOutput.stdout + (this.lastOutput.stderr ? '\n' + this.lastOutput.stderr : '');
                navigator.clipboard.writeText(text);
            }
        });
    }

    private getExampleCode(example: string): string {
        switch (example) {
            case 'hello':
                return `# Hello World
print("Hello from QNTX Python!")
_result = "Hello, World!"
`;
            case 'math':
                return `# Math example
import math

# Calculate factorial
n = 10
factorial = math.factorial(n)
print(f"{n}! = {factorial}")

# Calculate pi approximation
pi_approx = sum(1/k**2 for k in range(1, 10000)) * 6
print(f"π² ≈ {pi_approx:.6f}")
print(f"π ≈ {math.sqrt(pi_approx):.6f}")

_result = {"factorial": factorial, "pi": math.pi}
`;
            case 'json':
                return `# JSON processing
import json

data = {
    "name": "QNTX",
    "version": "0.1.0",
    "features": ["attestations", "pulse", "plugins"],
    "python": True
}

print(json.dumps(data, indent=2))
_result = data
`;
            default:
                return this.getDefaultCode();
        }
    }

    async switchTab(tab: 'editor' | 'output'): Promise<void> {
        log.debug(SEG.UI, `switchTab called: ${this.currentTab} -> ${tab}`);

        if (tab === this.currentTab) {
            log.debug(SEG.UI, 'Already on this tab, skipping');
            return;
        }

        // Store editor content before switching
        let editorContent = '';
        if (this.currentTab === 'editor' && this.editor) {
            editorContent = this.editor.state.doc.toString();
            log.debug(SEG.UI, 'Stored editor content, length:', editorContent.length);
        }

        this.currentTab = tab;

        // Update tab buttons
        const tabs = this.panel?.querySelectorAll('.python-editor-tab');
        tabs?.forEach(t => {
            const tabElement = t as HTMLElement;
            if (tabElement.dataset.tab === tab) {
                tabElement.classList.add('active');
            } else {
                tabElement.classList.remove('active');
            }
        });

        // Render tab content
        const sidebar = this.$('#tab-sidebar') as HTMLElement;
        const content = this.$('#tab-content') as HTMLElement;

        if (!sidebar || !content) {
            log.error(SEG.UI, 'Missing tab containers!', { sidebar: !!sidebar, content: !!content });
            return;
        }

        if (tab === 'editor') {
            log.debug(SEG.UI, 'Rendering editor tab templates...');
            sidebar.innerHTML = this.getEditorSidebarTemplate();
            content.innerHTML = this.getEditorContentTemplate();
            log.debug(SEG.UI, 'Templates rendered');

            // Re-initialize editor
            log.debug(SEG.UI, 'Calling initializeEditor...');
            await this.initializeEditor(editorContent);
            log.debug(SEG.UI, 'initializeEditor completed');

            // Re-check status
            await this.checkPluginStatus();

            // Bind sidebar events
            this.bindSidebarEvents();
        } else {
            sidebar.innerHTML = this.getOutputSidebarTemplate();
            content.innerHTML = this.getOutputContentTemplate();

            // Show last output if available
            if (this.lastOutput) {
                this.updateOutput(this.lastOutput);
            }

            // Bind sidebar events
            this.bindSidebarEvents();
        }

        log.debug(SEG.UI, `switchTab completed: now on ${tab} tab`);
    }
}

// Create singleton instance
const pythonEditorPanel = new PythonEditorPanel();

// Export show and toggle functions
export function showPythonEditor(): void {
    pythonEditorPanel.show();
}

export function togglePythonEditor(): void {
    pythonEditorPanel.toggle();
}
