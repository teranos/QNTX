/**
 * Browser Sync Orchestrator
 *
 * Merkle tree lifecycle + sync protocol client.
 * Speaks the same symmetric sync protocol as peer-to-peer (sync/protocol.go Msg format)
 * over a dedicated WebSocket to /ws/sync. The browser is just another peer.
 */

import { log, SEG } from './logger';
import { connectivityManager } from './connectivity';
import { embeddingStore } from './embedding-store';
import { apiFetch } from './api';
import {
    listAttestationIds,
    getAttestation,
    putAttestation,
    syncContentHash,
    syncMerkleInsert,
    syncMerkleRoot,
    syncMerkleGroupHashes,
    syncMerkleDiff,
    syncMerkleFindGroupKey,
    queryAttestations,
    type Attestation,
} from './qntx-wasm';

// ============================================================================
// Sync Protocol Types (mirrors sync/protocol.go Msg)
// ============================================================================

type MsgType = 'sync_hello' | 'sync_group_hashes' | 'sync_need' | 'sync_attestations' | 'sync_done';

interface SyncMsg {
    type: MsgType;
    root_hash?: string;
    name?: string;
    groups?: Record<string, string>;
    need?: string[];
    attestations?: Record<string, Attestation[]>;
    sent?: number;
    received?: number;
}

// ============================================================================
// BrowserSync
// ============================================================================

const EMBEDDING_BATCH_SIZE = 100;

export class BrowserSync {
    private periodicTimer: number | null = null;
    private connectivityUnsub: (() => void) | null = null;
    private reconciling = false;

    /**
     * Rebuild the Merkle tree from all attestations in IndexedDB.
     * Call after WASM init completes.
     */
    async initialize(): Promise<void> {
        const ids = await listAttestationIds();
        let inserted = 0;
        let errors = 0;

        for (const id of ids) {
            try {
                const attestation = await getAttestation(id);
                if (!attestation) continue;

                const json = JSON.stringify(attestation);
                const contentHash = syncContentHash(json);

                // Each attestation belongs to every (actor, context) pair
                const actors = attestation.actors ?? [];
                const contexts = attestation.contexts ?? [];

                if (actors.length === 0 || contexts.length === 0) {
                    // Attestations without actors/contexts still need to be tracked
                    // Use empty string as default group key component
                    syncMerkleInsert(
                        actors[0] ?? '',
                        contexts[0] ?? '',
                        contentHash,
                    );
                    inserted++;
                    continue;
                }

                for (const actor of actors) {
                    for (const context of contexts) {
                        syncMerkleInsert(actor, context, contentHash);
                    }
                }
                inserted++;
            } catch (err) {
                errors++;
                log.warn(SEG.WASM, `[BrowserSync] Failed to index attestation ${id}:`, err);
            }
        }

        const root = syncMerkleRoot();
        log.info(SEG.WASM, `[BrowserSync] Tree rebuilt: ${inserted} attestations, ${root.groups} groups, root=${root.root.slice(0, 12)}... (${errors} errors)`);

        // Subscribe to connectivity changes for auto-sync
        this.connectivityUnsub = connectivityManager.subscribe((state) => {
            if (state === 'online') {
                this.reconcile().catch(err => {
                    log.warn(SEG.WASM, '[BrowserSync] Auto-reconcile on online failed:', err);
                });
            }
        });
    }

    /**
     * Run one reconciliation cycle with the server.
     * Returns count of attestations sent and received.
     */
    async reconcile(): Promise<{ sent: number; received: number }> {
        if (this.reconciling) {
            log.debug(SEG.WASM, '[BrowserSync] Reconciliation already in progress, skipping');
            return { sent: 0, received: 0 };
        }

        this.reconciling = true;
        try {
            return await this.doReconcile();
        } finally {
            this.reconciling = false;
        }
    }

    startPeriodicSync(intervalMs: number): void {
        this.stopPeriodicSync();
        this.periodicTimer = window.setInterval(() => {
            if (connectivityManager.state === 'online') {
                this.reconcile().catch(err => {
                    log.warn(SEG.WASM, '[BrowserSync] Periodic sync failed:', err);
                });
            }
        }, intervalMs);
        log.info(SEG.WASM, `[BrowserSync] Periodic sync started (${intervalMs}ms)`);
    }

    stopPeriodicSync(): void {
        if (this.periodicTimer !== null) {
            clearInterval(this.periodicTimer);
            this.periodicTimer = null;
        }
    }

