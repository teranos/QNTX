/**
 * Go Editor Panel - Go code editor with gopls LSP integration
 *
 * Provides file browsing, editing, and saving of Go code files with
 * gopls LSP features (autocomplete, hover, diagnostics).
 */

import { BasePanel } from '../base-panel.ts';
import { GoEditorNavigation } from './navigation.ts';
import { CodeSuggestions } from './suggestions.ts';
import { apiFetch } from '../api.ts';
import { fetchDevMode } from '../dev-mode.ts';

// Status type for gopls connection
type GoplsStatus = 'connecting' | 'ready' | 'error' | 'unavailable';

// Status configuration
const STATUS_CONFIG: Record<GoplsStatus, { message: string; className: string }> = {
    connecting: { message: 'connecting...', className: 'gopls-status-connecting' },
    ready: { message: 'ready', className: 'gopls-status-ready' },
    error: { message: 'error', className: 'gopls-status-error' },
    unavailable: { message: 'unavailable', className: 'gopls-status-unavailable' }
};

class GoEditorPanel extends BasePanel {
    private editor: any | null = null; // CodeMirror editor instance
    private currentPath: string = '';
    private hasUnsavedChanges: boolean = false;
    private isDevMode: boolean = false;
    private workspaceRoot: string = '';

    // Components
    private navigation: GoEditorNavigation;
    private suggestions: CodeSuggestions;

    // DOM elements
    private breadcrumbElement: HTMLElement | null = null;
    private saveIndicator: HTMLElement | null = null;
    private statusElement: HTMLElement | null = null;

    // Save keyboard handler
    private saveHandler: ((e: KeyboardEvent) => void) | null = null;

    constructor() {
        super({
            id: 'go-editor-panel',
            classes: ['prose-panel'], // Reuse prose panel styles
            useOverlay: true,
            closeOnEscape: true
        });

        // Initialize navigation with callback
        this.navigation = new GoEditorNavigation({
            onFileSelect: (path: string) => {
                this.loadFile(path);
            }
        });

        // Initialize suggestions with callback (panel is guaranteed to exist from BasePanel)
        this.suggestions = new CodeSuggestions({
            panel: this.panel!,
            onNavigateToFile: (filePath: string, line: number) => {
                this.openFileAtLine(filePath, line);
            }
        });

        // Bind DOM elements
        if (this.panel) {
            this.breadcrumbElement = this.panel.querySelector('.prose-breadcrumb');
            this.saveIndicator = this.panel.querySelector('.go-editor-save-indicator');
            this.statusElement = this.panel.querySelector('#gopls-status');
            this.navigation.bindElements(this.panel);
        }
    }

    protected getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon go-editor-icon">go</span>
                    <span class="prose-name">Go Editor</span>
                    <span class="prose-breadcrumb"></span>
                </div>
                <button class="prose-close go-editor-close" aria-label="Close">✕</button>
            </div>
            <div class="go-editor-tabs">
                <button class="go-editor-tab active" data-tab="editor">Editor</button>
                <button class="go-editor-tab" data-tab="suggestions">Suggestions</button>
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
        // Close button
        const closeBtn = this.$('.go-editor-close');
        closeBtn?.addEventListener('click', () => this.hide());

