/**
 * Script Storage - Abstraction for storing Python/code scripts
 *
 * Supports multiple storage backends:
 * - LocalStorage (browser-based, current implementation)
 * - Backend API (disk-based, future)
 * - Attestations (ATS-based, future)
 *
 * This abstraction enables swapping storage strategies without changing
 * py-glyph.ts or other consumers.
 */

import { log, SEG } from '../logger';

/**
 * Metadata for a stored script
 */
export interface ScriptMetadata {
    id: string;
    name?: string;
    language: 'python';  // Future: 'go' | 'rust' | 'typescript'
    createdAt: number;
    modifiedAt: number;
}

/**
 * Script storage interface
 */
export interface ScriptStorage {
    /**
     * Save script content
     */
    save(id: string, code: string, metadata?: Partial<ScriptMetadata>): Promise<void>;

    /**
     * Load script content by ID
     */
    load(id: string): Promise<string | null>;

    /**
     * Delete script by ID
     */
    delete(id: string): Promise<void>;

    /**
     * List all scripts
     */
    list(): Promise<ScriptMetadata[]>;
}

/**
 * LocalStorage implementation of ScriptStorage
 *
 * Stores scripts in browser localStorage with metadata.
 * Storage key pattern: qntx-script:<id>
 */
class LocalStorageScriptStorage implements ScriptStorage {
    private readonly keyPrefix = 'qntx-script:';
    private readonly metadataKey = 'qntx-script-metadata';

    async save(id: string, code: string, metadata?: Partial<ScriptMetadata>): Promise<void> {
        try {
            const key = this.getKey(id);
            const now = Date.now();

            // Load or create metadata
            const allMetadata = await this.loadAllMetadata();
            const existing = allMetadata.find(m => m.id === id);

            const scriptMetadata: ScriptMetadata = {
                id,
                language: 'python',
                createdAt: existing?.createdAt ?? now,
                modifiedAt: now,
                ...metadata,
            };

            // Save code
            localStorage.setItem(key, code);

            // Update metadata index
            const updatedMetadata = allMetadata.filter(m => m.id !== id);
            updatedMetadata.push(scriptMetadata);
            localStorage.setItem(this.metadataKey, JSON.stringify(updatedMetadata));

            log.debug(SEG.UI, `[ScriptStorage] Saved script ${id} (${code.length} chars)`);
        } catch (error) {
            log.error(SEG.UI, `[ScriptStorage] Failed to save script ${id}:`, error);
            throw error;
        }
    }

    async load(id: string): Promise<string | null> {
        try {
            const key = this.getKey(id);
            const code = localStorage.getItem(key);

            if (code) {
                log.debug(SEG.UI, `[ScriptStorage] Loaded script ${id} (${code.length} chars)`);
            } else {
                log.debug(SEG.UI, `[ScriptStorage] No script found for ${id}`);
            }

            return code;
        } catch (error) {
            log.error(SEG.UI, `[ScriptStorage] Failed to load script ${id}:`, error);
            return null;
        }
    }

    async delete(id: string): Promise<void> {
        try {
            const key = this.getKey(id);
            localStorage.removeItem(key);

            // Remove from metadata index
            const allMetadata = await this.loadAllMetadata();
            const updatedMetadata = allMetadata.filter(m => m.id !== id);
            localStorage.setItem(this.metadataKey, JSON.stringify(updatedMetadata));

            log.debug(SEG.UI, `[ScriptStorage] Deleted script ${id}`);
        } catch (error) {
            log.error(SEG.UI, `[ScriptStorage] Failed to delete script ${id}:`, error);
            throw error;
        }
    }

    async list(): Promise<ScriptMetadata[]> {
        try {
            return await this.loadAllMetadata();
        } catch (error) {
            log.error(SEG.UI, `[ScriptStorage] Failed to list scripts:`, error);
            return [];
        }
    }

    private getKey(id: string): string {
        return `${this.keyPrefix}${id}`;
    }

    private async loadAllMetadata(): Promise<ScriptMetadata[]> {
        const stored = localStorage.getItem(this.metadataKey);
        if (!stored) return [];

        try {
            return JSON.parse(stored);
        } catch (error) {
            log.error(SEG.UI, `[ScriptStorage] Failed to parse metadata:`, error);
            return [];
        }
    }
}

/**
 * Get the active script storage implementation
 *
 * Currently always returns LocalStorage.
 * Future: Check feature flags, user preferences, or environment to select backend.
 */
export function getScriptStorage(): ScriptStorage {
    // TODO: Add feature flag or preference to select storage backend
    // if (featureFlags.useAttestationStorage) return new AttestationScriptStorage();
    // if (featureFlags.useBackendStorage) return new BackendScriptStorage();
    return new LocalStorageScriptStorage();
}
