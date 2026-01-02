/**
 * Tests for Pulse job validation and state transitions
 *
 * Ensures job state integrity and valid transitions
 */

import { describe, it, expect } from 'bun:test';
import type { ScheduledJobResponseResponse, ScheduledJobResponseState } from './types';

// Validation utilities
function isValidState(state: string): state is ScheduledJobResponseState {
  return ['active', 'paused', 'stopping', 'inactive'].includes(state);
}

function canTransitionTo(from: ScheduledJobResponseState, to: ScheduledJobResponseState): boolean {
  // Valid state transitions
  const transitions: Record<ScheduledJobResponseState, ScheduledJobResponseState[]> = {
    active: ['paused', 'stopping', 'inactive'],
    paused: ['active', 'inactive'],
    stopping: ['inactive'],
    inactive: [], // Terminal state
  };

  return transitions[from]?.includes(to) ?? false;
}

function isScheduledJobResponse(obj: any): obj is ScheduledJobResponse {
  return (
    typeof obj === 'object' &&
    obj !== null &&
    typeof obj.id === 'string' &&
    typeof obj.ats_code === 'string' &&
    typeof obj.interval_seconds === 'number' &&
    typeof obj.state === 'string' &&
    isValidState(obj.state)
  );
}

describe('Pulse Job Validation', () => {
  describe('isValidState', () => {
    it('should accept valid states', () => {
      expect(isValidState('active')).toBe(true);
      expect(isValidState('paused')).toBe(true);
      expect(isValidState('stopping')).toBe(true);
      expect(isValidState('inactive')).toBe(true);
    });

    it('should reject invalid states', () => {
      expect(isValidState('running')).toBe(false);
      expect(isValidState('pending')).toBe(false);
      expect(isValidState('')).toBe(false);
      expect(isValidState('ACTIVE')).toBe(false); // Case sensitive
    });
  });

  describe('canTransitionTo', () => {
    describe('from active', () => {
      it('should allow pause', () => {
        expect(canTransitionTo('active', 'paused')).toBe(true);
      });

      it('should allow stop', () => {
        expect(canTransitionTo('active', 'stopping')).toBe(true);
      });

      it('should allow inactivate', () => {
        expect(canTransitionTo('active', 'inactive')).toBe(true);
      });

      it('should not allow to active (same state)', () => {
        expect(canTransitionTo('active', 'active')).toBe(false);
      });
    });

    describe('from paused', () => {
      it('should allow resume', () => {
        expect(canTransitionTo('paused', 'active')).toBe(true);
      });

      it('should allow inactivate', () => {
        expect(canTransitionTo('paused', 'inactive')).toBe(true);
      });

      it('should not allow stop directly', () => {
        expect(canTransitionTo('paused', 'stopping')).toBe(false);
      });
    });

    describe('from stopping', () => {
      it('should only allow transition to inactive', () => {
        expect(canTransitionTo('stopping', 'inactive')).toBe(true);
        expect(canTransitionTo('stopping', 'active')).toBe(false);
        expect(canTransitionTo('stopping', 'paused')).toBe(false);
      });
    });

    describe('from inactive', () => {
      it('should not allow any transitions (terminal state)', () => {
        expect(canTransitionTo('inactive', 'active')).toBe(false);
        expect(canTransitionTo('inactive', 'paused')).toBe(false);
        expect(canTransitionTo('inactive', 'stopping')).toBe(false);
      });
    });
  });

  describe('isScheduledJobResponse', () => {
    it('should accept valid job object', () => {
      const job: ScheduledJobResponse = {
        id: 'SPJ_123',
        ats_code: 'ix https://example.com',
        interval_seconds: 3600,
        next_run_at: '2025-12-06T10:00:00Z',
        last_run_at: null,
        last_execution_id: '',
        state: 'active',
        created_from_doc: '',
        metadata: '',
        created_at: '2025-12-06T09:00:00Z',
        updated_at: '2025-12-06T09:00:00Z',
      };

      expect(isScheduledJobResponse(job)).toBe(true);
    });

    it('should reject missing required fields', () => {
      expect(isScheduledJobResponse({})).toBe(false);
      expect(isScheduledJobResponse({ id: 'SPJ_123' })).toBe(false);
      expect(isScheduledJobResponse({ id: 'SPJ_123', ats_code: 'ix url' })).toBe(false);
    });

    it('should reject invalid types', () => {
      expect(
        isScheduledJobResponse({
          id: 123, // Should be string
          ats_code: 'ix url',
          interval_seconds: 3600,
          state: 'active',
        })
      ).toBe(false);

      expect(
        isScheduledJobResponse({
          id: 'SPJ_123',
          ats_code: 'ix url',
          interval_seconds: '3600', // Should be number
          state: 'active',
        })
      ).toBe(false);
    });

    it('should reject invalid state', () => {
      expect(
        isScheduledJobResponse({
          id: 'SPJ_123',
          ats_code: 'ix url',
          interval_seconds: 3600,
          state: 'invalid_state',
        })
      ).toBe(false);
    });

    it('should reject null and undefined', () => {
      expect(isScheduledJobResponse(null)).toBe(false);
      expect(isScheduledJobResponse(undefined)).toBe(false);
    });

    it('should reject primitives', () => {
      expect(isScheduledJobResponse('job')).toBe(false);
      expect(isScheduledJobResponse(123)).toBe(false);
      expect(isScheduledJobResponse(true)).toBe(false);
    });
  });

  describe('job validation rules', () => {
    it('should require positive interval', () => {
      const isValidInterval = (seconds: number) => seconds > 0;

      expect(isValidInterval(1)).toBe(true);
      expect(isValidInterval(3600)).toBe(true);
      expect(isValidInterval(0)).toBe(false);
      expect(isValidInterval(-1)).toBe(false);
    });

    it('should require non-empty ATS code', () => {
      const isValidATSCode = (code: string) => code.trim().length > 0;

      expect(isValidATSCode('ix https://example.com')).toBe(true);
      expect(isValidATSCode('is engineer')).toBe(true);
      expect(isValidATSCode('')).toBe(false);
      expect(isValidATSCode('   ')).toBe(false);
    });

    it('should validate job ID format (basic)', () => {
      const isValidJobID = (id: string) => /^SPJ_\d+$/.test(id);

      expect(isValidJobID('SPJ_1733450123')).toBe(true);
      expect(isValidJobID('SPJ_123')).toBe(true);
      expect(isValidJobID('JOB_123')).toBe(false);
      expect(isValidJobID('SPJ_')).toBe(false);
      expect(isValidJobID('')).toBe(false);
    });
  });

  describe('state transition sequences', () => {
    it('should allow typical pause/resume cycle', () => {
      expect(canTransitionTo('active', 'paused')).toBe(true);
      expect(canTransitionTo('paused', 'active')).toBe(true);
    });

    it('should allow graceful shutdown', () => {
      expect(canTransitionTo('active', 'stopping')).toBe(true);
      expect(canTransitionTo('stopping', 'inactive')).toBe(true);
    });

    it('should allow immediate deletion', () => {
      expect(canTransitionTo('active', 'inactive')).toBe(true);
      expect(canTransitionTo('paused', 'inactive')).toBe(true);
    });

    it('should prevent resurrection of inactive jobs', () => {
      expect(canTransitionTo('inactive', 'active')).toBe(false);
      expect(canTransitionTo('inactive', 'paused')).toBe(false);
    });
  });
});