    destroy(): void {
        this.stopPeriodicSync();
        if (this.connectivityUnsub) {
            this.connectivityUnsub();
            this.connectivityUnsub = null;
        }
    }

    // ========================================================================
    // Private: Sync Protocol
    // ========================================================================

    private async doReconcile(): Promise<{ sent: number; received: number }> {
        const ws = await this.connectSyncWebSocket();
        let sent = 0;
        let received = 0;
        const receivedIds: string[] = [];

        try {
            // Phase 1: Exchange root hashes
            const localRoot = syncMerkleRoot();

            wsSend(ws, {
                type: 'sync_hello',
                root_hash: localRoot.root,
                name: 'browser',
            });

            const hello = await wsRecv(ws);
            if (hello.type !== 'sync_hello') {
                throw new Error(`Expected sync_hello, got ${hello.type}`);
            }

            // Roots match — already in sync
            if (hello.root_hash === localRoot.root) {
                log.debug(SEG.WASM, '[BrowserSync] Roots match, already in sync');
                wsSend(ws, { type: 'sync_done', sent: 0, received: 0 });
                await wsRecv(ws); // recv server's sync_done
                return { sent: 0, received: 0 };
            }

            log.debug(SEG.WASM, `[BrowserSync] Roots differ, local=${localRoot.root.slice(0, 12)}... remote=${hello.root_hash?.slice(0, 12)}...`);

            // Phase 2: Exchange group hashes
            const localGroups = syncMerkleGroupHashes();

            wsSend(ws, {
                type: 'sync_group_hashes',
                groups: localGroups,
            });

            const remoteGroupsMsg = await wsRecv(ws);
            if (remoteGroupsMsg.type !== 'sync_group_hashes') {
                throw new Error(`Expected sync_group_hashes, got ${remoteGroupsMsg.type}`);
            }

            // Phase 3: Compute diff and exchange needs
            const diff = syncMerkleDiff(remoteGroupsMsg.groups ?? {});
            const needed = [...diff.remote_only, ...diff.divergent];

            wsSend(ws, {
                type: 'sync_need',
                need: needed,
            });

            const needMsg = await wsRecv(ws);
            if (needMsg.type !== 'sync_need') {
                throw new Error(`Expected sync_need, got ${needMsg.type}`);
            }

            // Phase 4: Fulfill server's requests — send attestations they need
            const outgoing: Record<string, Attestation[]> = {};
            for (const hexKey of needMsg.need ?? []) {
                const groupKey = syncMerkleFindGroupKey(hexKey);
                if (!groupKey) continue;

                const attestations = await queryAttestations({
                    subjects: [],
                    predicates: [],
                    actors: [groupKey.actor],
                    contexts: [groupKey.context],
                });

                if (attestations.length > 0) {
                    outgoing[hexKey] = attestations;
                    sent += attestations.length;
                }
            }

            wsSend(ws, {
                type: 'sync_attestations',
                attestations: outgoing,
            });

            // Receive attestations we requested
            const attMsg = await wsRecv(ws);
            if (attMsg.type !== 'sync_attestations') {
                throw new Error(`Expected sync_attestations, got ${attMsg.type}`);
            }

            // Process received attestations
            for (const [, attestations] of Object.entries(attMsg.attestations ?? {})) {
                for (const attestation of attestations) {
                    try {
                        // Store in IndexedDB via WASM
                        await putAttestation(attestation);

                        // Insert into Merkle tree
                        const json = JSON.stringify(attestation);
                        const contentHash = syncContentHash(json);
                        const actors = attestation.actors ?? [];
                        const contexts = attestation.contexts ?? [];

                        for (const actor of actors.length > 0 ? actors : ['']) {
                            for (const context of contexts.length > 0 ? contexts : ['']) {
                                syncMerkleInsert(actor, context, contentHash);
                            }
                        }

                        received++;
                        if (attestation.id) {
                            receivedIds.push(attestation.id);
                        }
                    } catch (err) {
                        log.warn(SEG.WASM, `[BrowserSync] Failed to store received attestation ${attestation.id}:`, err);
                    }
                }
            }

            // Phase 5: Exchange done
            wsSend(ws, { type: 'sync_done', sent, received });
            await wsRecv(ws); // recv server's sync_done

            if (sent > 0 || received > 0) {
                log.info(SEG.WASM, `[BrowserSync] Reconciled: sent=${sent} received=${received}`);
            }
        } finally {
            ws.close();
        }

        // Post-sync: fetch embeddings for newly received attestations
        if (receivedIds.length > 0) {
            await this.fetchEmbeddings(receivedIds);
        }

        return { sent, received };
    }

