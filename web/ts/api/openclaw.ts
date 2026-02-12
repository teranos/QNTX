/**
 * OpenClaw API Client
 *
 * Fetches workspace snapshots from the qntx-claw plugin via the Go backend
 * proxy at /api/claw/*. Provides polling for live updates.
 */

import { apiFetch } from '../api';
import { log, SEG } from '../logger';

/** A tracked bootstrap file from the OpenClaw workspace. */
export interface BootstrapFile {
    name: string;
    exists: boolean;
    content: string;
    content_sha: string;
    mod_time: number;
}

/** A daily memory log entry. */
export interface DailyMemory {
    date: string;
    content: string;
    content_sha: string;
    mod_time: number;
}

/** Full workspace snapshot from the claw plugin. */
export interface OpenClawSnapshot {
    workspace_path: string;
    bootstrap_files: Record<string, BootstrapFile>;
    daily_memories: DailyMemory[];
    taken_at: number;
}

/** A change event from the workspace watcher. */
export interface ChangeEvent {
    file: string;
    category: string;
    operation: string;
}

/**
 * Fetch the full workspace snapshot.
 * Returns null if the plugin is unavailable (not installed, loading, etc).
 */
export async function fetchSnapshot(): Promise<OpenClawSnapshot | null> {
    try {
        const response = await apiFetch('/api/claw/snapshot');
        if (!response.ok) {
            if (response.status === 503) {
                // Plugin still loading
                return null;
            }
            log.warn(SEG.UI, `[OpenClaw] Snapshot request failed with status ${response.status}`);
            return null;
        }
        return await response.json();
    } catch (error) {
        log.debug(SEG.UI, '[OpenClaw] Snapshot fetch failed (plugin may not be running)', error);
        return null;
    }
}

/**
 * Fetch recent change events.
 */
export async function fetchChanges(): Promise<ChangeEvent[]> {
    try {
        const response = await apiFetch('/api/claw/changes');
        if (!response.ok) return [];
        return await response.json();
    } catch {
        return [];
    }
}

/**
 * Poll for snapshot updates. Calls the callback whenever a new snapshot
 * arrives with a different taken_at timestamp.
 *
 * Returns a cleanup function to stop polling.
 */
export function pollSnapshot(
    intervalMs: number,
    onUpdate: (snapshot: OpenClawSnapshot) => void,
): () => void {
    let lastTakenAt = 0;
    let stopped = false;
    let timeoutId: number | undefined;

    async function tick() {
        if (stopped) return;

        const snapshot = await fetchSnapshot();
        if (snapshot && snapshot.taken_at !== lastTakenAt) {
            lastTakenAt = snapshot.taken_at;
            onUpdate(snapshot);
        }

        if (!stopped) {
            timeoutId = window.setTimeout(tick, intervalMs);
        }
    }

    // Start first tick
    void tick();

    return () => {
        stopped = true;
        if (timeoutId !== undefined) {
            clearTimeout(timeoutId);
        }
    };
}
