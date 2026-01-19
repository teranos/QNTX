/**
 * CodeMirror 6 NodeView for ATS code blocks in ProseMirror
 * Enhanced with Pulse scheduling controls
 */

import { EditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import type { Node as PMNode } from 'prosemirror-model';
import type { EditorView as PMEditorView } from 'prosemirror-view';
import { createSchedulingControls } from '../../pulse/scheduling-controls';
import type { ScheduledJobResponse } from '../../pulse/types';
import { getScheduledJob } from '../../pulse/api';
import { hasPromptAction, openPromptFromAtsCode } from '../../prompt-editor-window';
import { SO } from '@generated/sym.js';

export class ATSCodeBlockNodeView {
    dom: HTMLElement;
    contentDOM: HTMLElement | null = null;
    private cmView: EditorView;
    private updating: boolean = false;
    private schedulingControls: HTMLElement | null = null;
    private promptInspectBtn: HTMLElement | null = null;
    private documentPath: string;

    constructor(
        private node: PMNode,
        private view: PMEditorView,
        private getPos: () => number | undefined,
        documentPath: string
    ) {
        this.documentPath = documentPath;
        // Create container
        this.dom = document.createElement('div');
        this.dom.className = 'code-block ats-code-block';

        // Create CodeMirror instance (simple, no line numbers)
        const initialContent = this.node.textContent;

        // TODO(issue #15): Cursor visibility issue - cursor is too thin/faint
        // The cursor should be thicker and more visible (currently appears very faint)
        // Tried: CSS !important, caretColor, borderLeftWidth - none seem to work reliably
        // May need to investigate CodeMirror's cursor rendering more deeply

        // Custom theme using CSS variables for easier theming
        const atsTheme = EditorView.theme({
            '&': {
                fontSize: 'var(--ats-editor-font-size, 14px)',
                fontFamily: "var(--ats-editor-font-family, 'JetBrains Mono', 'Fira Code', 'Consolas', monospace)"
            },
            '.cm-content': {
                caretColor: 'var(--ats-editor-caret-color, #66b3ff)',
                color: 'var(--ats-editor-text-color, #d4d4d4)',
                padding: 'var(--ats-editor-padding, 16px)'
            },
            '.cm-cursor, .cm-cursor-primary': {
                borderLeftColor: 'var(--ats-editor-caret-color, #66b3ff) !important',
                borderLeftWidth: 'var(--ats-editor-cursor-width, 3px) !important'
            },
            '.cm-line': {
                color: 'var(--ats-editor-text-color, #d4d4d4)'
            }
        });

        this.cmView = new EditorView({
            state: EditorState.create({
                doc: initialContent,
                extensions: [
                    atsTheme,
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

        // Add Pulse scheduling controls below CodeMirror editor
        this.renderSchedulingControls();

        // Add prompt inspect button if code contains "so prompt"
        this.renderPromptInspectButton();
    }

    /**
     * Check if ATS code contains a prompt action and render inspect button
     */
    private renderPromptInspectButton(): void {
        // Remove existing button if present
        if (this.promptInspectBtn) {
            this.promptInspectBtn.remove();
            this.promptInspectBtn = null;
        }

        const atsCode = this.cmView.state.doc.toString();

        // Only show button if code contains "so prompt"
        if (!hasPromptAction(atsCode)) {
            return;
        }

        // Create inspect button container
        this.promptInspectBtn = document.createElement('div');
        this.promptInspectBtn.className = 'ats-prompt-inspect';

        const btn = document.createElement('button');
        btn.className = 'ats-prompt-inspect-btn';
        btn.title = 'Open in Prompt Editor';

        const icon = document.createElement('span');
        icon.className = 'prompt-inspect-icon';
        icon.textContent = SO;

        const text = document.createElement('span');
        text.className = 'prompt-inspect-text';
        text.textContent = 'Inspect Prompt';

        btn.appendChild(icon);
        btn.appendChild(text);

        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            const currentCode = this.cmView.state.doc.toString();
            openPromptFromAtsCode(currentCode);
        });

        this.promptInspectBtn.appendChild(btn);
        this.dom.appendChild(this.promptInspectBtn);
    }

    private renderSchedulingControls(): void {
        // Remove existing controls if present
        if (this.schedulingControls) {
            this.schedulingControls.remove();
        }

        // Get ATS code from CodeMirror editor (more reliable than node.textContent)
        const atsCode = this.cmView.state.doc.toString();

        console.log('[ATS Block] Rendering scheduling controls:', {
            atsCode,
            atsCodeLength: atsCode.length,
            firstLine: atsCode.split('\n')[0],
            nodeTextContent: this.node.textContent,
            cmContent: atsCode,
            domElement: this.dom
        });

        // Load existing scheduled job if node has one
        const scheduledJobId = this.node.attrs.scheduledJobId;
        let existingJob: ScheduledJobResponse | undefined;

        // If there's a job ID, load it asynchronously
        if (scheduledJobId) {
            getScheduledJob(scheduledJobId)
                .then(job => {
                    existingJob = job;
                    this.updateSchedulingControls(existingJob);
                })
                .catch((error: unknown) => {
                    console.warn('Failed to load scheduled job:', error);
                    // Clear invalid job ID from node
                    this.updateNodeAttributes({ scheduledJobId: null });
                });
        }

        // Create scheduling controls
        this.schedulingControls = createSchedulingControls({
            atsCode: () => this.cmView.state.doc.toString(),  // Always get fresh content
            documentId: this.documentPath, // Prose document path for linking back
            existingJob,
            onJobCreated: (job: ScheduledJobResponse) => {
                this.updateNodeAttributes({ scheduledJobId: job.id });
                this.updateSchedulingControls(job);
            },
            onJobUpdated: (job: ScheduledJobResponse) => {
                this.updateSchedulingControls(job);
            },
            onJobDeleted: () => {
                this.updateNodeAttributes({ scheduledJobId: null });
                this.renderSchedulingControls();
            },
            onError: (error: Error) => {
                console.error('Scheduling error:', error);
                // Show error inline in the scheduling controls
                this.showSchedulingError(error.message);
            },
        });

        this.dom.appendChild(this.schedulingControls);
    }

    private updateSchedulingControls(job: ScheduledJobResponse): void {
        // Re-render controls with updated job
        if (this.schedulingControls) {
            this.schedulingControls.remove();
        }

        // Create new controls with the updated job
        this.schedulingControls = createSchedulingControls({
            atsCode: () => this.cmView.state.doc.toString(),
            documentId: this.documentPath,
            existingJob: job,  // Pass the updated job directly
            onJobCreated: (job: ScheduledJobResponse) => {
                this.updateNodeAttributes({ scheduledJobId: job.id });
                this.updateSchedulingControls(job);
            },
            onJobUpdated: (job: ScheduledJobResponse) => {
                this.updateSchedulingControls(job);
            },
            onJobDeleted: () => {
                this.updateNodeAttributes({ scheduledJobId: null });
                this.renderSchedulingControls();
            },
            onError: (error: Error) => {
                console.error('Scheduling error:', error);
                this.showSchedulingError(error.message);
            },
        });

        this.dom.appendChild(this.schedulingControls);
    }

    private showSchedulingError(message: string): void {
        if (!this.schedulingControls) return;

        // Remove any existing error
        const existingError = this.schedulingControls.querySelector('.pulse-error');
        if (existingError) existingError.remove();

        // Create error element
        const errorEl = document.createElement('div');
        errorEl.className = 'pulse-error';
        errorEl.textContent = message;

        // Insert at the top of scheduling controls
        this.schedulingControls.insertBefore(errorEl, this.schedulingControls.firstChild);

        // Auto-dismiss after 8 seconds
        setTimeout(() => errorEl.remove(), 8000);
    }

    private updateNodeAttributes(attrs: Record<string, any>): void {
        const pos = this.getPos();
        if (pos === undefined) return;

        const tr = this.view.state.tr.setNodeMarkup(pos, null, {
            ...this.node.attrs,
            ...attrs,
        });
        this.view.dispatch(tr);
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
                this.view.state.schema.nodes.ats_code_block.create(
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

            // Re-check for prompt actions after content changes
            this.renderPromptInspectButton();
        }

        return true;
    }

    destroy(): void {
        this.cmView.destroy();
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
