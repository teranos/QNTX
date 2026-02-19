/**
 * Browser sync — replicates server attestations + embeddings to IndexedDB.
 *
 * On WebSocket connect:
 * 1. Rebuild in-memory Merkle tree from existing IndexedDB attestations
 * 2. Send browser_sync_hello with current Merkle root
 * 3. Server streams missing attestations + embeddings in batches
 * 4. Browser stores attestations in IndexedDB and embeddings in a separate store
 */

import { sendMessage } from './websocket';
import { log, SEG } from './logger';
import {
    putAttestation,
    syncMerkleRoot,
    syncMerkleInsert,
    syncContentHash,
    listAttestationIds,
    getAttestation,
    initialize as initWasm,
} from './qntx-wasm';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';
import type {
    BrowserSyncAttestationsMessage,
    BrowserSyncDoneMessage,
    BrowserSyncEmbedding,
} from '../types/websocket';

// ============================================================================
// Embedding IndexedDB Store
// ============================================================================

const EMBEDDING_DB_NAME = 'qntx-embeddings';
const EMBEDDING_DB_VERSION = 2;
const EMBEDDING_STORE_NAME = 'vectors';
const QUERY_EMBEDDING_STORE_NAME = 'query_embeddings';

/** Stored embedding record */
export interface StoredEmbedding {
    attestation_id: string;
    vector: number[];
    model: string;
}

/** Stored query embedding (for offline SE search) */
export interface StoredQueryEmbedding {
    watcher_id: string;
    vector: number[];
}

/** Open (or create) the embeddings IndexedDB */
function openEmbeddingDB(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open(EMBEDDING_DB_NAME, EMBEDDING_DB_VERSION);

        request.onupgradeneeded = () => {
            const db = request.result;
            if (!db.objectStoreNames.contains(EMBEDDING_STORE_NAME)) {
                db.createObjectStore(EMBEDDING_STORE_NAME, { keyPath: 'attestation_id' });
            }
            if (!db.objectStoreNames.contains(QUERY_EMBEDDING_STORE_NAME)) {
                db.createObjectStore(QUERY_EMBEDDING_STORE_NAME, { keyPath: 'watcher_id' });
            }
        };

        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}

/** Store a batch of embeddings in IndexedDB */
async function storeEmbeddings(embeddings: BrowserSyncEmbedding[]): Promise<void> {
    if (embeddings.length === 0) return;

    const db = await openEmbeddingDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(EMBEDDING_STORE_NAME, 'readwrite');
        const store = tx.objectStore(EMBEDDING_STORE_NAME);

        for (const emb of embeddings) {
            store.put({
                attestation_id: emb.attestation_id,
                vector: emb.vector,
                model: emb.model,
            } satisfies StoredEmbedding);
        }

        tx.oncomplete = () => { db.close(); resolve(); };
        tx.onerror = () => { db.close(); reject(tx.error); };
    });
}

/** Get a single embedding by attestation ID */
export async function getEmbedding(attestationId: string): Promise<StoredEmbedding | null> {
    const db = await openEmbeddingDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(EMBEDDING_STORE_NAME, 'readonly');
        const store = tx.objectStore(EMBEDDING_STORE_NAME);
        const request = store.get(attestationId);

        request.onsuccess = () => { db.close(); resolve(request.result ?? null); };
        request.onerror = () => { db.close(); reject(request.error); };
    });
}

/** Get all stored embeddings (for local semantic search) */
export async function getAllEmbeddings(): Promise<StoredEmbedding[]> {
    const db = await openEmbeddingDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(EMBEDDING_STORE_NAME, 'readonly');
        const store = tx.objectStore(EMBEDDING_STORE_NAME);
        const request = store.getAll();

        request.onsuccess = () => { db.close(); resolve(request.result); };
        request.onerror = () => { db.close(); reject(request.error); };
    });
}

/** Store a query embedding for offline SE search */
export async function storeQueryEmbedding(watcherId: string, vector: number[]): Promise<void> {
    const db = await openEmbeddingDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(QUERY_EMBEDDING_STORE_NAME, 'readwrite');
        const store = tx.objectStore(QUERY_EMBEDDING_STORE_NAME);
        store.put({ watcher_id: watcherId, vector } satisfies StoredQueryEmbedding);

        tx.oncomplete = () => { db.close(); resolve(); };
        tx.onerror = () => { db.close(); reject(tx.error); };
    });
}

