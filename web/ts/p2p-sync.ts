/**
 * Peer-to-Peer Sync Orchestrator
 *
 * Browser-to-browser Merkle sync over WebRTC.
 * Reuses the same sync protocol as browser-to-server, just different transport.
 */

import { log, SEG } from './logger';
import { WebRTCPeer } from './webrtc-peer';
import {
    syncMerkleRoot,
    syncMerkleGroupHashes,
    syncMerkleFindGroupKey,
    listAttestationIds,
    getAttestation,
    putAttestation,
} from './qntx-wasm';

interface SyncMessage {
    type: 'sync_hello' | 'sync_group_hashes' | 'sync_need' | 'sync_attestations' | 'sync_done';
    root_hash?: string;
    name?: string;
    groups?: Record<string, string>;
    need?: string[];
    attestations?: Record<string, any[]>;
    sent?: number;
    received?: number;
}

export interface P2PSyncResult {
    sent: number;
    received: number;
}

/**
 * Sync with a remote peer over WebRTC.
 *
 * Same Merkle reconciliation logic as server sync, but peer-to-peer.
 * Works completely offline — no server required.
 */
export class P2PSync {
    private peer: WebRTCPeer;

    constructor(peer: WebRTCPeer) {
        this.peer = peer;
    }

    /**
     * Perform Merkle reconciliation with remote peer.
     *
     * Returns number of attestations sent and received.
     */
    async reconcile(): Promise<P2PSyncResult> {
        await this.peer.waitForOpen();

        let sent = 0;
        let received = 0;

        // Phase 1: Exchange root hashes
        const localRoot = syncMerkleRoot();

        this.sendMessage({
            type: 'sync_hello',
            root_hash: localRoot.root,
            name: 'browser-peer',
        });

        const hello = await this.receiveMessage();
        if (hello.type !== 'sync_hello') {
            throw new Error(`Expected sync_hello, got ${hello.type}`);
        }

        // Roots match — already in sync
        if (hello.root_hash === localRoot.root) {
            log.debug(SEG.WASM, '[P2PSync] Roots match, already in sync');
            this.sendMessage({ type: 'sync_done', sent: 0, received: 0 });
            await this.receiveMessage(); // recv peer's sync_done
            return { sent: 0, received: 0 };
        }

        log.debug(SEG.WASM, `[P2PSync] Roots differ, local=${localRoot.root.slice(0, 12)}... remote=${hello.root_hash?.slice(0, 12)}...`);

        // Phase 2: Exchange group hashes
        const localGroupHashes = syncMerkleGroupHashes();

        this.sendMessage({
            type: 'sync_group_hashes',
            groups: localGroupHashes,
        });

        const groupHashesMsg = await this.receiveMessage();
        if (groupHashesMsg.type !== 'sync_group_hashes') {
            throw new Error(`Expected sync_group_hashes, got ${groupHashesMsg.type}`);
        }

        const remoteGroupHashes = groupHashesMsg.groups || {};

        // Phase 3: Determine what we need
        const needGroupKeys: string[] = [];
        for (const [groupKey, remoteHash] of Object.entries(remoteGroupHashes)) {
            const localHash = localGroupHashes[groupKey];
            if (localHash !== remoteHash) {
                needGroupKeys.push(groupKey);
            }
        }

        this.sendMessage({
            type: 'sync_need',
            need: needGroupKeys,
        });

        // Phase 4: Receive what peer needs
        const needMsg = await this.receiveMessage();
        if (needMsg.type !== 'sync_need') {
            throw new Error(`Expected sync_need, got ${needMsg.type}`);
        }

        const peerNeeds = needMsg.need || [];

        // Phase 5: Send attestations peer needs
        const toSend: Record<string, any[]> = {};
        for (const groupKey of peerNeeds) {
            const ids = listAttestationIds().filter((id) => {
                const key = syncMerkleFindGroupKey(id);
                return key === groupKey;
            });

            toSend[groupKey] = ids.map((id) => {
                const as = getAttestation(id);
                if (!as) {
                    log.warn(SEG.WASM, `[P2PSync] Attestation ${id} disappeared during sync`);
                    return null;
                }
                return as;
            }).filter((a) => a !== null);

            sent += toSend[groupKey].length;
        }

        this.sendMessage({
            type: 'sync_attestations',
            attestations: toSend,
        });

        // Phase 6: Receive attestations we need
        const attestationsMsg = await this.receiveMessage();
        if (attestationsMsg.type !== 'sync_attestations') {
            throw new Error(`Expected sync_attestations, got ${attestationsMsg.type}`);
        }

        const receivedAttestations = attestationsMsg.attestations || {};
        for (const groupAtts of Object.values(receivedAttestations)) {
            for (const att of groupAtts) {
                putAttestation(att);
                received++;
            }
        }

        // Phase 7: Exchange done
        this.sendMessage({ type: 'sync_done', sent, received });
        await this.receiveMessage(); // recv peer's sync_done

        log.info(SEG.WASM, `[P2PSync] Reconciled: sent=${sent} received=${received}`);

        return { sent, received };
    }

    private sendMessage(msg: SyncMessage): void {
        this.peer.send(JSON.stringify(msg));
    }

    private async receiveMessage(): Promise<SyncMessage> {
        return new Promise((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error('P2P sync message timeout'));
            }, 30_000); // 30 second timeout

            this.peer.onMessage((data) => {
                clearTimeout(timeout);
                try {
                    const msg = JSON.parse(data) as SyncMessage;
                    resolve(msg);
                } catch (err) {
                    reject(new Error(`Failed to parse sync message: ${err}`));
                }
            });
        });
    }

    close(): void {
        this.peer.close();
    }
}
