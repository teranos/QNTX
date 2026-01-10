/**
 * Scheduling Controls Component
 *
 * Inline UI controls for ATS code blocks to configure Pulse scheduling
 */

import { log, SEG } from "../logger";
import type { ScheduledJobResponse } from "./types.ts";
import { INTERVAL_PRESETS, formatInterval } from "./types.ts";
import {
  createScheduledJob,
  updateScheduledJob,
  pauseScheduledJob,
  resumeScheduledJob,
  deleteScheduledJob,
} from "./api.ts";
import { Pulse } from "@generated/sym.js";

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
  existingJob?: ScheduledJobResponse;
  onJobCreated?: (job: ScheduledJobResponse) => void;
  onJobUpdated?: (job: ScheduledJobResponse) => void;
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

  // Build controls using DOM API for security
  container.innerHTML = '';

  const row = document.createElement('div');
  row.className = 'pulse-controls-row';

  // Schedule badge
  const badge = document.createElement('div');
  badge.className = `pulse-schedule-badge ${job.state}`;

  const icon = document.createElement('span');
  icon.className = 'pulse-icon';
  icon.textContent = Pulse;

  const interval = document.createElement('span');
  interval.className = 'pulse-interval';
  interval.textContent = formatInterval(job.interval_seconds ?? 0);

  const state = document.createElement('span');
  state.className = 'pulse-state';
  state.textContent = job.state;

  badge.appendChild(icon);
  badge.appendChild(interval);
  badge.appendChild(state);

  // Actions
  const actions = document.createElement('div');
  actions.className = 'pulse-schedule-actions';

  const pauseBtn = document.createElement('button');
  pauseBtn.className = 'pulse-btn-pause';
  pauseBtn.title = isActive ? "Pause job" : "Resume job";
  pauseBtn.textContent = isActive ? "Pause" : "Resume";

  const intervalSelect = document.createElement('select');
  intervalSelect.className = 'pulse-interval-select';

  INTERVAL_PRESETS.forEach(preset => {
    const option = document.createElement('option');
    option.value = String(preset.seconds);
    option.textContent = preset.label;
    if (preset.seconds === job.interval_seconds) {
      option.selected = true;
    }
    intervalSelect.appendChild(option);
  });

  const customOption = document.createElement('option');
  customOption.value = 'custom';
  customOption.textContent = 'Custom...';
  intervalSelect.appendChild(customOption);

  const deleteBtn = document.createElement('button');
  deleteBtn.className = 'pulse-btn-delete';
  deleteBtn.title = 'Remove schedule';
  deleteBtn.textContent = 'Delete';

  actions.appendChild(pauseBtn);
  actions.appendChild(intervalSelect);
  actions.appendChild(deleteBtn);

  row.appendChild(badge);
  row.appendChild(actions);
  container.appendChild(row);

  // Attach event listeners (using already-created elements)

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
        intervalSelect.value = (job.interval_seconds ?? 0).toString();
        return;
      }
      // TODO(#30): Parse custom interval
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

  // Build button using DOM API for security
  container.innerHTML = '';

  const addBtn = document.createElement('button');
  addBtn.className = 'pulse-btn-add-schedule';
  if (isEmpty) {
    addBtn.disabled = true;
    addBtn.title = 'Add ATS code to enable scheduling';
  }

  const icon = document.createElement('span');
  icon.className = 'pulse-icon';
  icon.textContent = Pulse;

  addBtn.appendChild(icon);
  addBtn.appendChild(document.createTextNode(' Add Schedule'));

  container.appendChild(addBtn);

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

// State for two-click confirmation
interface ConfirmationState {
  needsConfirmation: boolean;
  intervalSeconds: number;
  timeout: number | null;
}

const confirmationStates = new WeakMap<HTMLElement, ConfirmationState>();

/**
 * Render interval selection UI (expanded from "Add Schedule" button)
 */
