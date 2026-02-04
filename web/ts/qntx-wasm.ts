/**
 * QNTX WASM module - Browser attestation storage with IndexedDB
 *
 * Provides parser and minimal storage functions for browser-based
 * attestation management. Automatically initialized on import.
 */

import init, * as wasm from '../wasm/qntx_wasm.js';

/** Attestation from IndexedDB */
export interface Attestation {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    timestamp?: number;
    created_at?: number;
    [key: string]: unknown;
}

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
        await init();

        // Initialize IndexedDB store
        await wasm.init_store(dbName);

        console.log(`[qntx-wasm] Initialized (v${wasm.version()})`);
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
 * List all attestation IDs in IndexedDB.
 */
export async function listAttestationIds(): Promise<string[]> {
    await ensureInit();
    const json = await wasm.list_attestation_ids();
    return JSON.parse(json);
}

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
