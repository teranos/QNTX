/* tslint:disable */
/* eslint-disable */

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
 * Retrieve an attestation by ID from IndexedDB.
 * Returns a Promise that resolves to JSON-serialized attestation or null if not found.
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
 */
export function put_attestation(json: string): Promise<void>;

/**
 * Get the qntx-core version.
 */
export function version(): string;

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly delete_attestation: (a: number, b: number) => any;
    readonly exists_attestation: (a: number, b: number) => any;
    readonly get_attestation: (a: number, b: number) => any;
    readonly init_store: (a: number, b: number) => any;
    readonly is_store_initialized: () => number;
    readonly list_attestation_ids: () => any;
    readonly parse_query: (a: number, b: number) => [number, number];
    readonly put_attestation: (a: number, b: number) => any;
    readonly version: () => [number, number];
    readonly wasm_bindgen__closure__destroy__hd71dd998f6ad6b2e: (a: number, b: number) => void;
    readonly wasm_bindgen__closure__destroy__h622d11ff1c80a730: (a: number, b: number) => void;
    readonly wasm_bindgen__convert__closures_____invoke__h0970674685b3ee7c: (a: number, b: number, c: any, d: any) => void;
    readonly wasm_bindgen__convert__closures_____invoke__h2745a8c01784b9af: (a: number, b: number, c: any) => void;
    readonly wasm_bindgen__convert__closures_____invoke__ha99d37861838e4ea: (a: number, b: number, c: any) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
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
