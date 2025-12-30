/**
 * Go Editor Panel - Simple Go code editor with gopls LSP integration
 *
 * Demonstrates gopls service working via WebSocket LSP protocol.
 * Provides basic code editing with autocomplete, hover, and diagnostics.
 */

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
    private escapeHandler: ((e: KeyboardEvent) => void) | null = null;

    constructor() {
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

        // Click overlay to close
        this.overlay.addEventListener('click', () => this.handleClose());

        // Escape key to close
        this.escapeHandler = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.handleClose();
            }
        };
        document.addEventListener('keydown', this.escapeHandler);

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon go-editor-icon">go</span>
                    <span class="prose-name">Go Editor</span>
                    <span class="prose-breadcrumb" id="go-editor-file">example.go</span>
                </div>
                <button class="prose-close go-editor-close" aria-label="Close">✕</button>
            </div>
            <div class="prose-body go-editor-body">
                <div>
                    <div class="go-editor-info">
                        <span>💡 gopls LSP</span>
                        <span class="go-editor-info-status">Status: <span id="gopls-status" class="gopls-status-connecting">connecting...</span></span>
                    </div>
                    <div id="go-editor-container" class="go-editor-container">
                        <!-- CodeMirror will be initialized here -->
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

        // Initialize CodeMirror editor if not already done
        if (!this.editor) {
            await this.initializeEditor();

            // Check if initialization succeeded
            if (!this.editor) {
                console.error('[Go Editor] Failed to initialize editor, closing panel');
                this.updateStatus('error', 'Failed to initialize');
                // Keep panel open so user can see the error message
            }
        }

        console.log('[Go Editor] Panel shown');
    }

    hide(): void {
        this.isVisible = false;
        this.panel?.classList.remove('visible');
        this.panel?.classList.add('hidden');
        this.overlay?.classList.remove('visible');
        this.overlay?.classList.add('hidden');
        console.log('[Go Editor] Panel closed');
    }

    handleClose(): void {
        this.hide();
    }

    /**
     * Update gopls connection status display
     * @param status - Status state: 'connecting' | 'ready' | 'error' | 'unavailable'
     * @param message - Optional custom message to display
     */
    updateStatus(status: GoplsStatus, message?: string): void {
        const statusEl = this.panel?.querySelector('#gopls-status') as HTMLElement;
        if (!statusEl) return;

        const config = STATUS_CONFIG[status];

        // Remove all status classes
        Object.values(STATUS_CONFIG).forEach(cfg => {
            statusEl.classList.remove(cfg.className);
        });

        // Add current status class
        statusEl.classList.add(config.className);
        statusEl.textContent = message || config.message;
    }

    async initializeEditor(): Promise<void> {
        this.updateStatus('connecting');
        try {
            // Import CodeMirror modules (bundled by Bun)
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
                console.log('[Go Editor] Go language support loaded');
            } catch (err) {
                console.error('[Go Editor] Failed to load Go language support:', err);
                goExtension = []; // Fallback to no syntax highlighting
            }

            // Get backend URL for LSP WebSocket connection
            const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
            const wsProtocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
            const wsHost = backendUrl.replace(/^https?:\/\//, '');
            const goplsUri = `${wsProtocol}//${wsHost}/gopls` as `ws://${string}` | `wss://${string}`;

            // Fetch gopls workspace configuration from backend
            let workspaceRoot = 'file:///tmp/qntx-workspace'; // Fallback
            let goplsEnabled = false;

            try {
                const configResponse = await fetch(`${backendUrl}/api/config`);
                if (configResponse.ok) {
                    const config = await configResponse.json();

                    // Check if gopls is enabled
                    goplsEnabled = config.code?.gopls?.enabled ?? false;

                    if (!goplsEnabled) {
                        console.warn('[Go Editor] gopls is disabled in config');
                        this.updateStatus('unavailable', 'gopls disabled');
                        throw new Error('gopls service is disabled in configuration');
                    }

                    // Use the gopls workspace root from backend config
                    if (config.code?.gopls?.workspace_root) {
                        workspaceRoot = `file://${config.code.gopls.workspace_root}`;
                    }
                }
            } catch (e) {
                if (e instanceof Error && e.message.includes('disabled')) {
                    throw e; // Re-throw if gopls is disabled
                }
                console.warn('[Go Editor] Failed to fetch config, using fallback workspace:', e);
            }

            // Use a temp file for the sample code, not a real project file
            const documentUri = 'file:///tmp/qntx-editor/example.go';

            console.log('[Go Editor] Connecting to gopls at', goplsUri, 'workspace:', workspaceRoot);

            const container = this.panel?.querySelector('#go-editor-container');
            if (!container) {
                console.error('[Go Editor] Container not found');
                return;
            }

            // Sample Go code to demonstrate gopls features
            const sampleCode = `package main

import "fmt"

// greet returns a greeting message
func greet(name string) string {
    return fmt.Sprintf("Hello, %s!", name)
}

func main() {
    message := greet("World")
    fmt.Println(message)
}
`;

            // Custom theme for better contrast
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

            // Create CodeMirror editor with gopls LSP support
            this.editor = new EditorView({
                state: EditorState.create({
                    doc: sampleCode,
                    extensions: [
                        keymap.of([...defaultKeymap, ...completionKeymap]),
                        goExtension, // Will be go() or [] if loading failed
                        syntaxHighlighting(defaultHighlightStyle), // Apply syntax highlighting theme
                        autocompletion(),
                        // LSP integration for autocomplete, hover, diagnostics
                        languageServer({
                            serverUri: goplsUri,
                            rootUri: workspaceRoot,
                            documentUri: documentUri,
                            languageId: 'go',
                            workspaceFolders: [{
                                name: 'qntx',
                                uri: workspaceRoot
                            }]
                        }),
                        goEditorTheme,
                        EditorView.lineWrapping
                    ]
                }),
                parent: container
            });

            console.log('[Go Editor] CodeMirror initialized with LSP support');
            this.updateStatus('ready');

        } catch (error) {
            console.error('[Go Editor] Failed to initialize editor:', error);
            this.updateStatus('error');

            const container = this.panel?.querySelector('#go-editor-container');
            if (container) {
                const errorMsg = error instanceof Error ? error.message : String(error);
                const isGoplsDisabled = errorMsg.includes('disabled');

                container.innerHTML = `
                    <div class="go-editor-error">
                        <h3>Failed to load editor</h3>
                        <p>Error: ${errorMsg}</p>
                        ${isGoplsDisabled ? '' : `
                            <p class="go-editor-error-help">
                                Make sure CodeMirror dependencies are installed:<br>
                                <code>npm install codemirror @codemirror/lang-go @codemirror/state</code>
                            </p>
                        `}
                    </div>
                `;
            }
            // Set this.editor to null to indicate failure
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

    /**
     * Clean up and destroy the editor panel
     * Removes event listeners, destroys editor, and removes DOM elements
     */
    destroy(): void {
        console.log('[Go Editor] Destroying editor panel');

        // Destroy CodeMirror editor
        if (this.editor) {
            try {
                this.editor.destroy();
            } catch (err) {
                console.warn('[Go Editor] Error destroying editor:', err);
            }
            this.editor = null;
        }

        // Remove event listeners
        if (this.escapeHandler) {
            document.removeEventListener('keydown', this.escapeHandler);
            this.escapeHandler = null;
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
