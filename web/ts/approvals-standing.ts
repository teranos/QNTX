/**
 * The watcher every node is born holding, which tells the pages that an
 * approval moved (ADR-052).
 *
 * The same string as ats/watcher's StandingApproval. standing-watcher-id.test.ts
 * reads the Go source and fails if the two ever say different things. On its
 * own so the socket can route on it without pulling the element in.
 */
export const STANDING_APPROVAL = 'standing-approval';
