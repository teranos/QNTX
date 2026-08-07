import type { ScheduledJobState, ExecutionStatus } from './index';

// Each must be assignable — proves the union resolved to the five/three names
// rather than to never or any.
const a: ScheduledJobState = 'active';
const b: ScheduledJobState = 'paused';
const c: ScheduledJobState = 'stopping';
const d: ScheduledJobState = 'inactive';
const e: ScheduledJobState = 'deleted';
const f: ExecutionStatus = 'running';
const g: ExecutionStatus = 'completed';
const h: ExecutionStatus = 'failed';

// @ts-expect-error a value outside the set must not be assignable
const bad: ScheduledJobState = 'activ';

export const probe = [a, b, c, d, e, f, g, h, bad];
