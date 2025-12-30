/**
 * Go Editor Panel - Go code editor with gopls LSP integration
 *
 * Provides file browsing, editing, and saving of Go code files with
 * gopls LSP features (autocomplete, hover, diagnostics).
 */

import { GoEditorNavigation } from './go-editor-navigation.ts';
import { apiFetch } from './api.ts';
import { fetchDevMode } from './dev-mode.ts';

// Status type for gopls connection
type GoplsStatus = 'connecting' | 'ready' | 'error' | 'unavailable';

// Status configuration
const STATUS_CONFIG: Record<GoplsStatus, { message: string; className: string }> = {
    connecting: { message: 'connecting...', className: 'gopls-status-connecting' },
    ready: { message: 'ready', className: 'gopls-status-ready' },
    error: { message: 'error', className: 'gopls-status-error' },
    unavailable: { message: 'unavailable', className: 'gopls-status-unavailable' }
};

class GoEditorPanel {
    private panel: HTMLElement | null = null;
    private overlay: HTMLElement | null = null;
    private isVisible: boolean = false;
    private editor: any | null = null; // CodeMirror editor instance
    private currentPath: string = '';
    private hasUnsavedChanges: boolean = false;
    private isDevMode: boolean = false;
    private workspaceRoot: string = '';

    // Components
    private navigation: GoEditorNavigation;

    // DOM elements
    private breadcrumbElement: HTMLElement | null = null;
    private saveIndicator: HTMLElement | null = null;
    private statusElement: HTMLElement | null = null;

    // Event listeners
    private escapeHandler: ((e: KeyboardEvent) => void) | null = null;
    private saveHandler: ((e: KeyboardEvent) => void) | null = null;
    private overlayClickHandler: (() => void) | null = null;

    constructor() {
        // Initialize navigation with callback
        this.navigation = new GoEditorNavigation({
            onFileSelect: (path: string) => {
                this.loadFile(path);
            }
        });

        this.initialize();
    }

