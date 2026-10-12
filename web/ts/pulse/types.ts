/**
 * Pulse Scheduled Jobs - UI Helpers
 *
 * Type definitions are imported from generated types.
 * This file only contains UI-specific utilities.
 */

// Re-export types from central location for convenience
export type {
  ScheduledJobResponse,
  ScheduledJobState,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
} from '../../types';

import { short, spanOfMs } from '../span';

/**
 * Common interval presets for UI
 * First item is the default selection
 */
export const INTERVAL_PRESETS = [
  { label: "6 hours", seconds: 6 * 60 * 60 },
  { label: "12 hours", seconds: 12 * 60 * 60 },
  { label: "24 hours", seconds: 24 * 60 * 60 },
  { label: "1 hour", seconds: 60 * 60 },
  { label: "30 minutes", seconds: 30 * 60 },
  { label: "15 minutes", seconds: 15 * 60 },
] as const;

/**
 * An interval, in the span grammar the ax language reads back (span.ts).
 * A length with no start: weeks at most, never months.
 */
export function formatInterval(seconds: number): string {
  return short(spanOfMs(seconds * 1000));
}