/** Get a query embedding by watcher ID */
export async function getQueryEmbedding(watcherId: string): Promise<number[] | null> {
    const db = await openEmbeddingDB();
    return new Promise((resolve, reject) => {
        const tx = db.transaction(QUERY_EMBEDDING_STORE_NAME, 'readonly');
        const store = tx.objectStore(QUERY_EMBEDDING_STORE_NAME);
        const request = store.get(watcherId);

        request.onsuccess = () => {
            db.close();
            const result = request.result as StoredQueryEmbedding | undefined;
            resolve(result?.vector ?? null);
        };
        request.onerror = () => { db.close(); reject(request.error); };
    });
}

// ============================================================================
// Merkle Tree Rebuild
// ============================================================================

/**
 * Rebuild the in-memory Merkle tree from existing IndexedDB attestations.
 * Must be called before initiateBrowserSync() so the root hash reflects
 * what the browser already has.
 */
export async function rebuildMerkleFromIndexedDB(): Promise<void> {
    try {
        await initWasm();
    } catch (err) {
        log.warn(SEG.WASM, 'Browser sync: WASM init failed, skipping Merkle rebuild:', err);
        return;
    }

    const ids = await listAttestationIds();
    if (ids.length === 0) {
        log.debug(SEG.WASM, 'Browser sync: IndexedDB empty, Merkle tree stays empty');
        return;
    }

    let inserted = 0;
    for (const id of ids) {
        const att = await getAttestation(id);
        if (!att) continue;

        try {
            // Strip attributes — content hash doesn't use them, and proto format
            // (attributes as JSON string) would fail core Attestation deserialization.
            const { attributes, ...hashableAtt } = att as Record<string, unknown>;
            const hash = syncContentHash(JSON.stringify(hashableAtt));
            const actor = (att as any).actors?.[0] || '';
            const context = (att as any).contexts?.[0] || '';
            if (actor && context) {
                syncMerkleInsert(actor, context, hash);
                inserted++;
            }
        } catch (err) {
            log.debug(SEG.WASM, `Browser sync: failed to hash attestation ${id}:`, err);
        }
    }

    const root = syncMerkleRoot();
    log.info(SEG.WASM, `Browser sync: rebuilt Merkle tree from ${inserted} attestations, root=${root.root.substring(0, 12)}`);
}

// ============================================================================
// Sync Protocol
// ============================================================================

/**
 * Initiate browser sync — called after WebSocket connects.
 * Sends the current browser Merkle root to the server.
 */
export async function initiateBrowserSync(): Promise<void> {
    try {
        await initWasm();
    } catch (err) {
        log.warn(SEG.WASM, 'Browser sync: WASM not available, skipping:', err);
        return;
    }

    await rebuildMerkleFromIndexedDB();

    const root = syncMerkleRoot();
    log.info(SEG.WASM, `Browser sync: sent hello (root=${root.root.substring(0, 12)}, size=${root.size})`);

    sendMessage({
        type: 'browser_sync_hello',
        sync_root: root.root,
    });
}

/**
 * Handle a batch of attestations + embeddings from the server.
 */
export async function handleSyncAttestations(data: BrowserSyncAttestationsMessage): Promise<void> {
    const attestations = data.attestations as Attestation[];

    // Store attestations in IndexedDB and insert into Merkle tree
    for (const att of attestations) {
        try {
            await putAttestation(att);
        } catch (err) {
            log.debug(SEG.WASM, `Browser sync: failed to store attestation:`, err);
            continue;
        }

        try {
            // syncContentHash expects core Attestation format (attributes as object),
            // but server sends proto format (attributes as JSON string). Strip attributes
            // since content hash doesn't use them anyway.
            const { attributes, ...hashableAtt } = att as Record<string, unknown>;
            const hash = syncContentHash(JSON.stringify(hashableAtt));
            const actor = (att as any).actors?.[0] || '';
            const context = (att as any).contexts?.[0] || '';
            if (actor && context) {
                syncMerkleInsert(actor, context, hash);
            }
        } catch (err) {
            log.debug(SEG.WASM, `Browser sync: Merkle insert failed:`, err);
        }
    }

    // Store embeddings
    if (data.embeddings.length > 0) {
        try {
            await storeEmbeddings(data.embeddings);
        } catch (err) {
            log.warn(SEG.WASM, 'Browser sync: failed to store embeddings:', err);
        }
    }

    if (data.done) {
        const root = syncMerkleRoot();
        log.info(SEG.WASM, `Browser sync complete: stored ${data.stored}/${data.total} attestations, ${data.embeddings.length} embeddings, root=${root.root.substring(0, 12)}`);
    } else {
        log.debug(SEG.WASM, `Browser sync: batch ${data.stored}/${data.total}`);
    }
}

/**
 * Handle browser_sync_done — server says roots match.
 */
export function handleSyncDone(data: BrowserSyncDoneMessage): void {
    log.info(SEG.WASM, `Browser sync: ${data.message}`);
}
