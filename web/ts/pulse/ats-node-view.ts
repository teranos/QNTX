/**
 * ATS Code Block Node View with Pulse Scheduling
 *
 * Custom ProseMirror node view that adds scheduling controls to ATS blocks
 */

import { Node as PMNode } from "prosemirror-model";
import { EditorView, NodeView } from "prosemirror-view";
import type { ScheduledJobResponse } from "./types.ts";
import { createSchedulingControls } from "./scheduling-controls.ts";

export interface ATSNodeViewOptions {
  documentId?: string;
  onScheduleChange?: (jobId: string | null) => void;
}

/**
 * Custom node view for ATS code blocks with scheduling support
 */
export class ATSNodeView implements NodeView {
  dom: HTMLElement;
  contentDOM: HTMLElement;
  private schedulingControls: HTMLElement | null = null;
  private currentJob: ScheduledJobResponse | null = null;

  constructor(
    private node: PMNode,
    private view: EditorView,
    private getPos: () => number,
    private options: ATSNodeViewOptions = {}
  ) {
    // Create wrapper element
    this.dom = document.createElement("div");
    this.dom.className = "ats-code-block-wrapper";

    // Create code block header
    const header = document.createElement("div");
    header.className = "ats-code-block-header";
    header.innerHTML = `
      <span class="ats-language-label">ats</span>
    `;
    this.dom.appendChild(header);

    // TODO(issue #8): Add real-time execution state indicators
    // Show block execution state (idle/running/completed/failed) via color changes
    // Subscribe to WebSocket events: pulse_execution_started, pulse_execution_completed, pulse_execution_failed

    // Create content container for CodeMirror
    this.contentDOM = document.createElement("div");
    this.contentDOM.className = "ats-code-block-content";
    this.dom.appendChild(this.contentDOM);

    // Check if this block has a scheduled job
    const scheduledJobId = this.node.attrs.scheduledJobId;
    if (scheduledJobId) {
      this.loadScheduledJob(scheduledJobId);
    } else {
      // Show "Add Schedule" button
      this.renderSchedulingControls();
    }
  }

  /**
   * Load existing scheduled job data
   */
  private async loadScheduledJob(jobId: string): Promise<void> {
    try {
      const response = await fetch(`/api/pulse/schedules/${jobId}`);
      if (response.ok) {
        this.currentJob = await response.json();
        this.renderSchedulingControls();
      }
    } catch (error) {
      console.error("Failed to load scheduled job:", error);
      this.renderSchedulingControls();
    }
  }

  /**
   * Render scheduling controls UI
   */
  private renderSchedulingControls(): void {
    // Remove existing controls if present
    if (this.schedulingControls) {
      this.schedulingControls.remove();
    }

    // Get ATS code from node
    const atsCode = this.node.textContent;

    // Create new controls
    this.schedulingControls = createSchedulingControls({
      atsCode,
      documentId: this.options.documentId,
      existingJob: this.currentJob || undefined,
      onJobCreated: (job) => {
        this.currentJob = job;
        this.updateNodeAttributes({ scheduledJobId: job.id });
        this.options.onScheduleChange?.(job.id);
      },
      onJobUpdated: (job) => {
        this.currentJob = job;
      },
      onJobDeleted: () => {
        this.currentJob = null;
        this.updateNodeAttributes({ scheduledJobId: null });
        this.options.onScheduleChange?.(null);
        this.renderSchedulingControls();
      },
      onError: (error, context) => {
        console.error("Scheduling error:", error, context);
        // Show inline error (less intrusive than toast for block-level actions)
        this.showSchedulingError(error.message);
      },
    });

    this.dom.appendChild(this.schedulingControls);
  }

  /**
   * Show inline error message in scheduling controls
   */
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

  /**
   * Update node attributes in ProseMirror document
   */
  private updateNodeAttributes(attrs: Record<string, any>): void {
    const pos = this.getPos();
    const tr = this.view.state.tr.setNodeMarkup(pos, null, {
      ...this.node.attrs,
      ...attrs,
    });
    this.view.dispatch(tr);
  }

  /**
   * Update handler called when node content changes
   */
  update(node: PMNode): boolean {
    if (node.type !== this.node.type) {
      return false;
    }

    this.node = node;

    // If code content changed, re-render scheduling controls
    // (ATS code might have changed, need to sync with scheduled job)
    const newCode = node.textContent;
    if (this.currentJob && this.currentJob.ats_code !== newCode) {
      // ATS code changed - could update the scheduled job or show warning
      console.warn("ATS code changed but scheduled job exists");
    }

    return true;
  }

  /**
   * Cleanup when node view is destroyed
   */
  destroy(): void {
    if (this.schedulingControls) {
      this.schedulingControls.remove();
    }
  }

  /**
   * Ignore mutations to scheduling controls (they're not part of document content)
   */
  ignoreMutation(
    mutation: MutationRecord | { type: "selection"; target: Node }
  ): boolean {
    // Ignore mutations to scheduling controls
    if (this.schedulingControls?.contains(mutation.target as Node)) {
      return true;
    }
    return false;
  }
}

/**
 * Create node view factory for ProseMirror
 */
export function createATSNodeViewFactory(options: ATSNodeViewOptions = {}) {
  return (node: PMNode, view: EditorView, getPos: () => number | undefined): NodeView => {
    return new ATSNodeView(node, view, getPos as () => number, options);
  };
}
