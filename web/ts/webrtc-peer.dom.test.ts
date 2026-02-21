import { describe, it, expect } from 'bun:test';
import { WebRTCPeer } from './webrtc-peer';

describe('WebRTCPeer', () => {
    it('establishes connection and exchanges messages', async () => {
        // Peer A (initiator)
        const peerA = new WebRTCPeer();
        const offer = await peerA.createOffer();

        // Peer B (responder)
        const peerB = new WebRTCPeer();
        const answer = await peerB.createAnswer(offer);

        // Complete handshake
        await peerA.acceptAnswer(answer);

        // Wait for channels to open
        await Promise.all([peerA.waitForOpen(), peerB.waitForOpen()]);

        // Exchange messages
        const messagesA: string[] = [];
        const messagesB: string[] = [];

        peerA.onMessage((msg) => messagesA.push(msg));
        peerB.onMessage((msg) => messagesB.push(msg));

        peerA.send('Hello from A');
        peerB.send('Hello from B');

        // Wait a bit for messages to arrive
        await new Promise((resolve) => setTimeout(resolve, 100));

        expect(messagesB).toContain('Hello from A');
        expect(messagesA).toContain('Hello from B');

        peerA.close();
        peerB.close();
    });

    it('handles connection timeout', async () => {
        const peer = new WebRTCPeer({ connectionTimeout: 100 });

        // Create offer but don't complete handshake
        await peer.createOffer();

        // Should timeout waiting for open
        await expect(peer.waitForOpen()).rejects.toThrow('timeout');

        peer.close();
    });
});
