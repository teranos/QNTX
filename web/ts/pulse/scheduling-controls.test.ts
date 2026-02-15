/**
 * Tests for Pulse Scheduling Controls UI
 *
 * Tests UI state and interactions for scheduling controls
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test';
import { createSchedulingControls } from './scheduling-controls';
import type { ScheduledJobResponse } from './types';

// Mock API functions
const mockCreateScheduledJob = mock(() => Promise.resolve());
const mockUpdateScheduledJob = mock(() => Promise.resolve());
const mockPauseScheduledJob = mock(() => Promise.resolve());
const mockResumeScheduledJob = mock(() => Promise.resolve());
const mockDeleteScheduledJob = mock(() => Promise.resolve());

mock.module('./api', () => ({
  createScheduledJob: mockCreateScheduledJob,
  updateScheduledJob: mockUpdateScheduledJob,
  pauseScheduledJob: mockPauseScheduledJob,
  resumeScheduledJob: mockResumeScheduledJob,
  deleteScheduledJob: mockDeleteScheduledJob,
}));

describe('Scheduling Controls UI', () => {
  beforeEach(() => {
    mockCreateScheduledJob.mockClear();
    mockUpdateScheduledJob.mockClear();
    mockPauseScheduledJob.mockClear();
    mockResumeScheduledJob.mockClear();
    mockDeleteScheduledJob.mockClear();
  });

  describe('Empty ATS Code Prevention', () => {
    it('should disable "Add Schedule" button when ATS code is empty', () => {
      const controls = createSchedulingControls({
        atsCode: '',
      });

      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button).toBeTruthy();
      expect(button.disabled).toBe(true);
      expect(button.getAttribute('title')).toContain('Add ATS code');
    });

    it('should disable button when ATS code is only whitespace', () => {
      const controls = createSchedulingControls({
        atsCode: '   \n\t  ',
      });

      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(true);
    });

    it('should enable button when ATS code is provided', () => {
      const controls = createSchedulingControls({
        atsCode: 'ix https://example.com/api/data',
      });

      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(false);
      // When enabled, button has no title attribute (not disabled tooltip)
      expect(button.getAttribute('title')).toBeFalsy();
    });

    it('should enable button when ATS code is provided via function', () => {
      const controls = createSchedulingControls({
        atsCode: () => 'ix https://example.com/api/data',
      });

      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(false);
    });

    it('should disable button when function returns empty string', () => {
      const controls = createSchedulingControls({
        atsCode: () => '',
      });

      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(true);
    });
  });

  describe('Pause/Resume UI State', () => {
    it('should show pause button for active jobs', () => {
      const job: ScheduledJobResponse = {
        id: 'SPJ_123',
        ats_code: 'ix https://example.com',
        interval_seconds: 3600,
        next_run_at: '2025-12-07T10:00:00Z',
        last_run_at: null,
        last_execution_id: '',
        state: 'active',
        created_from_doc: '',
        metadata: '',
        created_at: '2025-12-07T09:00:00Z',
        updated_at: '2025-12-07T09:00:00Z',
      };

      const controls = createSchedulingControls({
        atsCode: job.ats_code,
        existingJob: job,
      });

      const pauseBtn = controls.querySelector('.pulse-btn-pause') as HTMLButtonElement;
      expect(pauseBtn).toBeTruthy();
      expect(pauseBtn.textContent?.trim()).toBe('Pause');
      expect(pauseBtn.getAttribute('title')).toBe('Pause job');
    });

    it('should show resume button for paused jobs', () => {
      const job: ScheduledJobResponse = {
        id: 'SPJ_123',
        ats_code: 'ix https://example.com',
        interval_seconds: 3600,
        next_run_at: '2025-12-07T10:00:00Z',
        last_run_at: null,
        last_execution_id: '',
        state: 'paused',
        created_from_doc: '',
        metadata: '',
        created_at: '2025-12-07T09:00:00Z',
        updated_at: '2025-12-07T09:00:00Z',
      };

      const controls = createSchedulingControls({
        atsCode: job.ats_code,
        existingJob: job,
      });

      const resumeBtn = controls.querySelector('.pulse-btn-pause') as HTMLButtonElement;
      expect(resumeBtn).toBeTruthy();
      expect(resumeBtn.textContent?.trim()).toBe('Resume');
      expect(resumeBtn.getAttribute('title')).toBe('Resume job');
    });

    it('should show interval dropdown for existing jobs', () => {
      const job: ScheduledJobResponse = {
        id: 'SPJ_123',
        ats_code: 'ix https://example.com',
        interval_seconds: 21600, // 6 hours
        next_run_at: '2025-12-07T15:00:00Z',
        last_run_at: null,
        last_execution_id: '',
        state: 'active',
        created_from_doc: '',
        metadata: '',
        created_at: '2025-12-07T09:00:00Z',
        updated_at: '2025-12-07T09:00:00Z',
      };

      const controls = createSchedulingControls({
        atsCode: job.ats_code,
        existingJob: job,
      });

      const intervalSelect = controls.querySelector('.pulse-interval-select') as HTMLSelectElement;
      expect(intervalSelect).toBeTruthy();
      expect(intervalSelect.value).toBe('21600'); // Should show current interval
    });

    it('should show delete button for existing jobs', () => {
      const job: ScheduledJobResponse = {
        id: 'SPJ_123',
        ats_code: 'ix https://example.com',
        interval_seconds: 3600,
        next_run_at: '2025-12-07T10:00:00Z',
        last_run_at: null,
        last_execution_id: '',
        state: 'active',
        created_from_doc: '',
        metadata: '',
        created_at: '2025-12-07T09:00:00Z',
        updated_at: '2025-12-07T09:00:00Z',
      };

      const controls = createSchedulingControls({
        atsCode: job.ats_code,
        existingJob: job,
      });

      const deleteBtn = controls.querySelector('.pulse-btn-delete') as HTMLButtonElement;
      expect(deleteBtn).toBeTruthy();
      expect(deleteBtn.getAttribute('title')).toBe('Remove schedule');
    });
  });

  describe('ATS Code Callback Pattern', () => {
    it('should call function to get fresh ATS code when needed', () => {
      let currentCode = 'ix https://example.com/api/data';
      const getCode = mock(() => currentCode);

      const controls = createSchedulingControls({
        atsCode: getCode,
      });

      // Initial render should call the function
      expect(getCode).toHaveBeenCalled();

      // Button should be enabled with valid code
      const button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(false);
    });

    it('should handle dynamic code changes via function', () => {
      let currentCode = '';
      const getCode = () => currentCode;

      // Start with empty code
      let controls = createSchedulingControls({
        atsCode: getCode,
      });

      let button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(true);

      // Update code and re-render
      currentCode = 'ix https://example.com/jobs/';
      controls = createSchedulingControls({
        atsCode: getCode,
      });

      button = controls.querySelector('.pulse-btn-add-schedule') as HTMLButtonElement;
      expect(button.disabled).toBe(false);
    });
  });
});