    initialize(): void {
        // Create overlay
        this.overlay = document.createElement('div');
        this.overlay.id = 'go-editor-overlay';
        this.overlay.className = 'prose-overlay hidden'; // Reuse prose overlay styles

        // Create panel
        this.panel = document.createElement('div');
        this.panel.id = 'go-editor-panel';
        this.panel.className = 'prose-panel hidden'; // Reuse prose panel styles
        this.panel.innerHTML = this.getTemplate();

        // Append to body
        document.body.appendChild(this.overlay);
        document.body.appendChild(this.panel);

        // Bind DOM elements
        this.breadcrumbElement = this.panel.querySelector('.prose-breadcrumb');
        this.saveIndicator = this.panel.querySelector('.go-editor-save-indicator');
        this.statusElement = this.panel.querySelector('#gopls-status');

        // Bind navigation elements
        this.navigation.bindElements(this.panel);

        // Click overlay to close
        this.overlayClickHandler = () => this.handleClose();
        this.overlay.addEventListener('click', this.overlayClickHandler);

        // Escape key to close
        this.escapeHandler = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.handleClose();
            }
        };
        document.addEventListener('keydown', this.escapeHandler);

        // Save on Cmd/Ctrl+S
        this.saveHandler = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 's' && this.isVisible) {
                e.preventDefault();
                this.saveContent();
            }
        };
        document.addEventListener('keydown', this.saveHandler);

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon go-editor-icon">go</span>
                    <span class="prose-name">Go Editor</span>
                    <span class="prose-breadcrumb"></span>
                </div>
                <button class="prose-close go-editor-close" aria-label="Close">✕</button>
            </div>
            <div class="prose-body">
                <div class="prose-sidebar">
                    <div class="prose-sidebar-header">
                        <input type="text" class="prose-search go-editor-search" placeholder="Search files..." />
                    </div>
                    <div class="prose-recent" id="go-editor-recent">
                        <!-- Recent files will be populated here -->
                    </div>
                    <div class="prose-tree" id="go-editor-tree">
                        <!-- Tree will be populated here -->
                    </div>
                </div>
                <div class="prose-content">
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
                </div>
            </div>
        `;
    }

    setupEventListeners(): void {
        const closeBtn = this.panel?.querySelector('.go-editor-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.handleClose());
        }
    }

    async show(): Promise<void> {
        if (this.isVisible) return;

        this.isVisible = true;
        this.overlay?.classList.remove('hidden');
        this.overlay?.classList.add('visible');
        this.panel?.classList.remove('hidden');
        this.panel?.classList.add('visible');

        // Fetch dev mode status
        this.isDevMode = await fetchDevMode();

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

    hide(): void {
        this.isVisible = false;
        this.panel?.classList.remove('visible');
        this.panel?.classList.add('hidden');
        this.overlay?.classList.remove('visible');
        this.overlay?.classList.add('hidden');

        // Clear URL fragment when closing
        if (window.location.hash.startsWith('#go-editor/')) {
            window.history.replaceState(null, '', window.location.pathname + window.location.search);
        }

        // Clean up editor
        this.destroyEditor();

        console.log('[Go Editor] Panel closed');
    }

    handleClose(): void {
        if (this.hasUnsavedChanges) {
            if (confirm('You have unsaved changes. Close anyway?')) {
                this.hide();
            }
        } else {
            this.hide();
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
            const { syntaxHighlighting, defaultHighlightStyle } = await import('@codemirror/language');
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

            const container = this.panel?.querySelector('#go-editor-container');
            if (!container) {
                console.error('[Go Editor] Container not found');
                return;
            }

            // Custom theme
            const goEditorTheme = EditorView.theme({
                "&": {
                    height: "100%",
                    fontSize: "14px",
                    fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace"
                },
                ".cm-scroller": {
                    overflow: "auto",
                    backgroundColor: "#1e1e1e"
                },
                ".cm-content": {
                    caretColor: "#66b3ff",
                    padding: "16px"
                },
                ".cm-cursor, .cm-cursor-primary": {
                    borderLeftColor: "#66b3ff !important",
                    borderLeftWidth: "2px !important"
                },
                ".cm-gutters": {
                    backgroundColor: "#1e1e1e",
                    color: "#858585",
                    border: "none"
                },
                ".cm-activeLineGutter": {
                    backgroundColor: "#2a2d2e"
                }
            });

            // Create CodeMirror editor
            this.editor = new EditorView({
                state: EditorState.create({
                    doc: content,
                    extensions: [
                        keymap.of([...defaultKeymap, ...completionKeymap]),
                        goExtension,
                        syntaxHighlighting(defaultHighlightStyle),
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
                        goEditorTheme,
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
        // Could add a status message display
        console.log('[Go Editor]', message);
    }

    showError(message: string): void {
        const container = this.panel?.querySelector('#go-editor-container');
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

    async toggle(): Promise<void> {
        if (this.isVisible) {
            this.hide();
        } else {
            await this.show();
        }
    }

    destroy(): void {
        console.log('[Go Editor] Destroying panel');

        this.destroyEditor();

        // Remove event listeners
        if (this.escapeHandler) {
            document.removeEventListener('keydown', this.escapeHandler);
            this.escapeHandler = null;
        }
        if (this.saveHandler) {
            document.removeEventListener('keydown', this.saveHandler);
            this.saveHandler = null;
        }
        if (this.overlay && this.overlayClickHandler) {
            this.overlay.removeEventListener('click', this.overlayClickHandler);
            this.overlayClickHandler = null;
        }

        // Remove DOM elements
        if (this.panel) {
            this.panel.remove();
            this.panel = null;
        }
        if (this.overlay) {
            this.overlay.remove();
            this.overlay = null;
        }

        this.isVisible = false;
    }
}

// Create singleton instance
const goEditorPanel = new GoEditorPanel();

// Export toggle function for symbol palette
export function toggleGoEditor(): void {
    goEditorPanel.toggle();
}
