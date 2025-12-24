// Session state management using localStorage

import type { SessionData } from '../types/core';

const STORAGE_KEY: string = 'qntx-graph-session';
const MAX_AGE_MS: number = 7 * 24 * 60 * 60 * 1000; // 7 days

/**
 * Save current session state to localStorage
 * @param state - Partial session data to save
 */
export function saveSession(state: Partial<SessionData>): void {
    try {
        const session: SessionData = {
            ...state,
            timestamp: Date.now()
        };
        localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
    } catch (e) {
        console.error('Failed to save session:', e);
    }
}

/**
 * Restore session state from localStorage
 * @returns Session data if valid and not expired, null otherwise
 */
export function restoreSession(): SessionData | null {
    try {
        const saved = localStorage.getItem(STORAGE_KEY);
        if (!saved) return null;

        const session = JSON.parse(saved) as SessionData;

        // Validate session structure
        if (!isValidSession(session)) {
            console.warn('Invalid session structure, clearing');
            clearSession();
            return null;
        }

        // Check if session is too old
        if (Date.now() - session.timestamp > MAX_AGE_MS) {
            clearSession();
            return null;
        }

        return session;
    } catch (e) {
        console.error('Failed to restore session:', e);
        return null;
    }
}

/**
 * Clear session state from localStorage
 */
export function clearSession(): void {
    try {
        localStorage.removeItem(STORAGE_KEY);
    } catch (e) {
        console.error('Failed to clear session:', e);
    }
}

/**
 * Type guard to validate session data structure
 * @param data - Data to validate
 * @returns True if data is a valid SessionData object
 */
function isValidSession(data: unknown): data is SessionData {
    if (!data || typeof data !== 'object') return false;

    const session = data as Record<string, unknown>;

    // Required field: timestamp must be a number
    if (typeof session.timestamp !== 'number') return false;

    // Optional fields: validate types if present
    if ('query' in session && typeof session.query !== 'string') return false;
    if ('verbosity' in session && typeof session.verbosity !== 'number') return false;

    return true;
}