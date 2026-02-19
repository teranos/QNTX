/**
 * QNTX WASM module - Browser attestation storage with IndexedDB
 *
 * Provides parser and minimal storage functions for browser-based
 * attestation management. Automatically initialized on import.
 *
 * Uses proto-generated types (ADR-006, ADR-007) for type consistency
 * across the TypeScript codebase.
 */

import init, * as wasm from '../wasm/qntx_wasm.js';
import { log, SEG } from './logger.ts';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';

// Re-export proto-generated Attestation type for convenience
export type { Attestation };

/** Parsed AX query */
export interface AxQuery {
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    temporal?: unknown;
    [key: string]: unknown;
}

/** Query parse result */
export type ParseResult =
    | { ok: true; query: AxQuery }
    | { ok: false; error: string };

/** A fuzzy match result from the WASM engine */
export interface FuzzyMatch {
    value: string;
    score: number;
    strategy: string;
}

/** Fuzzy index rebuild statistics */
export interface FuzzyIndexStats {
    predicates: number;
    contexts: number;
    hash: string;
}

/** Fuzzy engine status */
export interface FuzzyStatus {
    ready: boolean;
    predicates: number;
    contexts: number;
    hash: string;
}

/** Vocabulary type for fuzzy search */
export type FuzzyVocabType = 'predicates' | 'contexts';

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
            await init();
        } catch (error: unknown) {
            // Construct expected WASM URL for debugging
            const wasmUrl = new URL('qntx_wasm_bg.wasm', import.meta.url).href;

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
        await wasm.init_store(dbName);

        // Build fuzzy index from existing IndexedDB vocabulary
        const statsJson = await wasm.fuzzy_rebuild_index();
        const stats: FuzzyIndexStats = JSON.parse(statsJson);
        log.info(SEG.WASM, `[qntx-wasm] Initialized (v${wasm.version()}) fuzzy: ${stats.predicates}P/${stats.contexts}C`);
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
 * Parse an AX query string.
 * Synchronous operation, no initialization required.
 *
 * @example
 * const result = parseQuery("ALICE author ARTICLE");
 * if (result.ok) {
 *   console.log(result.query);
 * } else {
 *   console.error(result.error);
 * }
 */
export function parseQuery(input: string): ParseResult {
    const json = wasm.parse_query(input);
    const parsed = JSON.parse(json);

    if ('error' in parsed) {
        return { ok: false, error: parsed.error };
    }

    return { ok: true, query: parsed };
}

/**
 * Store an attestation in IndexedDB.
 * Returns the attestation on success.
 */
export async function putAttestation(attestation: Attestation): Promise<Attestation> {
    await ensureInit();
    const json = JSON.stringify(attestation);
    await wasm.put_attestation(json);
    return attestation;
}

/**
 * Retrieve an attestation by ID from IndexedDB.
 * Returns null if not found.
 */
export async function getAttestation(id: string): Promise<Attestation | null> {
    await ensureInit();
    const json = await wasm.get_attestation(id);
    return json ? JSON.parse(json) : null;
}

/**
 * Delete an attestation by ID from IndexedDB.
 * Returns true if deleted, false if not found.
 */
export async function deleteAttestation(id: string): Promise<boolean> {
    await ensureInit();
    return await wasm.delete_attestation(id);
}

/**
 * Check if an attestation exists in IndexedDB.
 */
export async function existsAttestation(id: string): Promise<boolean> {
    await ensureInit();
    return await wasm.exists_attestation(id);
}

/**
 * Query attestations from IndexedDB using an AxFilter.
 * Returns matching attestations in proto format.
 */
export async function queryAttestations(filter: AxQuery): Promise<Attestation[]> {
    await ensureInit();
    const json = await wasm.query_attestations(JSON.stringify(filter));
    return JSON.parse(json);
}

/**
 * List all attestation IDs in IndexedDB.
 */
export async function listAttestationIds(): Promise<string[]> {
    await ensureInit();
    const json = await wasm.list_attestation_ids();
    return JSON.parse(json);
}

// ============================================================================
// Fuzzy Search
// ============================================================================

/**
 * Rebuild the fuzzy search index from current IndexedDB vocabulary.
 * Call after init, and again after storing attestations with new predicates/contexts.
 * Hash-based dedup makes redundant calls cheap.
 */
export async function rebuildFuzzyIndex(): Promise<FuzzyIndexStats> {
    await ensureInit();
    const json = await wasm.fuzzy_rebuild_index();
    return JSON.parse(json);
}

/**
 * Search vocabulary using the fuzzy engine.
 * Returns matches sorted by score descending.
 */
export function fuzzySearch(
    query: string,
    vocabType: FuzzyVocabType,
    limit: number = 20,
    minScore: number = 0.6,
): FuzzyMatch[] {
    const json = wasm.fuzzy_search(query, vocabType, limit, minScore);
    return JSON.parse(json);
}

/**
 * Get fuzzy engine status (ready state, vocabulary counts, index hash).
 */
export function getFuzzyStatus(): FuzzyStatus {
    return JSON.parse(wasm.fuzzy_status());
}

// ============================================================================
// Similarity
// ============================================================================

/**
 * Compute cosine similarity between two embedding vectors.
 * Returns a value between -1 and 1 (1 = identical direction).
 */
export function semanticSimilarity(a: number[], b: number[]): number {
    const json = wasm.semantic_similarity(JSON.stringify({ a, b }));
    const result = JSON.parse(json);
    if ('error' in result) {
        throw new Error(result.error);
    }
    return result.similarity;
}

// ============================================================================
// Sync (Merkle Tree)
// ============================================================================

/** Merkle tree root info */
export interface MerkleRootInfo {
    root: string;
    size: number;
    groups: number;
}

/**
 * Get the browser Merkle tree root hash and stats.
 */
export function syncMerkleRoot(): MerkleRootInfo {
    return JSON.parse(wasm.sync_merkle_root());
}

/**
 * Insert an attestation into the browser Merkle tree.
 * Input: { actor, context, content_hash }
 */
export function syncMerkleInsert(actor: string, context: string, contentHash: string): void {
    const result = JSON.parse(wasm.sync_merkle_insert(JSON.stringify({
        actor,
        context,
        content_hash: contentHash,
    })));
    if (result.error) {
        throw new Error(result.error);
    }
}

/**
 * Compute content hash for an attestation JSON string.
 */
export function syncContentHash(attestationJson: string): string {
    const result = JSON.parse(wasm.sync_content_hash(attestationJson));
    if (result.error) {
        throw new Error(result.error);
    }
    return result.hash;
}

// ============================================================================
// Utilities
// ============================================================================

/**
 * Get the qntx-core version.
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
