/**
 * Browser Embedding Store
 *
 * Separate IndexedDB database (qntx-embeddings) for storing embedding vectors.
 * Kept separate from the Rust-managed qntx DB to avoid version conflicts —
 * embeddings flow TypeScript → IndexedDB → WASM cosine similarity, never through Rust storage.
 */

import { log, SEG } from './logger';

const DB_NAME = 'qntx-embeddings';
const DB_VERSION = 1;
const STORE_NAME = 'embeddings';

interface EmbeddingRecord {
    source_id: string;
    vector: ArrayBuffer;  // Float32Array stored as ArrayBuffer in IndexedDB
    model: string;
}

export class EmbeddingStore {
    private db: IDBDatabase | null = null;

    async open(): Promise<void> {
        if (this.db) return;

        return new Promise((resolve, reject) => {
            const request = indexedDB.open(DB_NAME, DB_VERSION);

            request.onupgradeneeded = () => {
                const db = request.result;
                if (!db.objectStoreNames.contains(STORE_NAME)) {
                    db.createObjectStore(STORE_NAME, { keyPath: 'source_id' });
                }
            };

            request.onsuccess = () => {
                this.db = request.result;
                log.debug(SEG.WASM, `[EmbeddingStore] Opened ${DB_NAME} v${DB_VERSION}`);
                resolve();
            };

            request.onerror = () => {
                reject(new Error(`Failed to open ${DB_NAME}: ${request.error?.message}`));
            };
        });
    }

    async put(sourceId: string, vector: Float32Array, model: string): Promise<void> {
        const db = this.requireDb();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readwrite');
            const store = tx.objectStore(STORE_NAME);
            const record: EmbeddingRecord = {
                source_id: sourceId,
                vector: vector.buffer.slice(vector.byteOffset, vector.byteOffset + vector.byteLength),
                model,
            };
            const request = store.put(record);
            request.onsuccess = () => resolve();
            request.onerror = () => reject(new Error(`Failed to put embedding ${sourceId}: ${request.error?.message}`));
        });
    }

    async get(sourceId: string): Promise<{ vector: Float32Array; model: string } | null> {
        const db = this.requireDb();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readonly');
            const store = tx.objectStore(STORE_NAME);
            const request = store.get(sourceId);
            request.onsuccess = () => {
                const record = request.result as EmbeddingRecord | undefined;
                if (!record) {
                    resolve(null);
                    return;
                }
                resolve({
                    vector: new Float32Array(record.vector),
                    model: record.model,
                });
            };
            request.onerror = () => reject(new Error(`Failed to get embedding ${sourceId}: ${request.error?.message}`));
        });
    }

    async getMany(sourceIds: string[]): Promise<Map<string, Float32Array>> {
        const db = this.requireDb();
        const result = new Map<string, Float32Array>();
        if (sourceIds.length === 0) return result;

        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readonly');
            const store = tx.objectStore(STORE_NAME);
            let completed = 0;

            for (const id of sourceIds) {
                const request = store.get(id);
                request.onsuccess = () => {
                    const record = request.result as EmbeddingRecord | undefined;
                    if (record) {
                        result.set(id, new Float32Array(record.vector));
                    }
                    completed++;
                    if (completed === sourceIds.length) resolve(result);
                };
                request.onerror = () => {
                    log.warn(SEG.WASM, `Failed to get embedding for ${id}: ${request.error?.message || 'unknown error'}`);
                    completed++;
                    if (completed === sourceIds.length) resolve(result);
                };
            }

            tx.onerror = () => reject(new Error(`Failed to getMany embeddings: ${tx.error?.message}`));
        });
    }

    async listIds(): Promise<string[]> {
        const db = this.requireDb();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readonly');
            const store = tx.objectStore(STORE_NAME);
            const request = store.getAllKeys();
            request.onsuccess = () => resolve(request.result as string[]);
            request.onerror = () => reject(new Error(`Failed to list embedding IDs: ${request.error?.message}`));
        });
    }

    async getAll(): Promise<Map<string, Float32Array>> {
        const db = this.requireDb();
        return new Promise((resolve, reject) => {
            const tx = db.transaction(STORE_NAME, 'readonly');
            const store = tx.objectStore(STORE_NAME);
            const request = store.getAll();
            request.onsuccess = () => {
                const result = new Map<string, Float32Array>();
                for (const record of request.result as EmbeddingRecord[]) {
                    result.set(record.source_id, new Float32Array(record.vector));
                }
                resolve(result);
            };
            request.onerror = () => reject(new Error(`Failed to getAll embeddings: ${request.error?.message}`));
        });
    }

    private requireDb(): IDBDatabase {
        if (!this.db) throw new Error('EmbeddingStore not opened. Call open() first.');
        return this.db;
    }
}

/** Singleton instance */
export const embeddingStore = new EmbeddingStore();