        // Tab switching
        const tabs = this.panel?.querySelectorAll('.go-editor-tab');
        tabs?.forEach(tab => {
            tab.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;
                const tabName = target.dataset.tab as 'editor' | 'suggestions';
                this.switchTab(tabName);
            });
        });

        // Save on Cmd/Ctrl+S
        this.saveHandler = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 's' && this.isVisible) {
                e.preventDefault();
                this.saveContent();
            }
        };
        document.addEventListener('keydown', this.saveHandler);
    }

    protected beforeHide(): boolean {
        if (this.hasUnsavedChanges) {
            return confirm('You have unsaved changes. Close anyway?');
        }
        return true;
    }

    protected async onShow(): Promise<void> {
        // Fetch dev mode status
        this.isDevMode = await fetchDevMode();

        // Initialize editor tab (renders sidebar and content)
        this.switchTab('editor');

        // Load navigation tree and recent files
        await this.navigation.refresh();

        // Load file from URL fragment or last viewed file or default
        const fragment = window.location.hash.slice(1).replace('go-editor/', '');
        const lastViewed = this.navigation.getLastViewedFile();
        const filePath = fragment || lastViewed || 'cmd/qntx/main.go';

        if (filePath) {
            await this.loadFile(filePath);
        }

        console.log('[Go Editor] Panel shown');
    }

    protected onHide(): void {
        // Clear URL fragment when closing
        if (window.location.hash.startsWith('#go-editor/')) {
            window.history.replaceState(null, '', window.location.pathname + window.location.search);
        }

        // Clean up editor
        this.destroyEditor();

        console.log('[Go Editor] Panel closed');
    }

    protected onDestroy(): void {
        this.destroyEditor();

        if (this.saveHandler) {
            document.removeEventListener('keydown', this.saveHandler);
            this.saveHandler = null;
        }
    }

    async loadFile(path: string): Promise<void> {
        try {
            const response = await apiFetch(`/api/code/${path}`);
            if (!response.ok) {
                throw new Error(`Failed to load file: ${response.statusText}`);
            }

            const content = await response.text();
            this.currentPath = path;
            await this.initializeEditor(content);
            this.updateBreadcrumb(path);

            // Update URL fragment for deep linking
            window.history.replaceState(null, '', `#go-editor/${path}`);

            // Add to recent files
            this.navigation.addToRecentFiles(path);
        } catch (error) {
            console.error('Failed to load file:', error);
            this.showError(`Failed to load ${path}`);
        }
    }

    async initializeEditor(content: string): Promise<void> {
        this.updateStatus('connecting');

        // Destroy existing editor if present
        if (this.editor) {
            this.editor.destroy();
            this.editor = null;
        }

        try {
            // Import CodeMirror modules
            const { EditorView, keymap } = await import('@codemirror/view');
            const { EditorState } = await import('@codemirror/state');
            const { defaultKeymap } = await import('@codemirror/commands');
            const { oneDark } = await import('@codemirror/theme-one-dark');
            const { autocompletion, completionKeymap } = await import('@codemirror/autocomplete');
            const { languageServer } = await import('codemirror-languageserver');

            // Import Go language support
            let goExtension;
            try {
                const goModule = await import('@codemirror/lang-go');
                goExtension = goModule.go();
            } catch (err) {
                console.error('[Go Editor] Failed to load Go language support:', err);
                goExtension = [];
            }

            // Get backend URL and gopls configuration
            const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
            const wsProtocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
            const wsHost = backendUrl.replace(/^https?:\/\//, '');
            const goplsUri = `${wsProtocol}//${wsHost}/gopls` as `ws://${string}` | `wss://${string}`;

            // Fetch workspace configuration
            let workspaceRoot = 'file:///tmp/qntx-workspace';
            let goplsEnabled = false;

            try {
                const configResponse = await fetch(`${backendUrl}/api/config`);
                if (configResponse.ok) {
                    const config = await configResponse.json();
                    goplsEnabled = config.code?.gopls?.enabled ?? false;

                    if (!goplsEnabled) {
                        console.warn('[Go Editor] gopls is disabled in config');
                        this.updateStatus('unavailable', 'gopls disabled');
                        throw new Error('gopls service is disabled in configuration');
                    }

                    if (config.code?.gopls?.workspace_root) {
                        this.workspaceRoot = config.code.gopls.workspace_root;
                        workspaceRoot = `file://${this.workspaceRoot}`;
                    }
                }
            } catch (e) {
                if (e instanceof Error && e.message.includes('disabled')) {
                    throw e;
                }
                console.warn('[Go Editor] Failed to fetch config:', e);
            }

            // Use real workspace file URI for gopls
            const documentUri = `file://${this.workspaceRoot}/${this.currentPath}`;

            console.log('[Go Editor] Initializing with workspace:', workspaceRoot, 'document:', documentUri);

            const container = this.$('#go-editor-container');
            if (!container) {
                console.error('[Go Editor] Container not found');
                return;
            }

            // Create CodeMirror editor
            this.editor = new EditorView({
                state: EditorState.create({
                    doc: content,
                    extensions: [
                        keymap.of([...defaultKeymap, ...completionKeymap]),
                        goExtension,
                        oneDark,
                        autocompletion(),
                        languageServer({
                            serverUri: goplsUri,
                            rootUri: workspaceRoot,
                            documentUri: documentUri,
                            languageId: 'go',
                            workspaceFolders: [{
                                name: 'workspace',
                                uri: workspaceRoot
                            }]
                        }),
                        EditorView.lineWrapping,
                        EditorView.updateListener.of((update) => {
                            if (update.docChanged) {
                                this.hasUnsavedChanges = true;
                                this.updateSaveIndicator();
                            }
                        })
                    ]
                }),
                parent: container
            });

            this.hasUnsavedChanges = false;
            this.updateSaveIndicator();
            this.updateStatus('ready');

            console.log('[Go Editor] Editor initialized');
        } catch (error) {
            console.error('[Go Editor] Failed to initialize editor:', error);
            this.updateStatus('error');
            this.showError(error instanceof Error ? error.message : String(error));
            this.editor = null;
        }
    }

    async saveContent(): Promise<void> {
        if (!this.editor || !this.hasUnsavedChanges || !this.isDevMode || !this.currentPath) {
            return;
        }

        try {
            const doc = this.editor.state.doc;
            const content = doc.toString();

            const response = await apiFetch(`/api/code/${this.currentPath}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'text/plain' },
                body: content
            });

            if (!response.ok) {
                throw new Error(`Failed to save: ${response.statusText}`);
            }

            this.hasUnsavedChanges = false;
            this.updateSaveIndicator();
            this.showStatus('Saved');
            console.log('[Go Editor] File saved:', this.currentPath);
        } catch (error) {
            console.error('Failed to save content:', error);
            this.showError('Failed to save');
        }
    }

    updateBreadcrumb(path: string): void {
        if (!this.breadcrumbElement) return;
        const parts = path.split('/');
        this.breadcrumbElement.textContent = parts.join(' / ');
    }

    updateSaveIndicator(): void {
        if (!this.saveIndicator) return;
        if (this.hasUnsavedChanges) {
            this.saveIndicator.textContent = '● Unsaved changes';
            this.saveIndicator.classList.remove('hidden');
        } else {
            this.saveIndicator.classList.add('hidden');
        }
    }

    updateStatus(status: GoplsStatus, message?: string): void {
        if (!this.statusElement) return;

        const config = STATUS_CONFIG[status];

        // Remove all status classes
        Object.values(STATUS_CONFIG).forEach(cfg => {
            this.statusElement?.classList.remove(cfg.className);
        });

        // Add current status class
        this.statusElement.classList.add(config.className);
        this.statusElement.textContent = message || config.message;
    }

    showStatus(message: string): void {
        console.log('[Go Editor]', message);
    }

    showError(message: string): void {
        const container = this.$('#go-editor-container');
        if (container) {
            container.innerHTML = `
                <div class="go-editor-error">
                    <h3>Error</h3>
                    <p>${message}</p>
                </div>
            `;
        }
    }

    destroyEditor(): void {
        if (this.editor) {
            try {
                this.editor.destroy();
            } catch (err) {
                console.warn('[Go Editor] Error destroying editor:', err);
            }
            this.editor = null;
        }
    }

    switchTab(tab: 'editor' | 'suggestions'): void {
        // Update tab buttons
        const tabs = this.panel?.querySelectorAll('.go-editor-tab');
        tabs?.forEach(t => {
            const tabElement = t as HTMLElement;
            if (tabElement.dataset.tab === tab) {
                tabElement.classList.add('active');
            } else {
                tabElement.classList.remove('active');
            }
        });

        // Render tab content dynamically
        const sidebar = this.$('#tab-sidebar') as HTMLElement;
        const content = this.$('#tab-content') as HTMLElement;

        if (!sidebar || !content) return;

        if (tab === 'editor') {
            // Render editor sidebar
            sidebar.innerHTML = `
                <div class="prose-sidebar-header">
                    <input type="text" class="prose-search go-editor-search" placeholder="Search files..." />
                </div>
                <div class="prose-recent" id="go-editor-recent">
                    <!-- Recent files will be populated here -->
                </div>
                <div class="prose-tree" id="go-editor-tree">
                    <!-- Tree will be populated here -->
                </div>
            `;

            // Render editor content
            content.innerHTML = `
                <div class="go-editor-info">
                    <span>💡 gopls LSP</span>
                    <span class="go-editor-info-status">Status: <span id="gopls-status" class="gopls-status-connecting">connecting...</span></span>
                    <span class="go-editor-save-indicator hidden"></span>
                </div>
                <div class="prose-editor-container">
                    <div id="go-editor-container" class="go-editor-container">
                        <!-- CodeMirror will be initialized here -->
                    </div>
                </div>
            `;

            // Re-bind navigation and status elements after DOM change
            this.navigation.bindElements(this.panel!);
            this.statusElement = this.panel!.querySelector('#gopls-status');
            this.saveIndicator = this.panel!.querySelector('.go-editor-save-indicator');

            // Reinitialize editor if we had content
            if (this.currentPath && this.editor) {
                // Store current content before destroying
                const currentContent = this.editor.state.doc.toString();
                this.destroyEditor();
                this.initializeEditor(currentContent);
            }
        } else {
            // Render suggestions sidebar
            sidebar.innerHTML = `
                <div class="prose-sidebar-header">
                    <select class="prose-search" id="pr-select">
                        <option value="">Loading PRs...</option>
                    </select>
                </div>
                <div id="pr-info" class="hidden">
                    <div class="pr-stats">
                        <span id="suggestion-count">0 suggestions</span>
                    </div>
                </div>
            `;

            // Render suggestions content with proper container structure
            content.innerHTML = `
                <div id="suggestions-content">
                    <div id="suggestions-list" class="suggestions-list">
                        <div class="no-suggestions">Select a PR to view suggestions</div>
                    </div>
                </div>
            `;

            // Load open PRs into dropdown
            this.suggestions.loadOpenPRs();
        }
    }

    openFileAtLine(filePath: string, line: number): void {
        // Switch to editor tab FIRST (so the container exists)
        this.switchTab('editor');

        // Then load the file
        this.loadFile(filePath).then(() => {
            // Scroll to line after file loads (CodeMirror 6 API)
            if (this.editor && line > 0) {
                setTimeout(() => {
                    try {
                        // CodeMirror 6: Use dispatch to scroll to position
                        const pos = this.editor.state.doc.line(line).from;
                        this.editor.dispatch({
                            selection: { anchor: pos, head: pos },
                            scrollIntoView: true
                        });
                        this.editor.focus();
                    } catch (err) {
                        console.warn('[Go Editor] Failed to scroll to line:', err);
                    }
                }, 100);
            }
        });
    }
}

// Create singleton instance
const goEditorPanel = new GoEditorPanel();

// Export show and toggle functions
export function showGoEditor(): void {
    goEditorPanel.show();
}

export function toggleGoEditor(): void {
    goEditorPanel.toggle();
}
