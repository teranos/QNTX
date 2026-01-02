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
 * Format interval seconds to human-readable string
 */
export function formatInterval(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

/**
 * Parse interval string to seconds (for custom intervals)
 */
export function parseInterval(input: string): number | null {
  const match = input.match(/^(\d+)\s*(s|m|h|d)$/);
  if (!match) return null;

  const [, value, unit] = match;
  const num = parseInt(value, 10);

  switch (unit) {
    case "s":
      return num;
    case "m":
      return num * 60;
    case "h":
      return num * 60 * 60;
    case "d":
      return num * 24 * 60 * 60;
    default:
      return null;
  }
}
