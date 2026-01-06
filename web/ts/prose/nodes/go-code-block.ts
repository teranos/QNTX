/**
 * CodeMirror 6 NodeView for Go code blocks in ProseMirror
 * Enhanced with gopls LSP integration via WebSocket
 */

import { EditorView } from '@codemirror/view';
import { EditorState, Compartment } from '@codemirror/state';
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import type { Node as PMNode } from 'prosemirror-model';
import type { EditorView as PMEditorView } from 'prosemirror-view';
import { getGoBlockTheme, onThemeChange, type ThemeMode } from '../../codemirror-themes';

export class GoCodeBlockNodeView {
    dom: HTMLElement;
    contentDOM: HTMLElement | null = null;
    private cmView: EditorView | null = null;
    private updating: boolean = false;
    private themeCompartment = new Compartment();
    private themeUnsubscribe: (() => void) | null = null;

    constructor(
        private node: PMNode,
        private view: PMEditorView,
        private getPos: () => number | undefined
    ) {
        // Create container
        this.dom = document.createElement('div');
        this.dom.className = 'code-block go-code-block';

        // Initialize editor asynchronously to load Go language support
        this.initializeEditor();
    }

    private async initializeEditor(): Promise<void> {
        // Load Go language support dynamically to avoid bundling issues
        let goExtension;
        try {
            const goModule = await import('@codemirror/lang-go');
            goExtension = goModule.go();
        } catch (err) {
            console.error('[Go Block] Go language support unavailable:', err);
            goExtension = [];  // Editor works without syntax highlighting
        }

        // Create CodeMirror instance
        const initialContent = this.node.textContent;

        this.cmView = new EditorView({
            state: EditorState.create({
                doc: initialContent,
                extensions: [
                    goExtension,  // Go language support (empty if unavailable)
                    syntaxHighlighting(defaultHighlightStyle), // Apply syntax highlighting theme
                    // Theme via compartment for dynamic switching
                    this.themeCompartment.of(getGoBlockTheme()),
                    EditorView.lineWrapping,

                    EditorView.updateListener.of((update) => {
                        if (this.updating) return;
                        if (!update.docChanged) return;

                        // Sync CodeMirror changes back to ProseMirror
                        const newContent = update.state.doc.toString();
                        this.syncToProseMirror(newContent);
                    })
                ]
            }),
            parent: this.dom
        });

        // Subscribe to theme changes
        this.themeUnsubscribe = onThemeChange((_mode: ThemeMode) => {
            if (this.cmView) {
                this.cmView.dispatch({
                    effects: this.themeCompartment.reconfigure(getGoBlockTheme())
                });
            }
        });

        console.log('[Go Block] CodeMirror initialized with Go syntax highlighting');
    }

    private syncToProseMirror(content: string): void {
        if (this.updating) return;

        const pos = this.getPos();
        if (pos === undefined) return;

        try {
            this.updating = true;

            const tr = this.view.state.tr.replaceWith(
                pos,
                pos + this.node.nodeSize,
                this.view.state.schema.nodes.go_code_block.create(
                    this.node.attrs,
                    content ? this.view.state.schema.text(content) : undefined
                )
            );

            this.view.dispatch(tr);
        } finally {
            this.updating = false;
        }
    }

    update(node: PMNode): boolean {
        // Only handle updates to the same node type
        if (node.type !== this.node.type) return false;
        if (!this.cmView) return true; // Not initialized yet

        this.node = node;

        // Sync ProseMirror changes to CodeMirror
        const newContent = node.textContent;
        if (this.cmView.state.doc.toString() !== newContent) {
            try {
                this.updating = true;
                this.cmView.dispatch({
                    changes: {
                        from: 0,
                        to: this.cmView.state.doc.length,
                        insert: newContent
                    }
                });
            } finally {
                this.updating = false;
            }
        }

        return true;
    }

    destroy(): void {
        // Unsubscribe from theme changes
        if (this.themeUnsubscribe) {
            this.themeUnsubscribe();
            this.themeUnsubscribe = null;
        }
        if (this.cmView) {
            this.cmView.destroy();
        }
    }

    selectNode(): void {
        this.dom.classList.add('ProseMirror-selectednode');
    }

    deselectNode(): void {
        this.dom.classList.remove('ProseMirror-selectednode');
    }

    stopEvent(): boolean {
        // Let CodeMirror handle all events inside the block
        return true;
    }

    ignoreMutation(): boolean {
        // Ignore mutations - we handle sync ourselves
        return true;
    }
}
