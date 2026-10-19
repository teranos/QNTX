/**
 * QNTX WASM module - Browser attestation storage with IndexedDB
 *
 * Provides the AX query filter and minimal storage functions for browser-based
 * attestation management. Automatically initialized on import.
 *
 * Uses proto-generated types (ADR-006, ADR-007) for type consistency
 * across the TypeScript codebase.
 */

import init, * as wasm from '../wasm/ats_wasm.js';
import { log, SEG } from './logger.ts';
import { timed, timedAsync } from './wasm-metrics';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';

// Re-export proto-generated Attestation type for convenience
export type { Attestation };

/**
 * The rows an AX query asks for: Rust's `AxFilter`. Time is resolved to epoch
 * milliseconds; null is unbounded on that side.
 */
export interface AxFilter {
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    time_start: number | null;
    time_end: number | null;
    source: string | null;
    limit: number;
}

/** An AX query read as a filter, or why it names no rows */
export type AxFilterResult =
    | { ok: true; filter: AxFilter; actions: string[] }
    | { ok: false; error: string };

/** Promise that resolves when WASM is initialized */
let initPromise: Promise<void> | null = null;

/** Default database name for browser IndexedDB storage */
const DEFAULT_DB_NAME = 'qntx';

/**
 * Initialize the WASM module and IndexedDB store.
 * Called automatically on first use. Can be called explicitly for preloading.
 */
export async function initialize(dbName: string = DEFAULT_DB_NAME): Promise<void> {
    if (initPromise) {
        return initPromise;
    }

    initPromise = (async () => {
        // Initialize WASM module
        try {
            await timedAsync('ats', 'instantiate', () => init());
        } catch (error: unknown) {
            // Construct expected WASM URL for debugging
            const wasmUrl = new URL('ats_wasm_bg.wasm', import.meta.url).href;

            // Try to fetch manually to get HTTP status
            let httpStatus = 'unknown';
            try {
                const response = await fetch(wasmUrl);
                httpStatus = `${response.status} ${response.statusText}`;
            } catch (fetchError: unknown) {
                httpStatus = fetchError instanceof Error ? fetchError.message : 'fetch failed';
            }

            const errorMsg = [
                'Failed to initialize WASM module',
                `  Attempted URL: ${wasmUrl}`,
                `  HTTP Status: ${httpStatus}`,
                `  Original error: ${error instanceof Error ? error.message : String(error)}`
            ].join('\n');

            throw new Error(errorMsg);
        }

        // Initialize IndexedDB store
        await timedAsync('ats', 'init_store', () => wasm.init_store(dbName));

        log.info(SEG.WASM, `[ats-wasm] Initialized (v${wasm.version()})`);
    })();

    return initPromise;
}

/** Ensure WASM is initialized before use */
async function ensureInit(): Promise<void> {
    if (!initPromise) {
        await initialize();
    }
    await initPromise;
}

/**
 * Read an AX query as the filter it asks for, its time resolved against now:
 * the same Rust `ax_filter` the node reads queries with, so `since last month`
 * bounds the same rows offline as on the server.
 * Synchronous; the module must be initialized.
 *
 * @example
 * const result = axFilter("ALICE since last month", EVERY_ROW);
 * if (result.ok) {
 *   const rows = await queryAttestations(result.filter);
 * }
 */
export function axFilter(query: string, limit: number): AxFilterResult {
    const answer = JSON.parse(timed('ats', 'ax_filter', () => wasm.ax_filter(query, Date.now(), limit)));

    if ('error' in answer) {
        return { ok: false, error: answer.error };
    }

    return { ok: true, filter: answer.filter, actions: answer.actions };
}

/**
 * Store an attestation in IndexedDB.
 * Returns the attestation on success.
 */
export async function putAttestation(attestation: Attestation): Promise<Attestation> {
    await ensureInit();
    const json = JSON.stringify(attestation);
    await timedAsync('ats', 'put_attestation', () => wasm.put_attestation(json));
    return attestation;
}

/**
 * Retrieve an attestation by ID from IndexedDB.
 * Returns null if not found.
 */
export async function getAttestation(id: string): Promise<Attestation | null> {
    await ensureInit();
    const json = await timedAsync('ats', 'get_attestation', () => wasm.get_attestation(id));
    return json ? JSON.parse(json) : null;
}

/**
 * Delete an attestation by ID from IndexedDB.
 * Returns true if deleted, false if not found.
 */
export async function deleteAttestation(id: string): Promise<boolean> {
    await ensureInit();
    return await timedAsync('ats', 'delete_attestation', () => wasm.delete_attestation(id));
}

/**
 * Check if an attestation exists in IndexedDB.
 */
export async function existsAttestation(id: string): Promise<boolean> {
    await ensureInit();
    return await timedAsync('ats', 'exists_attestation', () => wasm.exists_attestation(id));
}

/** The limit that reads every row, said; Rust's `EVERY_ROW`. */
export const EVERY_ROW = 2147483647;

/**
 * Query attestations from IndexedDB with the filter `axFilter` read.
 * Returns matching attestations in proto format. A limit of 0 is 0 rows.
 */
export async function queryAttestations(filter: AxFilter): Promise<Attestation[]> {
    await ensureInit();
    const filterJson = JSON.stringify(filter);
    const json = await timedAsync('ats', 'query_attestations', () => wasm.query_attestations(filterJson));
    return JSON.parse(json);
}

/**
 * List all attestation IDs in IndexedDB.
 */
export async function listAttestationIds(): Promise<string[]> {
    await ensureInit();
    const json = await timedAsync('ats', 'list_attestation_ids', () => wasm.list_attestation_ids());
    return JSON.parse(json);
}

// ============================================================================
// Cosine Similarity
// ============================================================================

/**
 * Compute cosine similarity between two vectors via WASM.
 * Uses f32 typed arrays directly (no JSON serialization overhead).
 *
 * @throws {Error} If vectors have different dimensions (e.g., "vector dimension mismatch: 384 vs 768")
 */
export function cosineSimilarity(query: Float32Array, candidate: Float32Array): number {
    return wasm.cosine_similarity_f32(query, candidate);
}

// ============================================================================
// Identity
// ============================================================================

/** ASUID generation result from Rust WASM engine */
export interface AsuidResult {
    full: string;
    short: string;
}

/**
 * Generate an ASUID (Attestation System Unique ID) via the Rust WASM engine.
 *
 * Uses crypto.getRandomValues for entropy — no RNG in the Rust crate.
 * Returns human-readable ID: `AS-SARAH-AUTHOR-GITHUB-7K4M3B9X`
 */
export function generateASUID(prefix: string, subject: string, predicate: string, context: string): AsuidResult {
    const randomBytes = new Uint8Array(8);
    crypto.getRandomValues(randomBytes);

    const input = JSON.stringify({
        prefix,
        subject,
        predicate,
        context,
        random_bytes: Array.from(randomBytes),
    });

    const result = JSON.parse(timed('ats', 'generate_asuid', () => wasm.generate_asuid(input)));
    if (result.error) {
        throw new Error(`ASUID generation failed: ${result.error}`);
    }
    return result;
}

// ============================================================================
// Utilities
// ============================================================================

/**
 * Get the ats version.
 */
export function getVersion(): string {
    return wasm.version();
}

/**
 * Check if the store is initialized.
 */
export function isInitialized(): boolean {
    return wasm.is_store_initialized();
}

/** Export raw WASM module for advanced use */
export { wasm };