function renderIntervalSelection(
  container: HTMLElement,
  options: SchedulingControlsOptions
): void {
  // Build interval picker using DOM API for security
  container.innerHTML = '';

  // Initialize confirmation state for this container
  confirmationStates.set(container, {
    needsConfirmation: false,
    intervalSeconds: 0,
    timeout: null
  });

  const picker = document.createElement('div');
  picker.className = 'pulse-interval-picker';

  const label = document.createElement('span');
  label.className = 'pulse-label';
  label.textContent = 'Run every:';

  const intervalSelect = document.createElement('select');
  intervalSelect.className = 'pulse-interval-select';

  INTERVAL_PRESETS.forEach(preset => {
    const option = document.createElement('option');
    option.value = String(preset.seconds);
    option.textContent = preset.label;
    intervalSelect.appendChild(option);
  });

  const customOption = document.createElement('option');
  customOption.value = 'custom';
  customOption.textContent = 'Custom...';
  intervalSelect.appendChild(customOption);

  const confirmBtn = document.createElement('button');
  confirmBtn.className = 'pulse-btn-confirm';
  confirmBtn.textContent = '✓';

  const cancelBtn = document.createElement('button');
  cancelBtn.className = 'pulse-btn-cancel';
  cancelBtn.textContent = '✗';

  picker.appendChild(label);
  picker.appendChild(intervalSelect);
  picker.appendChild(confirmBtn);
  picker.appendChild(cancelBtn);

  container.appendChild(picker);

  // Reset confirmation when interval changes
  intervalSelect.addEventListener("change", () => {
    const state = confirmationStates.get(container);
    if (state && state.needsConfirmation) {
      state.needsConfirmation = false;
      confirmBtn.textContent = '✓';
      confirmBtn.classList.remove('pulse-btn-confirm-active');
      const hint = container.querySelector('.pulse-confirm-hint');
      hint?.remove();
    }
  });

  confirmBtn.addEventListener("click", async () => {
    const value = intervalSelect.value;
    if (value === "custom") {
      // TODO(#30): Show custom interval input
      return;
    }

    const state = confirmationStates.get(container);
    if (!state) return;

    const intervalSeconds = parseInt(value, 10);

    // First click: show confirmation state
    if (!state.needsConfirmation || state.intervalSeconds !== intervalSeconds) {
      state.needsConfirmation = true;
      state.intervalSeconds = intervalSeconds;

      // Update button to confirmation state
      confirmBtn.textContent = 'Confirm';
      confirmBtn.classList.add('pulse-btn-confirm-active');

      // Add hint
      const existingHint = container.querySelector('.pulse-confirm-hint');
      if (!existingHint) {
        const hint = document.createElement('div');
        hint.className = 'pulse-confirm-hint panel-confirm-hint';
        hint.textContent = 'Click again to schedule';
        picker.appendChild(hint);
      }

      // Auto-reset after 5 seconds
      if (state.timeout) {
        clearTimeout(state.timeout);
      }
      state.timeout = window.setTimeout(() => {
        state.needsConfirmation = false;
        confirmBtn.textContent = '✓';
        confirmBtn.classList.remove('pulse-btn-confirm-active');
        const hint = container.querySelector('.pulse-confirm-hint');
        hint?.remove();
      }, 5000);

      return;
    }

    // Second click: actually create schedule
    state.needsConfirmation = false;
    if (state.timeout) {
      clearTimeout(state.timeout);
      state.timeout = null;
    }

    try {
      const atsCode = resolveAtsCode(options.atsCode);

      // Validate ATS code before sending
      if (!atsCode || atsCode.trim() === '') {
        throw new Error('ATS code is empty - cannot schedule empty query. Try refreshing the page.');
      }

      log.debug(SEG.PULSE, 'Creating job with:', {
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

      log.debug(SEG.PULSE, 'API Request:', request);

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
        intervalSeconds: intervalSeconds,
        documentId: options.documentId,
      });
    }
  });

  cancelBtn.addEventListener("click", () => {
    const state = confirmationStates.get(container);
    if (state?.timeout) {
      clearTimeout(state.timeout);
    }
    container.innerHTML = "";
    renderAddScheduleButton(container, options);
  });
}
