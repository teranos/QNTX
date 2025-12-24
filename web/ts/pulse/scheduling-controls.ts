/**
 * Scheduling Controls Component
 *
 * Inline UI controls for ATS code blocks to configure Pulse scheduling
 */

import { debugLog } from "../debug.ts";
import type { ScheduledJob, ScheduledJobState } from "./types.ts";
import { INTERVAL_PRESETS, formatInterval } from "./types.ts";
import {
  createScheduledJob,
  updateScheduledJob,
  pauseScheduledJob,
  resumeScheduledJob,
  deleteScheduledJob,
} from "./api.ts";

export interface SchedulingControlsOptions {
  /**
   * ATS code to schedule - can be static string or getter function.
   *
   * Use function when content may change dynamically (e.g., CodeMirror editor).
   * The function will be called each time fresh code is needed (validation, submission).
   *
   * @example
   * // Static ATS code
   * { atsCode: "ix https://example.com/api/data" }
   *
   * @example
   * // Dynamic code from CodeMirror (always fetches latest content)
   * { atsCode: () => this.cmView.state.doc.toString() }
   */
  atsCode: string | (() => string);

  documentId?: string;
  existingJob?: ScheduledJob;
  onJobCreated?: (job: ScheduledJob) => void;
  onJobUpdated?: (job: ScheduledJob) => void;
  onJobDeleted?: () => void;
  onError?: (error: Error, context?: ErrorContext) => void;
}

export interface ErrorContext {
  atsCode?: string;
  intervalSeconds?: number;
  documentId?: string;
  action?: string;
}

// Helper to resolve atsCode (string or function)
function resolveAtsCode(atsCode: string | (() => string)): string {
  return typeof atsCode === 'function' ? atsCode() : atsCode;
}

/**
 * Create scheduling controls DOM element
 */
export function createSchedulingControls(
  options: SchedulingControlsOptions
): HTMLElement {
  const container = document.createElement("div");
  container.className = "pulse-scheduling-controls";

  if (options.existingJob) {
    // Show existing job controls
    renderExistingJobControls(container, options);
  } else {
    // Show "Add Schedule" button
    renderAddScheduleButton(container, options);
  }

  return container;
}

/**
 * Render controls for existing scheduled job
 */
function renderExistingJobControls(
  container: HTMLElement,
  options: SchedulingControlsOptions
): void {
  const job = options.existingJob!;
  const isActive = job.state === "active";

  container.innerHTML = `
    <div class="pulse-controls-row">
      <div class="pulse-schedule-badge ${job.state}">
        <span class="pulse-icon">꩜</span>
        <span class="pulse-interval">${formatInterval(job.interval_seconds)}</span>
        <span class="pulse-state">${job.state}</span>
      </div>
      <div class="pulse-schedule-actions">
        <button class="pulse-btn-pause" title="${isActive ? "Pause job" : "Resume job"}">
          ${isActive ? "Pause" : "Resume"}
        </button>
        <select class="pulse-interval-select">
          ${INTERVAL_PRESETS.map(
            (preset) => `
            <option value="${preset.seconds}" ${preset.seconds === job.interval_seconds ? "selected" : ""}>
              ${preset.label}
            </option>
          `
          ).join("")}
          <option value="custom">Custom...</option>
        </select>
        <button class="pulse-btn-delete" title="Remove schedule">Delete</button>
      </div>
    </div>
  `;

  // Attach event listeners
  const pauseBtn = container.querySelector(".pulse-btn-pause") as HTMLButtonElement;
  const intervalSelect = container.querySelector(
    ".pulse-interval-select"
  ) as HTMLSelectElement;
  const deleteBtn = container.querySelector(".pulse-btn-delete") as HTMLButtonElement;

  pauseBtn.addEventListener("click", async () => {
    try {
      const updatedJob = isActive
        ? await pauseScheduledJob(job.id)
        : await resumeScheduledJob(job.id);
      options.onJobUpdated?.(updatedJob);
      // Re-render with updated job
      renderExistingJobControls(container, {
        ...options,
        existingJob: updatedJob,
      });
    } catch (error) {
      options.onError?.(error as Error, {
        action: isActive ? 'pause' : 'resume',
      });
    }
  });

  intervalSelect.addEventListener("change", async () => {
    const value = intervalSelect.value;
    if (value === "custom") {
      const customValue = prompt("Enter interval (e.g., 30m, 2h, 1d):");
      if (!customValue) {
        intervalSelect.value = job.interval_seconds.toString();
        return;
      }
      // TODO: Parse custom interval
      return;
    }

    try {
      const newInterval = parseInt(value, 10);
      const updatedJob = await updateScheduledJob(job.id, {
        interval_seconds: newInterval,
      });
      options.onJobUpdated?.(updatedJob);
      renderExistingJobControls(container, {
        ...options,
        existingJob: updatedJob,
      });
    } catch (error) {
      options.onError?.(error as Error, {
        action: 'change interval',
        intervalSeconds: parseInt(value, 10),
      });
    }
  });

  deleteBtn.addEventListener("click", async () => {
    if (!confirm("Remove this scheduled job?")) return;

    try {
      await deleteScheduledJob(job.id);
      options.onJobDeleted?.();
      // Re-render to show "Add Schedule" button
      container.innerHTML = "";
      renderAddScheduleButton(container, options);
    } catch (error) {
      options.onError?.(error as Error, {
        action: 'delete',
        atsCode: job.ats_code,
      });
    }
  });
}

