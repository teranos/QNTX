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
import { getTheme, onThemeChange, type ThemeMode } from '../codemirror-themes.ts';

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
    private currentTab: 'editor' | 'suggestions' = 'editor';
    private editorContent: string = '';

    // Theme support
    private themeCompartment: any | null = null;
    private themeUnsubscribe: (() => void) | null = null;

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
            const modules = await this.loadCodeMirrorModules();
            const goplsConfig = await this.fetchGoplsConfig();
            await this.createEditorInstance(content, modules, goplsConfig);

            this.hasUnsavedChanges = false;
            this.updateSaveIndicator();
            this.updateStatus('ready');

            this.log('Editor initialized');
        } catch (error) {
            this.log('Failed to initialize editor: ' + (error instanceof Error ? error.message : String(error)));
            this.updateStatus('error');
            this.showError(error instanceof Error ? error.message : String(error));
            this.editor = null;
        }
    }

    private async loadCodeMirrorModules() {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState, Compartment } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { autocompletion, completionKeymap } = await import('@codemirror/autocomplete');
        const { languageServer } = await import('codemirror-languageserver');

        // Import Go language support
        let goExtension;
        try {
            const goModule = await import('@codemirror/lang-go');
            goExtension = goModule.go();
        } catch (err) {
            this.log('Failed to load Go language support: ' + err);
            goExtension = [];
        }

        return {
            EditorView,
            EditorState,
            Compartment,
            keymap,
            defaultKeymap,
            completionKeymap,
            autocompletion,
            languageServer,
            goExtension
        };
    }

    private async fetchGoplsConfig() {
        const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
        const wsProtocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
        const wsHost = backendUrl.replace(/^https?:\/\//, '');
        const goplsUri = `${wsProtocol}//${wsHost}/gopls` as `ws://${string}` | `wss://${string}`;

        let workspaceRoot = 'file:///tmp/qntx-workspace';

        try {
            const configResponse = await fetch(`${backendUrl}/api/config`);
            if (configResponse.ok) {
                const config = await configResponse.json();
                const goplsEnabled = config.code?.gopls?.enabled ?? false;

                if (!goplsEnabled) {
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
            this.log('Failed to fetch config, using defaults');
        }

        const documentUri = `file://${this.workspaceRoot}/${this.currentPath}`;

        return { goplsUri, workspaceRoot, documentUri };
    }

    private async createEditorInstance(content: string, modules: any, goplsConfig: any): Promise<void> {
        const container = this.$('#go-editor-container');
        if (!container) {
            throw new Error('Editor container not found');
        }

        const { EditorView, EditorState, Compartment, keymap, defaultKeymap, completionKeymap,
                autocompletion, languageServer, goExtension } = modules;
        const { goplsUri, workspaceRoot, documentUri } = goplsConfig;

        // Create theme compartment for dynamic switching
        this.themeCompartment = new Compartment();

        this.editor = new EditorView({
            state: EditorState.create({
                doc: content,
                extensions: [
                    keymap.of([...defaultKeymap, ...completionKeymap]),
                    goExtension,
                    // Theme via compartment for dynamic switching
                    this.themeCompartment.of(getTheme()),
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
                    EditorView.updateListener.of((update: any) => {
                        if (update.docChanged) {
                            this.hasUnsavedChanges = true;
                            this.updateSaveIndicator();
                        }
                    })
                ]
            }),
            parent: container
        });

        // Subscribe to theme changes
        this.themeUnsubscribe = onThemeChange((_mode: ThemeMode) => {
            if (this.editor && this.themeCompartment) {
                this.editor.dispatch({
                    effects: this.themeCompartment.reconfigure(getTheme())
                });
            }
        });
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

    private log(message: string): void {
        console.log('[Go Editor]', message);
    }

    showStatus(message: string): void {
        this.log(message);
    }

    showError(message: string): void {
        const container = this.$('#go-editor-container');
        if (container) {
            const errorDiv = document.createElement('div');
            errorDiv.className = 'go-editor-error';

            const heading = document.createElement('h3');
            heading.textContent = 'Error';

            const para = document.createElement('p');
            para.textContent = message;

            errorDiv.appendChild(heading);
            errorDiv.appendChild(para);

            container.innerHTML = '';
            container.appendChild(errorDiv);
        }
    }

    destroyEditor(): void {
        // Unsubscribe from theme changes
        if (this.themeUnsubscribe) {
            this.themeUnsubscribe();
            this.themeUnsubscribe = null;
        }

        if (this.editor) {
            try {
                this.editor.destroy();
            } catch (err) {
                console.warn('[Go Editor] Error destroying editor:', err);
            }
            this.editor = null;
        }
        this.themeCompartment = null;
    }

    private getEditorSidebarTemplate(): string {
        return `
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
    }

    private getEditorContentTemplate(): string {
        return `
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
    }

    private getSuggestionsSidebarTemplate(): string {
        return `
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
    }

    private getSuggestionsContentTemplate(): string {
        return `
            <div id="suggestions-content">
                <div id="suggestions-list" class="suggestions-list">
                    <div class="no-suggestions">Select a PR to view suggestions</div>
                </div>
            </div>
        `;
    }

    private bindStatusElements(): void {
        this.statusElement = this.panel!.querySelector('#gopls-status');
        this.saveIndicator = this.panel!.querySelector('.go-editor-save-indicator');
    }

    async switchTab(tab: 'editor' | 'suggestions'): Promise<void> {
        // Don't switch if already on this tab
        if (tab === this.currentTab) return;

        // Store editor content before switching away from editor tab
        if (this.currentTab === 'editor' && this.editor) {
            this.editorContent = this.editor.state.doc.toString();
        }

        this.currentTab = tab;

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
            sidebar.innerHTML = this.getEditorSidebarTemplate();
            content.innerHTML = this.getEditorContentTemplate();

            // Re-bind navigation and status elements after DOM change
            this.navigation.bindElements(this.panel!);
            this.bindStatusElements();

            // Reinitialize editor with preserved content
            if (this.currentPath && this.editorContent) {
                this.destroyEditor();
                await this.initializeEditor(this.editorContent);
            }
        } else {
            sidebar.innerHTML = this.getSuggestionsSidebarTemplate();
            content.innerHTML = this.getSuggestionsContentTemplate();

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
