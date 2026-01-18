/**
 * CodeMirror 6 NodeView for YAML frontmatter blocks in ProseMirror
 * Collapsible metadata section with subtle dark theme styling
 */

import { EditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import type { Node as PMNode } from 'prosemirror-model';
import type { EditorView as PMEditorView } from 'prosemirror-view';

export class FrontmatterNodeView {
    dom: HTMLElement;
    contentDOM: HTMLElement | null = null;
    private cmView: EditorView | null = null;
    private updating: boolean = false;
    private editorContainer: HTMLElement;
    private collapseButton: HTMLElement;
    private isCollapsed: boolean = false;

    constructor(
        private node: PMNode,
        private view: PMEditorView,
        private getPos: () => number | undefined
    ) {
        // Create container
        this.dom = document.createElement('div');
        this.dom.className = 'frontmatter-block';

        // Create collapse button
        this.collapseButton = document.createElement('button');
        this.collapseButton.className = 'frontmatter-collapse';
        this.collapseButton.textContent = '▼';
        this.collapseButton.setAttribute('aria-label', 'Toggle frontmatter');
        this.collapseButton.addEventListener('click', () => this.toggleCollapse());
        this.dom.appendChild(this.collapseButton);

        // Create editor container
        this.editorContainer = document.createElement('div');
        this.editorContainer.className = 'frontmatter-editor';
        this.dom.appendChild(this.editorContainer);

        // Initialize editor asynchronously to load YAML language support
        this.initializeEditor();
    }

    private toggleCollapse(): void {
        this.isCollapsed = !this.isCollapsed;
        this.editorContainer.style.display = this.isCollapsed ? 'none' : 'block';
        this.collapseButton.textContent = this.isCollapsed ? '▶' : '▼';
    }

    private async initializeEditor(): Promise<void> {
        // Load YAML language support dynamically
        let yamlExtension;
        try {
            const yamlModule = await import('@codemirror/lang-yaml');
            yamlExtension = yamlModule.yaml();
        } catch (error: unknown) {
            console.error('[Frontmatter Block] YAML language support unavailable:', error);
            yamlExtension = [];
        }

        const initialContent = this.node.textContent;

        // Dark theme matching the prose editor
        const frontmatterTheme = EditorView.theme({
            '&': {
                fontSize: '13px',
                fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
                backgroundColor: '#1a1a1a'
            },
            '.cm-content': {
                caretColor: '#888',
                padding: '8px 12px'
            },
            '.cm-cursor, .cm-cursor-primary': {
                borderLeftColor: '#888 !important',
                borderLeftWidth: '2px !important'
            },
            '.cm-gutters': {
                backgroundColor: '#1a1a1a',
                color: '#666',
                border: 'none'
            },
            '.cm-activeLineGutter': {
                backgroundColor: '#252525'
            },
            '.cm-line': {
                padding: '0 4px'
            }
        });

        this.cmView = new EditorView({
            state: EditorState.create({
                doc: initialContent,
                extensions: [
                    yamlExtension,
                    syntaxHighlighting(defaultHighlightStyle),
                    frontmatterTheme,
                    EditorView.lineWrapping,

                    EditorView.updateListener.of((update) => {
                        if (this.updating) return;
                        if (!update.docChanged) return;

                        const newContent = update.state.doc.toString();
                        this.syncToProseMirror(newContent);
                    })
                ]
            }),
            parent: this.editorContainer
        });
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
                this.view.state.schema.nodes.frontmatter_block.create(
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
        if (node.type !== this.node.type) return false;
        if (!this.cmView) return true;

        this.node = node;

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

    stopEvent(event: Event): boolean {
        // Let collapse button clicks through
        if (event.target === this.collapseButton) {
            return false;
        }
        // Let CodeMirror handle all other events
        return true;
    }

    ignoreMutation(): boolean {
        return true;
    }
}
