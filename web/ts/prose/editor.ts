/**
 * Prose Editor - ProseMirror integration for markdown editing
 *
 * Handles document loading, ProseMirror editor initialization, content saving,
 * and UI feedback (breadcrumb, save indicator, status messages).
 */

import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { history, undo, redo } from 'prosemirror-history';
import { keymap } from 'prosemirror-keymap';
import { baseKeymap } from 'prosemirror-commands';
import { apiFetch } from '../api.ts';
import { proseMarkdownParser, proseMarkdownSerializer } from './markdown.ts';
import { ATSCodeBlockNodeView } from './nodes/ats-code-block.ts';
import { GoCodeBlockNodeView } from './nodes/go-code-block.ts';
import { proseInputRules } from './input-rules.ts';
import { handleError, SEG } from '../error-handler.ts';

export interface ProseEditorCallbacks {
    onDocumentLoad?: (path: string) => void;
    onContentChange?: (hasChanges: boolean) => void;
}

export class ProseEditor {
    private editorView: EditorView | null = null;
    private currentPath: string = '';
    private hasUnsavedChanges: boolean = false;
    private isDevMode: boolean = false;
    private callbacks: ProseEditorCallbacks;

    // DOM elements
    private editorContainer: HTMLElement | null = null;
    private breadcrumbElement: HTMLElement | null = null;
    private saveIndicator: HTMLElement | null = null;
    private statusElement: HTMLElement | null = null;

    constructor(callbacks: ProseEditorCallbacks = {}) {
        this.callbacks = callbacks;
    }

    setDevMode(isDevMode: boolean): void {
        this.isDevMode = isDevMode;
    }

    bindElements(panel: HTMLElement): void {
        this.editorContainer = panel.querySelector('#prose-editor');
        this.breadcrumbElement = panel.querySelector('.prose-breadcrumb');
        this.saveIndicator = panel.querySelector('.prose-save-indicator');
        this.statusElement = panel.querySelector('.prose-status-text');
    }

    async loadDocument(path: string): Promise<void> {
        try {
            const response = await apiFetch(`/api/prose/${path}`);
            if (!response.ok) {
                throw new Error(`Failed to load document: ${response.statusText}`);
            }

            const content = await response.text();
            this.currentPath = path;
            this.initializeEditor(content);
            this.updateBreadcrumb(path);

            // Update URL fragment for deep linking
            window.history.replaceState(null, '', `#${path}`);

            // Notify parent
            if (this.callbacks.onDocumentLoad) {
                this.callbacks.onDocumentLoad(path);
            }
        } catch (error) {
            handleError(error, `Failed to load document: ${path}`, { context: SEG.ERROR, silent: true });
            this.showError(`Failed to load ${path}`);
        }
    }

    initializeEditor(markdownContent: string): void {
        if (!this.editorContainer) {
            console.error('Editor container not bound');
            return;
        }

        // Parse markdown to ProseMirror document using custom parser
        let doc;
        try {
            doc = proseMarkdownParser.parse(markdownContent);
        } catch (error) {
            handleError(error, 'Failed to parse markdown', { context: SEG.ERROR, silent: true });
            this.showError('Failed to parse document');
            return;
        }

        // Create editor state
        const state = EditorState.create({
            doc,
            plugins: [
                proseInputRules,
                history(),
                keymap({
                    'Mod-z': undo,
                    'Mod-y': redo,
                    'Mod-Shift-z': redo
                }),
                keymap(baseKeymap)
            ]
        });

        // Destroy existing editor if present
        if (this.editorView) {
            this.editorView.destroy();
        }

        // Create new editor view with custom NodeViews for ats_code_block and go_code_block
        this.editorView = new EditorView(this.editorContainer, {
            state,
            nodeViews: {
                ats_code_block: (node, view, getPos) => new ATSCodeBlockNodeView(node, view, getPos as () => number, this.currentPath),
                go_code_block: (node, view, getPos) => new GoCodeBlockNodeView(node, view, getPos as () => number)
            },
            dispatchTransaction: (transaction) => {
                if (!this.editorView) return;
                const newState = this.editorView.state.apply(transaction);
                this.editorView.updateState(newState);

                // Track changes for save indicator
                if (transaction.docChanged) {
                    this.hasUnsavedChanges = true;
                    this.updateSaveIndicator();
                    if (this.callbacks.onContentChange) {
                        this.callbacks.onContentChange(true);
                    }
                }
            },
            editable: () => this.isDevMode,
            attributes: {
                spellcheck: 'false'
            }
        });

        this.hasUnsavedChanges = false;
        this.updateSaveIndicator();
    }

    async saveContent(): Promise<void> {
        if (!this.editorView || !this.hasUnsavedChanges || !this.isDevMode) return;

        try {
            const doc = this.editorView.state.doc;
            const markdown = proseMarkdownSerializer.serialize(doc);

            const response = await apiFetch(`/api/prose/${this.currentPath}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'text/markdown' },
                body: markdown
            });

            if (!response.ok) {
                throw new Error(`Failed to save: ${response.statusText}`);
            }

            this.hasUnsavedChanges = false;
            this.updateSaveIndicator();
            this.showStatus('Saved');
            if (this.callbacks.onContentChange) {
                this.callbacks.onContentChange(false);
            }
        } catch (error) {
            handleError(error, 'Failed to save content', { context: SEG.ERROR, silent: true });
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
            this.saveIndicator.textContent = 'Unsaved changes';
            this.saveIndicator.classList.remove('hidden');
        } else {
            this.saveIndicator.classList.add('hidden');
        }
    }

    showStatus(message: string): void {
        if (!this.statusElement) return;
        this.statusElement.textContent = message;
        this.statusElement.classList.remove('error');
    }

    showError(message: string): void {
        if (!this.statusElement) return;
        this.statusElement.textContent = message;
        this.statusElement.classList.add('error');
        setTimeout(() => {
            if (this.statusElement) {
                this.statusElement.classList.remove('error');
            }
        }, 3000);
    }

    destroy(): void {
        if (this.editorView) {
            this.editorView.destroy();
            this.editorView = null;
        }
    }

    getCurrentPath(): string {
        return this.currentPath;
    }

    getHasUnsavedChanges(): boolean {
        return this.hasUnsavedChanges;
    }
}