/**
 * Render "Add Schedule" button for unscheduled ATS blocks
 */
function renderAddScheduleButton(
  container: HTMLElement,
  options: SchedulingControlsOptions
): void {
  // Check if ATS code is empty
  const atsCode = resolveAtsCode(options.atsCode);
  const isEmpty = !atsCode || atsCode.trim() === '';

  container.innerHTML = `
    <button class="pulse-btn-add-schedule" ${isEmpty ? 'disabled' : ''} ${isEmpty ? 'title="Add ATS code to enable scheduling"' : ''}>
      <span class="pulse-icon">꩜</span>
      Add Schedule
    </button>
  `;

  const addBtn = container.querySelector(
    ".pulse-btn-add-schedule"
  ) as HTMLButtonElement;

  addBtn.addEventListener("click", () => {
    // Double-check ATS code isn't empty (button should be disabled, but validate anyway)
    const currentCode = resolveAtsCode(options.atsCode);
    if (!currentCode || currentCode.trim() === '') {
      return; // Do nothing if code is empty
    }

    // Show interval selection dropdown
    renderIntervalSelection(container, options);
  });
}

/**
 * Render interval selection UI (expanded from "Add Schedule" button)
 */
function renderIntervalSelection(
  container: HTMLElement,
  options: SchedulingControlsOptions
): void {
  container.innerHTML = `
    <div class="pulse-interval-picker">
      <span class="pulse-label">Run every:</span>
      <select class="pulse-interval-select">
        ${INTERVAL_PRESETS.map(
          (preset) => `
          <option value="${preset.seconds}">${preset.label}</option>
        `
        ).join("")}
        <option value="custom">Custom...</option>
      </select>
      <button class="pulse-btn-confirm">✓</button>
      <button class="pulse-btn-cancel">✗</button>
    </div>
  `;

  const intervalSelect = container.querySelector(
    ".pulse-interval-select"
  ) as HTMLSelectElement;
  const confirmBtn = container.querySelector(".pulse-btn-confirm") as HTMLButtonElement;
  const cancelBtn = container.querySelector(".pulse-btn-cancel") as HTMLButtonElement;

  confirmBtn.addEventListener("click", async () => {
    const value = intervalSelect.value;
    if (value === "custom") {
      // TODO: Show custom interval input
      return;
    }

    try {
      const intervalSeconds = parseInt(value, 10);
      const atsCode = resolveAtsCode(options.atsCode);

      // Validate ATS code before sending
      if (!atsCode || atsCode.trim() === '') {
        throw new Error('ATS code is empty - cannot schedule empty query. Try refreshing the page.');
      }

      debugLog('[Scheduling Controls] Creating job with:', {
        atsCode,
        intervalSeconds,
        documentId: options.documentId,
        hasDocumentId: !!options.documentId
      });

      const request = {
        ats_code: atsCode,
        interval_seconds: intervalSeconds,
        created_from_doc: options.documentId,
      };

      debugLog('[Scheduling Controls] API Request:', request);

      const job = await createScheduledJob(request);

      options.onJobCreated?.(job);
      // Re-render with created job
      container.innerHTML = "";
      renderExistingJobControls(container, {
        ...options,
        existingJob: job,
      });
    } catch (error) {
      options.onError?.(error as Error, {
        action: 'create',
        atsCode: resolveAtsCode(options.atsCode),
        intervalSeconds: parseInt(value, 10),
        documentId: options.documentId,
      });
    }
  });

  cancelBtn.addEventListener("click", () => {
    container.innerHTML = "";
    renderAddScheduleButton(container, options);
  });
}
