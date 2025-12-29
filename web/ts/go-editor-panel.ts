/**
 * Go Editor Panel - Simple Go code editor with gopls LSP integration
 *
 * Demonstrates gopls service working via WebSocket LSP protocol.
 * Provides basic code editing with autocomplete, hover, and diagnostics.
 */

class GoEditorPanel {
    private panel: HTMLElement | null = null;
    private overlay: HTMLElement | null = null;
    private isVisible: boolean = false;
    private editor: any | null = null; // CodeMirror editor instance

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
        document.addEventListener('keydown', (e: KeyboardEvent) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.handleClose();
            }
        });

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon" style="font-family: monospace; font-size: 14px; font-weight: bold;">go</span>
                    <span class="prose-name">Go Editor</span>
                    <span class="prose-breadcrumb" id="go-editor-file">example.go</span>
                </div>
                <button class="prose-close go-editor-close" aria-label="Close">✕</button>
            </div>
            <div class="prose-body" style="display: flex;">
                <div style="flex: 1; display: flex; flex-direction: column;">
                    <div class="go-editor-info" style="padding: 10px; background: #252526; color: #cccccc; font-size: 12px; border-bottom: 1px solid #3e3e42;">
                        <span>💡 gopls LSP</span>
                        <span style="margin-left: 20px;">Status: <span id="gopls-status" style="color: #858585;">connecting...</span></span>
                    </div>
                    <div id="go-editor-container" style="flex: 1; overflow: auto; background: #1e1e1e;">
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

    async initializeEditor(): Promise<void> {
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
            const goplsUri = `${wsProtocol}//${wsHost}/gopls`;

            // Fetch gopls workspace configuration from backend
            let workspaceRoot = 'file:///tmp/qntx-workspace'; // Fallback

            try {
                const configResponse = await fetch(`${backendUrl}/api/config`);
                if (configResponse.ok) {
                    const config = await configResponse.json();
                    // Use the gopls workspace root from backend config
                    if (config.code?.gopls?.workspace_root) {
                        workspaceRoot = `file://${config.code.gopls.workspace_root}`;
                    }
                }
            } catch (e) {
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

        } catch (error) {
            console.error('[Go Editor] Failed to initialize editor:', error);
            const container = this.panel?.querySelector('#go-editor-container');
            if (container) {
                container.innerHTML = `
                    <div style="padding: 20px; color: #ff6b6b;">
                        <h3>Failed to load editor</h3>
                        <p>Error: ${error instanceof Error ? error.message : String(error)}</p>
                        <p style="margin-top: 10px; font-size: 12px; color: #888;">
                            Make sure CodeMirror dependencies are installed:<br>
                            <code style="background: #2a2a2a; padding: 2px 6px; border-radius: 3px;">
                                npm install codemirror @codemirror/lang-go @codemirror/state
                            </code>
                        </p>
                    </div>
                `;
            }
        }
    }

    connectToGopls(): void {
        const statusEl = this.panel?.querySelector('#gopls-status') as HTMLElement;
        if (!statusEl) return;

        try {
            // Use backend URL in dev mode, otherwise use current host
            const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
            const wsUrl = backendUrl.replace(/^http/, 'ws') + '/gopls';

            const ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                console.log('[Go Editor] Connected to gopls WebSocket');
                statusEl.textContent = 'connected';
                statusEl.style.color = '#4ec9b0';

                // TODO: Send LSP initialize request
                // TODO: Handle LSP responses for completion, hover, diagnostics
            };

            ws.onmessage = (event) => {
                console.log('[Go Editor] Received from gopls:', event.data);
                // TODO: Handle LSP messages
            };

            ws.onerror = (error) => {
                console.error('[Go Editor] WebSocket error:', error);
                statusEl.textContent = 'error';
                statusEl.style.color = '#f48771';
            };

            ws.onclose = () => {
                console.log('[Go Editor] Disconnected from gopls');
                statusEl.textContent = 'disconnected';
                statusEl.style.color = '#858585';
            };
        } catch (error) {
            console.error('[Go Editor] Failed to connect to gopls:', error);
            statusEl.textContent = 'unavailable';
            statusEl.style.color = '#858585';
        }
    }

    async toggle(): Promise<void> {
        if (this.isVisible) {
            this.hide();
        } else {
            await this.show();
        }
    }
}

// Create singleton instance
const goEditorPanel = new GoEditorPanel();

// Export toggle function for symbol palette
export function toggleGoEditor(): void {
    goEditorPanel.toggle();
}
