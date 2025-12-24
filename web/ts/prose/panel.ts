/**
 * Prose Panel - Markdown Documentation Viewer/Editor
 *
 * Shows markdown content with ProseMirror when clicking ▣ (prose) in the symbol palette.
 * Supports rich text editing of documentation and other markdown content.
 *
 * This module orchestrates the ProseEditor and ProseNavigation components.
 */

import { ProseEditor } from './editor.ts';
import { ProseNavigation } from './navigation.ts';
import { fetchDevMode } from '../dev-mode.ts';

class ProsePanel {
    private panel: HTMLElement | null = null;
    private overlay: HTMLElement | null = null;
    private isVisible: boolean = false;

    // Component modules
    private editor: ProseEditor;
    private navigation: ProseNavigation;

    // Event listener references for cleanup
    private escapeKeyHandler: ((e: KeyboardEvent) => void) | null = null;
    private saveKeyHandler: ((e: KeyboardEvent) => void) | null = null;
    private overlayClickHandler: (() => void) | null = null;

    constructor() {
        // Initialize component modules with callbacks
        this.editor = new ProseEditor({
            onDocumentLoad: (path: string) => {
                this.navigation.addToRecentDocs(path);
            }
        });

        this.navigation = new ProseNavigation({
            onDocumentSelect: (path: string) => {
                this.editor.loadDocument(path);
            }
        });

        this.initialize();
    }

    initialize(): void {
        // Create overlay element
        this.overlay = document.createElement('div');
        this.overlay.id = 'prose-overlay';
        this.overlay.className = 'prose-overlay hidden';

        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'prose-panel';
        this.panel.className = 'prose-panel hidden';
        this.panel.innerHTML = this.getTemplate();

        // Append to body
        document.body.appendChild(this.overlay);
        document.body.appendChild(this.panel);

        // Bind DOM elements to component modules
        this.editor.bindElements(this.panel);
        this.navigation.bindElements(this.panel);

        // Click overlay to close
        this.overlayClickHandler = () => this.handleClose();
        this.overlay.addEventListener('click', this.overlayClickHandler);

        // Escape key to close
        this.escapeKeyHandler = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.handleClose();
            }
        };
        document.addEventListener('keydown', this.escapeKeyHandler);

        // Setup event listeners
        this.setupEventListeners();
    }

    getTemplate(): string {
        return `
            <div class="prose-header">
                <div class="prose-title">
                    <span class="prose-icon">▣</span>
                    <span class="prose-name">Prose</span>
                    <span class="prose-breadcrumb"></span>
                </div>
                <button class="prose-close" aria-label="Close">✕</button>
            </div>
            <div class="prose-body">
                <div class="prose-sidebar">
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
                        <button id="theme-toggle" aria-label="Toggle theme">
                            <span class="theme-icon">☀</span>
                        </button>
                    </div>
                </div>
            </div>
        `;
    }

    setupEventListeners(): void {
        if (!this.panel) return;

        // Close button
        const closeBtn = this.panel.querySelector('.prose-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.handleClose());
        }

        // Save on Cmd/Ctrl+S
        this.saveKeyHandler = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 's' && this.isVisible) {
                e.preventDefault();
                this.editor.saveContent();
            }
        };
        document.addEventListener('keydown', this.saveKeyHandler);
    }

    handleClose(): void {
        if (this.editor.getHasUnsavedChanges()) {
            if (confirm('You have unsaved changes. Close anyway?')) {
                this.hide();
            }
        } else {
            this.hide();
        }
    }

    async show(): Promise<void> {
        if (!this.panel || !this.overlay) return;

        this.isVisible = true;
        this.overlay.classList.remove('hidden');
        this.overlay.classList.add('visible');
        this.panel.classList.remove('hidden');
        this.panel.classList.add('visible');

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

    hide(): void {
        if (!this.panel || !this.overlay) return;

        this.isVisible = false;
        this.panel.classList.remove('visible');
        this.panel.classList.add('hidden');
        this.overlay.classList.remove('visible');
        this.overlay.classList.add('hidden');

        // Clear URL fragment when closing
        if (window.location.hash) {
            window.history.replaceState(null, '', window.location.pathname + window.location.search);
        }

        // Clean up editor
        this.editor.destroy();
    }

    toggle(): void {
        if (this.isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    // Clean up event listeners (for proper resource management)
    destroy(): void {
        // Remove document-level event listeners
        if (this.escapeKeyHandler) {
            document.removeEventListener('keydown', this.escapeKeyHandler);
            this.escapeKeyHandler = null;
        }
        if (this.saveKeyHandler) {
            document.removeEventListener('keydown', this.saveKeyHandler);
            this.saveKeyHandler = null;
        }

        // Remove overlay listener
        if (this.overlay && this.overlayClickHandler) {
            this.overlay.removeEventListener('click', this.overlayClickHandler);
            this.overlayClickHandler = null;
        }

        // Clean up component modules
        this.editor.destroy();

        // Remove DOM elements
        if (this.panel) {
            this.panel.remove();
            this.panel = null;
        }
        if (this.overlay) {
            this.overlay.remove();
            this.overlay = null;
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
