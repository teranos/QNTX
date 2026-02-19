/* tslint:disable */
/* eslint-disable */

/**
 * Classify claim conflicts. Takes JSON input with claim groups, temporal config,
 * and current time. Returns JSON with classified conflicts, resolution strategies,
 * and actor rankings.
 *
 * Input:
 * ```json
 * {
 *   "claim_groups": [{"key": "...", "claims": [...]}],
 *   "config": {"verification_window_ms": 60000, ...},
 *   "now_ms": 1234567890
 * }
 * ```
 *
 * Returns JSON with conflicts, auto_resolved count, review_required count.
 */
export function classify_claims(input: string): string;

/**
 * Delete an attestation by ID from IndexedDB.
 * Returns a Promise that resolves to true if deleted, false if not found.
 */
export function delete_attestation(id: string): Promise<boolean>;

/**
 * Check if an attestation exists in IndexedDB.
 * Returns a Promise that resolves to true if exists, false otherwise.
 */
export function exists_attestation(id: string): Promise<boolean>;

/**
 * Rebuild the fuzzy search index from current IndexedDB vocabulary.
 * Pulls distinct predicates and contexts from the attestation store.
 * Returns JSON: {"predicates": N, "contexts": N, "hash": "..."}
 */
export function fuzzy_rebuild_index(): Promise<string>;

/**
 * Search the fuzzy index for matching vocabulary.
 * vocab_type: "predicates" or "contexts"
 * Returns JSON array: [{"value":"...", "score":0.95, "strategy":"exact"}, ...]
 */
export function fuzzy_search(query: string, vocab_type: string, limit: number, min_score: number): string;

/**
 * Get fuzzy engine status.
 * Returns JSON: {"ready": bool, "predicates": N, "contexts": N, "hash": "..."}
 */
export function fuzzy_status(): string;

/**
 * Retrieve an attestation by ID from IndexedDB.
 * Returns a Promise that resolves to JSON-serialized attestation or null if not found.
 *
 * Returns JSON matching proto schema (timestamps as numbers, attributes as JSON string).
 * Converts from internal core::Attestation format before serialization.
 */
export function get_attestation(id: string): Promise<string | undefined>;

/**
 * Initialize the IndexedDB store. Must be called before any storage operations.
 * Returns a Promise that resolves when initialization is complete.
 */
export function init_store(db_name?: string | null): Promise<void>;

/**
 * Check if the store is initialized.
 */
export function is_store_initialized(): boolean;

/**
 * Get all attestation IDs from IndexedDB.
 * Returns a Promise that resolves to JSON array of IDs.
 */
export function list_attestation_ids(): Promise<string>;

/**
 * Parse an AX query string. Returns JSON-serialized AxQuery or error.
 *
 * Returns: `{"subjects":["ALICE"],"predicates":["author"],...}` on success
 *          `{"error":"description"}` on error
 */
export function parse_query(input: string): string;

/**
 * Store an attestation in IndexedDB.
 * Returns a Promise that resolves to null on success or error message on failure.
 *
 * Expects JSON matching proto schema (timestamps as numbers, attributes as JSON string).
 * Converts to internal core::Attestation format before storage.
 */
export function put_attestation(json: string): Promise<void>;

/**
 * Query attestations from IndexedDB using an AxFilter.
 * Expects JSON-serialized AxFilter. Returns JSON array of proto-format attestations.
 */
export function query_attestations(filter_json: string): Promise<string>;

/**
 * Compute cosine similarity between two embedding vectors.
 * Input: `{"a":[f32,...], "b":[f32,...]}`
 * Returns: `{"similarity": f32}` or `{"error":"..."}`
 */
export function semantic_similarity(input: string): string;

/**
 * Compute content hash for an attestation.
 * Input: JSON-serialized Attestation
 * Returns: `{"hash":"<64-char hex>"}` or `{"error":"..."}`
 */
export function sync_content_hash(attestation_json: string): string;

/**
 * Check if a content hash exists in the global Merkle tree.
 * Input: `{"content_hash":"<hex>"}`
 * Returns: `{"exists":true|false}`
 */
export function sync_merkle_contains(input: string): string;

/**
 * Diff Merkle tree against remote group hashes.
 * Input: `{"remote":{"<hex>":"<hex>",...}}`
 * Returns: `{"local_only":[...],"remote_only":[...],"divergent":[...]}`
 */
export function sync_merkle_diff(remote_json: string): string;

/**
 * Reverse-lookup a group key hash to its (actor, context) pair.
 * Input: `{"group_key_hash":"<hex>"}`
 * Returns: `{"actor":"...","context":"..."}` or `{"error":"group not found"}`
 */
export function sync_merkle_find_group_key(input: string): string;

/**
 * Get all group hashes from the Merkle tree.
 * Returns: `{"groups":{"<hex>":"<hex>",...}}`
 */
export function sync_merkle_group_hashes(): string;

/**
 * Insert into the global Merkle tree.
 * Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
 * Returns: `{"ok":true}` or `{"error":"..."}`
 */
export function sync_merkle_insert(input: string): string;

/**
 * Remove from the global Merkle tree.
 * Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
 * Returns: `{"ok":true}`
 */
export function sync_merkle_remove(input: string): string;

/**
 * Get the Merkle tree root hash and stats.
 * Returns: `{"root":"<hex>","size":N,"groups":N}`
 */
export function sync_merkle_root(): string;

/**
 * Get the qntx-core version.
 */
export function version(): string;

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly classify_claims: (a: number, b: number) => [number, number];
    readonly delete_attestation: (a: number, b: number) => any;
    readonly exists_attestation: (a: number, b: number) => any;
    readonly fuzzy_rebuild_index: () => any;
    readonly fuzzy_search: (a: number, b: number, c: number, d: number, e: number, f: number) => [number, number, number, number];
    readonly fuzzy_status: () => [number, number];
    readonly get_attestation: (a: number, b: number) => any;
    readonly init_store: (a: number, b: number) => any;
    readonly is_store_initialized: () => number;
    readonly list_attestation_ids: () => any;
    readonly parse_query: (a: number, b: number) => [number, number];
    readonly put_attestation: (a: number, b: number) => any;
    readonly query_attestations: (a: number, b: number) => any;
    readonly semantic_similarity: (a: number, b: number) => [number, number];
    readonly sync_content_hash: (a: number, b: number) => [number, number];
    readonly sync_merkle_contains: (a: number, b: number) => [number, number];
    readonly sync_merkle_diff: (a: number, b: number) => [number, number];
    readonly sync_merkle_find_group_key: (a: number, b: number) => [number, number];
    readonly sync_merkle_group_hashes: () => [number, number];
    readonly sync_merkle_insert: (a: number, b: number) => [number, number];
    readonly sync_merkle_remove: (a: number, b: number) => [number, number];
    readonly sync_merkle_root: () => [number, number];
    readonly version: () => [number, number];
    readonly wasm_bindgen__closure__destroy__h622d11ff1c80a730: (a: number, b: number) => void;
    readonly wasm_bindgen__closure__destroy__h63454322f75c3832: (a: number, b: number) => void;
    readonly wasm_bindgen__convert__closures_____invoke__h0970674685b3ee7c: (a: number, b: number, c: any, d: any) => void;
    readonly wasm_bindgen__convert__closures_____invoke__ha99d37861838e4ea: (a: number, b: number, c: any) => void;
    readonly wasm_bindgen__convert__closures_____invoke__h857fdb0c9bdea0c8: (a: number, b: number, c: any) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
