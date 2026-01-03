/**
 * Pulse Panel Custom Events
 *
 * Type-safe custom events for cross-panel communication.
 * Replaces global window object access with event-based messaging.
 */

import type { Execution } from './execution-types';

// ============================================================================
// Event Names
// ============================================================================

export const PULSE_EVENTS = {
    /** Fired when a pulse execution starts */
    EXECUTION_STARTED: 'pulse:execution:started',
    /** Fired when a pulse execution completes successfully */
    EXECUTION_COMPLETED: 'pulse:execution:completed',
    /** Fired when a pulse execution fails */
    EXECUTION_FAILED: 'pulse:execution:failed',
    /** Fired when execution logs are received */
    EXECUTION_LOG: 'pulse:execution:log',
} as const;

// ============================================================================
// Event Detail Types
// ============================================================================

export interface ExecutionStartedDetail {
    scheduledJobId: string;
    executionId: string;
    atsCode: string;
    timestamp: number;
}

export interface ExecutionCompletedDetail {
    scheduledJobId: string;
    executionId: string;
    asyncJobId?: string;
    resultSummary?: string;
    durationMs: number;
    timestamp: number;
}

export interface ExecutionFailedDetail {
    scheduledJobId: string;
    executionId: string;
    errorMessage: string;
    durationMs: number;
    atsCode: string;
    timestamp: number;
}

export interface ExecutionLogDetail {
    scheduledJobId: string;
    executionId: string;
    logChunk: string;
}

// ============================================================================
// Typed Custom Events
// ============================================================================

export type PulseExecutionStartedEvent = CustomEvent<ExecutionStartedDetail>;
export type PulseExecutionCompletedEvent = CustomEvent<ExecutionCompletedDetail>;
export type PulseExecutionFailedEvent = CustomEvent<ExecutionFailedDetail>;
export type PulseExecutionLogEvent = CustomEvent<ExecutionLogDetail>;

// ============================================================================
// Event Dispatch Helpers
// ============================================================================

/**
 * Dispatch a pulse execution started event
 */
export function dispatchExecutionStarted(detail: ExecutionStartedDetail): void {
    document.dispatchEvent(new CustomEvent(PULSE_EVENTS.EXECUTION_STARTED, { detail }));
}

/**
 * Dispatch a pulse execution completed event
 */
export function dispatchExecutionCompleted(detail: ExecutionCompletedDetail): void {
    document.dispatchEvent(new CustomEvent(PULSE_EVENTS.EXECUTION_COMPLETED, { detail }));
}

/**
 * Dispatch a pulse execution failed event
 */
export function dispatchExecutionFailed(detail: ExecutionFailedDetail): void {
    document.dispatchEvent(new CustomEvent(PULSE_EVENTS.EXECUTION_FAILED, { detail }));
}

/**
 * Dispatch a pulse execution log event
 */
export function dispatchExecutionLog(detail: ExecutionLogDetail): void {
    document.dispatchEvent(new CustomEvent(PULSE_EVENTS.EXECUTION_LOG, { detail }));
}

// ============================================================================
// Event Subscription Helpers
// ============================================================================

/**
 * Subscribe to pulse execution started events
 * @returns Unsubscribe function
 */
export function onExecutionStarted(
    callback: (detail: ExecutionStartedDetail) => void
): () => void {
    const handler = (e: Event) => callback((e as PulseExecutionStartedEvent).detail);
    document.addEventListener(PULSE_EVENTS.EXECUTION_STARTED, handler);
    return () => document.removeEventListener(PULSE_EVENTS.EXECUTION_STARTED, handler);
}

/**
 * Subscribe to pulse execution completed events
 * @returns Unsubscribe function
 */
export function onExecutionCompleted(
    callback: (detail: ExecutionCompletedDetail) => void
): () => void {
    const handler = (e: Event) => callback((e as PulseExecutionCompletedEvent).detail);
    document.addEventListener(PULSE_EVENTS.EXECUTION_COMPLETED, handler);
    return () => document.removeEventListener(PULSE_EVENTS.EXECUTION_COMPLETED, handler);
}

/**
 * Subscribe to pulse execution failed events
 * @returns Unsubscribe function
 */
export function onExecutionFailed(
    callback: (detail: ExecutionFailedDetail) => void
): () => void {
    const handler = (e: Event) => callback((e as PulseExecutionFailedEvent).detail);
    document.addEventListener(PULSE_EVENTS.EXECUTION_FAILED, handler);
    return () => document.removeEventListener(PULSE_EVENTS.EXECUTION_FAILED, handler);
}

/**
 * Subscribe to pulse execution log events
 * @returns Unsubscribe function
 */
export function onExecutionLog(
    callback: (detail: ExecutionLogDetail) => void
): () => void {
    const handler = (e: Event) => callback((e as PulseExecutionLogEvent).detail);
    document.addEventListener(PULSE_EVENTS.EXECUTION_LOG, handler);
    return () => document.removeEventListener(PULSE_EVENTS.EXECUTION_LOG, handler);
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Convert Unix timestamp (seconds) to ISO string
 * Used throughout pulse event handlers for consistent timestamp formatting
 */
export function unixToISO(timestamp: number): string {
    return new Date(timestamp * 1000).toISOString();
}
