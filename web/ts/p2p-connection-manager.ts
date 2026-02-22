/**
 * P2P Connection Manager
 *
 * Handles WebRTC signaling (SDP offer/answer exchange) for peer-to-peer sync.
 * Initial implementation uses manual copy-paste (invite links).
 */

import { log, SEG } from './logger';
import { WebRTCPeer } from './webrtc-peer';
import { P2PSync } from './p2p-sync';

export interface InviteLink {
    /** Base64-encoded SDP offer */
    offer: string;
    /** Version for future compatibility */
    version: number;
}

export interface P2PConnection {
    peer: WebRTCPeer;
    sync: P2PSync;
}

/**
 * Generate an invite link to share with remote peer.
 *
 * Returns a JSON string containing SDP offer. Peer pastes this to connect.
 */
export async function generateInviteLink(): Promise<string> {
    // For localhost testing, use empty ICE servers (local connection only)
    const peer = new WebRTCPeer({
        iceServers: window.location.hostname === 'localhost' ? [] : undefined,
    });
    const offer = await peer.createOffer();

    const inviteData: InviteLink = {
        offer: btoa(JSON.stringify(offer)),
        version: 1,
    };

    // Store peer in global map keyed by offer (so we can resume when answer arrives)
    pendingPeers.set(inviteData.offer, peer);

    return JSON.stringify(inviteData);
}

/**
 * Connect to a peer using their invite link.
 *
 * Takes invite link JSON, creates answer, returns answer JSON to paste back.
 */
export async function connectToInvite(inviteLinkJson: string): Promise<{ answerJson: string; connection: P2PConnection }> {
    const inviteData: InviteLink = JSON.parse(inviteLinkJson);

    if (inviteData.version !== 1) {
        throw new Error(`Unsupported invite version: ${inviteData.version}`);
    }

    const offer = JSON.parse(atob(inviteData.offer));

    const peer = new WebRTCPeer({
        iceServers: window.location.hostname === 'localhost' ? [] : undefined,
    });
    const answer = await peer.createAnswer(offer);

    const answerData = {
        answer: btoa(JSON.stringify(answer)),
        version: 1,
    };

    const sync = new P2PSync(peer);

    log.info(SEG.WS, '[P2P] Connected to invite, waiting for channel open');

    return {
        answerJson: JSON.stringify(answerData),
        connection: { peer, sync },
    };
}

/**
 * Complete connection (initiator receives answer from peer).
 *
 * Takes the answer JSON, completes WebRTC handshake, returns connection.
 */
export async function acceptAnswer(inviteLinkJson: string, answerJson: string): Promise<P2PConnection> {
    const inviteData: InviteLink = JSON.parse(inviteLinkJson);
    const answerData = JSON.parse(answerJson);

    if (answerData.version !== 1) {
        throw new Error(`Unsupported answer version: ${answerData.version}`);
    }

    const peer = pendingPeers.get(inviteData.offer);
    if (!peer) {
        throw new Error('No pending peer found for this invite');
    }

    const answer = JSON.parse(atob(answerData.answer));
    await peer.acceptAnswer(answer);

    const sync = new P2PSync(peer);

    log.info(SEG.WS, '[P2P] Answer accepted, waiting for channel open');

    return { peer, sync };
}

// Pending peers waiting for answer (keyed by base64 offer)
const pendingPeers = new Map<string, WebRTCPeer>();