    private connectSyncWebSocket(): Promise<WebSocket> {
        return new Promise((resolve, reject) => {
            const backendUrl = (window as any).__BACKEND_URL__ || window.location.origin;
            const backendHost = backendUrl.replace(/^https?:\/\//, '');
            const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
            const wsUrl = `${protocol}//${backendHost}/ws/sync`;

            const ws = new WebSocket(wsUrl);

            const timeout = window.setTimeout(() => {
                ws.close();
                reject(new Error(`Sync WebSocket connection timeout to ${wsUrl}`));
            }, 10_000);

            ws.onopen = () => {
                clearTimeout(timeout);
                resolve(ws);
            };

            ws.onerror = () => {
                clearTimeout(timeout);
                reject(new Error(`Sync WebSocket connection failed to ${wsUrl}`));
            };
        });
    }

    // ========================================================================
    // Private: Post-sync embedding fetch (Step 6)
    // ========================================================================

    private async fetchEmbeddings(attestationIds: string[]): Promise<void> {
        try {
            await embeddingStore.open();
        } catch (err) {
            log.warn(SEG.WASM, '[BrowserSync] Failed to open embedding store, skipping embedding fetch:', err);
            return;
        }

        // Filter out IDs that already have local embeddings
        const existingIds = new Set(await embeddingStore.listIds());
        const missing = attestationIds.filter(id => !existingIds.has(id));
        if (missing.length === 0) return;

        log.debug(SEG.WASM, `[BrowserSync] Fetching embeddings for ${missing.length} new attestations`);

        // Batch fetch
        for (let i = 0; i < missing.length; i += EMBEDDING_BATCH_SIZE) {
            const batch = missing.slice(i, i + EMBEDDING_BATCH_SIZE);

            try {
                const response = await apiFetch('/api/embeddings/by-source', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ source_ids: batch }),
                });

                if (!response.ok) {
                    log.warn(SEG.WASM, `[BrowserSync] Embedding fetch failed: ${response.status}`);
                    continue;
                }

                const data = await response.json() as {
                    embeddings: Array<{
                        source_id: string;
                        vector: number[];
                        model: string;
                        dimensions: number;
                    }>;
                };

                for (const emb of data.embeddings) {
                    await embeddingStore.put(
                        emb.source_id,
                        new Float32Array(emb.vector),
                        emb.model,
                    );
                }

                log.debug(SEG.WASM, `[BrowserSync] Stored ${data.embeddings.length} embeddings (batch ${Math.floor(i / EMBEDDING_BATCH_SIZE) + 1})`);
            } catch (err) {
                log.warn(SEG.WASM, `[BrowserSync] Embedding fetch error for batch starting at ${i}:`, err);
            }
        }
    }
}

// ============================================================================
// WebSocket Helpers
// ============================================================================

function wsSend(ws: WebSocket, msg: SyncMsg): void {
    ws.send(JSON.stringify(msg));
}

const RECV_TIMEOUT_MS = 30_000;

function wsRecv(ws: WebSocket): Promise<SyncMsg> {
    return new Promise((resolve, reject) => {
        const cleanup = () => {
            clearTimeout(timer);
            ws.removeEventListener('message', onMessage);
            ws.removeEventListener('close', onClose);
            ws.removeEventListener('error', onError);
        };

        const timer = window.setTimeout(() => {
            cleanup();
            reject(new Error(`Sync WebSocket recv timeout (${RECV_TIMEOUT_MS}ms)`));
        }, RECV_TIMEOUT_MS);

        const onMessage = (event: MessageEvent) => {
            cleanup();
            try {
                resolve(JSON.parse(event.data));
            } catch (err) {
                reject(new Error(`Failed to parse sync message: ${err}`));
            }
        };

        const onClose = () => {
            cleanup();
            reject(new Error('Sync WebSocket closed unexpectedly'));
        };

        const onError = () => {
            cleanup();
            reject(new Error('Sync WebSocket error'));
        };

        ws.addEventListener('message', onMessage);
        ws.addEventListener('close', onClose);
        ws.addEventListener('error', onError);
    });
}

/** Singleton instance */
export const browserSync = new BrowserSync();
