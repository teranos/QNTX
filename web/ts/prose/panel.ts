/**
 * Prose Panel - Markdown Documentation Viewer/Editor
 *
 * Shows markdown content with ProseMirror when clicking ▣ (prose) in the symbol palette.
 * Supports rich text editing of documentation and other markdown content.
 *
 * This module orchestrates the ProseEditor and ProseNavigation components.
 */

import { BasePanel } from '../base-panel.ts';
import { ProseEditor } from './editor.ts';
import { ProseNavigation } from './navigation.ts';
import { PromptPreviewPanel } from './prompt-preview-panel.ts';
import { fetchDevMode } from '../dev-mode.ts';

class ProsePanel extends BasePanel {
    // Component modules
    private editor: ProseEditor;
    private navigation: ProseNavigation;
    private promptPreview: PromptPreviewPanel;

    // Save keyboard handler (separate from escape, which BasePanel handles)
    private saveKeyHandler: ((e: KeyboardEvent) => void) | null = null;

    constructor() {
        super({
            id: 'prose-panel',
            classes: ['prose-panel'],
            useOverlay: true,
            closeOnEscape: true,
            slideFromRight: true
        });

        // Initialize component modules with callbacks
        this.editor = new ProseEditor({
            onDocumentLoad: (path: string) => {
                this.navigation.addToRecentDocs(path);
                this.checkPromptFile(path);
            }
        });

        this.navigation = new ProseNavigation({
            onDocumentSelect: (path: string) => {
                this.editor.loadDocument(path);
            }
        });

        // Initialize prompt preview panel
        this.promptPreview = new PromptPreviewPanel({
            onClose: () => {
                // Adjust Prose panel width when preview closes
                this.adjustPanelWidth(false);
            },
            getEditorContent: () => this.editor.getContent()
        });

        // TODO(issue #11): Implement Layout rendering modes for DocBlock views
        // Support list, cluster, timeline, and radial layout modes
        // Render views based on Layout field in DocBlock YAML headers

        // TODO(issue #12): Connect view DocBlocks to live ATS data via API
        // When view DocBlocks are implemented, fetch data from /api/view/render
        // Replace placeholder rendering with live attestation data

        // Bind DOM elements to component modules
        if (this.panel) {
            this.editor.bindElements(this.panel);
            this.navigation.bindElements(this.panel);
        }
    }

    protected getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon">▣</span>
                    <span class="prose-name">Prose</span>
                    <span class="prose-breadcrumb"></span>
                </div>
                <button class="panel-close" aria-label="Close">✕</button>
            </div>
            <div class="prose-body">
                <div class="prose-sidebar">
                    <button class="prose-sidebar-toggle" aria-label="Toggle sidebar" title="Toggle sidebar"></button>
                    <div class="prose-sidebar-header">
                        <input type="text" class="prose-search" placeholder="Search documentation..." />
                    </div>
                    <div class="prose-recent" id="prose-recent">
                        <!-- Recent docs will be populated here -->
                    </div>
                    <div class="prose-tree" id="prose-tree">
                        <!-- Tree will be populated here -->
                    </div>
                </div>
                <div class="prose-content">
                    <div class="prose-editor-container">
                        <div id="prose-editor"></div>
                    </div>
                    <div class="prose-status">
                        <span class="prose-status-text"></span>
                        <span class="prose-save-indicator hidden">●</span>
                    </div>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Close button is handled automatically by BasePanel

        // Sidebar toggle
        const sidebarToggle = this.panel?.querySelector('.prose-sidebar-toggle');
        const sidebar = this.panel?.querySelector('.prose-sidebar');
        const body = this.panel?.querySelector('.prose-body');

        sidebarToggle?.addEventListener('click', () => {
            sidebar?.classList.toggle('collapsed');
            body?.classList.toggle('sidebar-collapsed');
        });

        // Save on Cmd/Ctrl+S
        this.saveKeyHandler = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 's' && this.isVisible) {
                e.preventDefault();
                this.editor.saveContent();
            }
        };
        document.addEventListener('keydown', this.saveKeyHandler);
    }

    protected beforeHide(): boolean {
        if (this.editor.getHasUnsavedChanges()) {
            return confirm('You have unsaved changes. Close anyway?');
        }
        return true;
    }

    protected async onShow(): Promise<void> {
        // Fetch dev mode status and set on editor
        const isDevMode = await fetchDevMode();
        this.editor.setDevMode(isDevMode);

        // Load navigation tree and recent docs
        await this.navigation.refresh();

        // Load document from URL fragment, or last viewed doc, or default to index.md
        const fragment = window.location.hash.slice(1); // Remove '#' prefix
        const lastViewed = this.navigation.getLastViewedDoc();
        const docPath = fragment || lastViewed || 'index.md';
        await this.editor.loadDocument(docPath);
    }

    protected onHide(): void {
        // Clear URL fragment when closing
        if (window.location.hash) {
            window.history.replaceState(null, '', window.location.pathname + window.location.search);
        }

        // Clean up editor
        this.editor.destroy();
    }

    protected onDestroy(): void {
        // Remove save key handler
        if (this.saveKeyHandler) {
            document.removeEventListener('keydown', this.saveKeyHandler);
            this.saveKeyHandler = null;
        }

        // Clean up component modules
        this.editor.destroy();
        this.promptPreview.destroy();
    }

    /**
     * Check if the loaded document is a prompt file and update preview panel
     */
    private checkPromptFile(path: string): void {
        // Check if file has prompt indicators:
        // 1. Contains .prompt in filename (e.g., recipe.prompt.md)
        // 2. Has frontmatter with type: prompt-template
        // 3. Is in a prompts/ directory
        const isPromptFile =
            path.includes('.prompt.') ||
            path.includes('/prompts/') ||
            path.startsWith('prompts/');

        this.promptPreview.setPromptFileActive(isPromptFile);

        // Auto-show preview panel for prompt files
        if (isPromptFile) {
            this.promptPreview.show();
            this.adjustPanelWidth(true);
        }
    }

    /**
     * Adjust the Prose panel width when preview panel is shown/hidden
     */
    private adjustPanelWidth(hasPreview: boolean): void {
        if (!this.panel) return;

        // When preview is shown, reduce Prose panel width to make room
        if (hasPreview) {
            this.panel.style.width = '40%';
        } else {
            this.panel.style.width = ''; // Reset to CSS default
        }
    }
}

// Create and export instance
const prosePanel = new ProsePanel();

// Export for use in other modules
export function showProsePanel(): void {
    prosePanel.show();
}

export function hideProsePanel(): void {
    prosePanel.hide();
}

export function toggleProsePanel(): void {
    prosePanel.toggle();
}

export async function showProseDocument(docId: string): Promise<void> {
    await prosePanel.show();
    // TODO: Navigate to specific document once we have document ID resolution
    console.log('[Prose Panel] Request to show document:', docId);
}
