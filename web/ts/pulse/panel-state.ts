/**
 * Pulse Panel State Management
 *
 * Manages all stateful data for the pulse panel including:
 * - Expanded/collapsed job IDs
 * - Loaded executions per job
 * - Loading states
 * - Error states
 * - Execution display limits
 * - localStorage persistence
 */

import type { Execution } from './execution-types';
import { log, SEG } from '../logger';

export class PulsePanelState {
    // Which jobs have their execution history expanded
    public expandedJobs: Set<string> = new Set();

    // Executions loaded for each job
    public jobExecutions: Map<string, Execution[]> = new Map();

    // Jobs currently loading executions
    public loadingExecutions: Set<string> = new Set();

    // Error messages for failed execution loads
    public executionErrors: Map<string, string> = new Map();

    // How many executions to show per job (default 5, can increase with "Load more")
    public executionLimits: Map<string, number> = new Map();

    private readonly STORAGE_KEY = 'pulse-panel-expanded-jobs';
    private readonly DEFAULT_LIMIT = 5;

    constructor() {
        this.loadFromLocalStorage();
    }

    /**
     * Load expanded job IDs from localStorage
     */
    public loadFromLocalStorage(): void {
        try {
            const stored = localStorage.getItem(this.STORAGE_KEY);
            if (stored) {
                const jobIds = JSON.parse(stored) as string[];
                this.expandedJobs = new Set(jobIds);
            }
        } catch (e) {
            log.error(SEG.PULSE, 'Failed to load expanded state:', e);
        }
    }

    /**
     * Save expanded job IDs to localStorage
     */
    public saveToLocalStorage(): void {
        try {
            const jobIds = Array.from(this.expandedJobs);
            localStorage.setItem(this.STORAGE_KEY, JSON.stringify(jobIds));
        } catch (e) {
            log.error(SEG.PULSE, 'Failed to save expanded state:', e);
        }
    }

    /**
     * Remove orphaned job IDs from expandedJobs that no longer exist
     * Prevents unbounded localStorage growth from deleted jobs
     */
    public cleanupOrphanedJobs(validJobIds: Set<string>): void {
        const orphanedIds: string[] = [];

        for (const jobId of this.expandedJobs) {
            if (!validJobIds.has(jobId)) {
                orphanedIds.push(jobId);
            }
        }

        if (orphanedIds.length > 0) {
            orphanedIds.forEach(id => this.expandedJobs.delete(id));
            this.saveToLocalStorage();
            log.debug(SEG.PULSE, `Cleaned up ${orphanedIds.length} orphaned job IDs from localStorage`);
        }
    }

    /**
     * Toggle expansion state for a job
     * Returns the new state (true = expanded, false = collapsed)
     */
    public toggleExpanded(jobId: string): boolean {
        if (this.expandedJobs.has(jobId)) {
            this.expandedJobs.delete(jobId);
            this.saveToLocalStorage();
            return false;
        } else {
            this.expandedJobs.add(jobId);
            this.saveToLocalStorage();
            return true;
        }
    }

    /**
     * Check if a job is expanded
     */
    public isExpanded(jobId: string): boolean {
        return this.expandedJobs.has(jobId);
    }

    /**
     * Set executions for a job
     */
    public setExecutions(jobId: string, executions: Execution[]): void {
        this.jobExecutions.set(jobId, executions);
    }

    /**
     * Get executions for a job
     */
    public getExecutions(jobId: string): Execution[] | undefined {
        return this.jobExecutions.get(jobId);
    }

    /**
     * Add a single execution to a job's execution list
     * Returns true if added successfully
     */
    public addExecution(jobId: string, execution: Execution): boolean {
        const existing = this.jobExecutions.get(jobId);
        if (!existing) {
            this.jobExecutions.set(jobId, [execution]);
            return true;
        }

        // Check if execution already exists
        const existingIndex = existing.findIndex(e => e.id === execution.id);
        if (existingIndex >= 0) {
            // Update existing execution
            existing[existingIndex] = execution;
            return true;
        }

        // Add new execution at the beginning (newest first)
        existing.unshift(execution);
        return true;
    }

    /**
     * Update an execution's status/data
     * Returns true if found and updated
     */
    public updateExecution(executionId: string, updates: Partial<Execution>): boolean {
        for (const executions of this.jobExecutions.values()) {
            const index = executions.findIndex(e => e.id === executionId);
            if (index >= 0) {
                executions[index] = { ...executions[index], ...updates };
                return true;
            }
        }
        return false;
    }

    /**
     * Set loading state for a job
     */
    public setLoading(jobId: string, loading: boolean): void {
        if (loading) {
            this.loadingExecutions.add(jobId);
        } else {
            this.loadingExecutions.delete(jobId);
        }
    }

    /**
     * Check if a job is loading executions
     */
    public isLoading(jobId: string): boolean {
        return this.loadingExecutions.has(jobId);
    }

    /**
     * Set error state for a job
     */
    public setError(jobId: string, error: string | null): void {
        if (error) {
            this.executionErrors.set(jobId, error);
        } else {
            this.executionErrors.delete(jobId);
        }
    }

    /**
     * Get error message for a job
     */
    public getError(jobId: string): string | undefined {
        return this.executionErrors.get(jobId);
    }

    /**
     * Get execution limit for a job (default 5)
     */
    public getLimit(jobId: string): number {
        return this.executionLimits.get(jobId) ?? this.DEFAULT_LIMIT;
    }

    /**
     * Increase execution limit for a job
     */
    public increaseLimit(jobId: string, increment: number): void {
        const currentLimit = this.getLimit(jobId);
        this.executionLimits.set(jobId, currentLimit + increment);
    }

    /**
     * Reset limit for a job to default
     */
    public resetLimit(jobId: string): void {
        this.executionLimits.delete(jobId);
    }

    /**
     * Clear all state for a job (when job is deleted)
     */
    public clearJob(jobId: string): void {
        this.expandedJobs.delete(jobId);
        this.jobExecutions.delete(jobId);
        this.loadingExecutions.delete(jobId);
        this.executionErrors.delete(jobId);
        this.executionLimits.delete(jobId);
        this.saveToLocalStorage();
    }

    /**
     * Clear all state
     */
    public clearAll(): void {
        this.expandedJobs.clear();
        this.jobExecutions.clear();
        this.loadingExecutions.clear();
        this.executionErrors.clear();
        this.executionLimits.clear();
        this.saveToLocalStorage();
    }
}
