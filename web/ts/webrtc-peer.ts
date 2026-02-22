/**
 * WebRTC Peer Connection for Browser-to-Browser Sync
 *
 * Provides a WebSocket-like interface over WebRTC data channels.
 * Reuses the same Merkle sync protocol as server sync — just different transport.
 */

import { log, SEG } from './logger';

export interface PeerConnectionConfig {
    /** ICE servers for NAT traversal (STUN/TURN) */
    iceServers?: RTCIceServer[];
    /** Timeout for connection establishment (ms) */
    connectionTimeout?: number;
}

const DEFAULT_ICE_SERVERS: RTCIceServer[] = [
    // Google's public STUN server
    { urls: 'stun:stun.l.google.com:19302' },
    { urls: 'stun:stun1.l.google.com:19302' },
];

const DEFAULT_CONNECTION_TIMEOUT = 30_000; // 30 seconds

/**
 * WebRTC peer connection wrapper with WebSocket-like interface.
 *
 * Creates a reliable, ordered data channel for sync protocol messages.
 * Handles ICE candidate exchange, connection state, and message framing.
 */
export class WebRTCPeer {
    private pc: RTCPeerConnection;
    private channel: RTCDataChannel | null = null;
    private onMessageCallback: ((data: string) => void) | null = null;
    private onCloseCallback: (() => void) | null = null;
    private pendingMessages: string[] = [];
    private connectionTimeout: number;

    constructor(config: PeerConnectionConfig = {}) {
        this.connectionTimeout = config.connectionTimeout ?? DEFAULT_CONNECTION_TIMEOUT;

        this.pc = new RTCPeerConnection({
            iceServers: config.iceServers ?? DEFAULT_ICE_SERVERS,
        });

        // Log connection state changes
        this.pc.onconnectionstatechange = () => {
            log.info(SEG.WS, `[WebRTC] Connection state: ${this.pc.connectionState}`);

            if (this.pc.connectionState === 'failed' || this.pc.connectionState === 'closed') {
                this.onCloseCallback?.();
            }
        };

        this.pc.oniceconnectionstatechange = () => {
            log.info(SEG.WS, `[WebRTC] ICE connection state: ${this.pc.iceConnectionState}`);
        };

        this.pc.onicegatheringstatechange = () => {
            log.info(SEG.WS, `[WebRTC] ICE gathering state: ${this.pc.iceGatheringState}`);
        };

        this.pc.onicecandidate = (event) => {
            if (event.candidate) {
                log.debug(SEG.WS, `[WebRTC] ICE candidate: ${event.candidate.candidate}`);
            } else {
                log.info(SEG.WS, '[WebRTC] ICE gathering complete (null candidate)');
            }
        };
    }

    /**
     * Create offer (initiating peer).
     * Returns SDP offer to be sent to remote peer via signaling channel.
     */
    async createOffer(): Promise<RTCSessionDescriptionInit> {
        // Create data channel (initiator creates, responder receives)
        this.channel = this.pc.createDataChannel('qntx-sync', {
            ordered: true,    // Preserve message order
            maxRetransmits: undefined, // Reliable delivery
        });
        this.setupDataChannel(this.channel);

        const offer = await this.pc.createOffer();
        await this.pc.setLocalDescription(offer);

        // Wait for ICE gathering to complete
        await this.waitForIceGathering();

        return this.pc.localDescription!;
    }

    /**
     * Create answer (responding peer).
     * Takes remote offer, returns SDP answer.
     */
    async createAnswer(remoteOffer: RTCSessionDescriptionInit): Promise<RTCSessionDescriptionInit> {
        await this.pc.setRemoteDescription(remoteOffer);

        // Set up handler for incoming data channel
        this.pc.ondatachannel = (event) => {
            this.channel = event.channel;
            this.setupDataChannel(this.channel);
        };

        const answer = await this.pc.createAnswer();
        await this.pc.setLocalDescription(answer);

        await this.waitForIceGathering();

        return this.pc.localDescription!;
    }

    /**
     * Complete connection (initiator receives answer).
     */
    async acceptAnswer(remoteAnswer: RTCSessionDescriptionInit): Promise<void> {
        await this.pc.setRemoteDescription(remoteAnswer);
    }

    /**
     * Send message over data channel (like WebSocket.send).
     */
    send(data: string): void {
        if (!this.channel || this.channel.readyState !== 'open') {
            // Queue message if channel not ready yet
            this.pendingMessages.push(data);
            return;
        }

        this.channel.send(data);
    }

    /**
     * Set message handler (like WebSocket.onmessage).
     */
    onMessage(callback: (data: string) => void): void {
        this.onMessageCallback = callback;
    }

    /**
     * Set close handler (like WebSocket.onclose).
     */
    onClose(callback: () => void): void {
        this.onCloseCallback = callback;
    }

    /**
     * Close connection.
     */
    close(): void {
        if (this.channel) {
            this.channel.close();
        }
        this.pc.close();
    }

    /**
     * Wait for channel to be open.
     */
    async waitForOpen(): Promise<void> {
        if (this.channel?.readyState === 'open') {
            return;
        }

        return new Promise((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error(`WebRTC channel open timeout after ${this.connectionTimeout}ms`));
            }, this.connectionTimeout);

            const checkOpen = () => {
                if (this.channel?.readyState === 'open') {
                    clearTimeout(timeout);
                    resolve();
                }
            };

            // Poll every 100ms
            const interval = setInterval(checkOpen, 100);
            setTimeout(() => clearInterval(interval), this.connectionTimeout);
        });
    }

    private setupDataChannel(channel: RTCDataChannel): void {
        channel.onopen = () => {
            log.debug(SEG.WS, '[WebRTC] Data channel opened');

            // Flush pending messages
            while (this.pendingMessages.length > 0) {
                const msg = this.pendingMessages.shift()!;
                channel.send(msg);
            }
        };

        channel.onmessage = (event) => {
            this.onMessageCallback?.(event.data);
        };

        channel.onclose = () => {
            log.debug(SEG.WS, '[WebRTC] Data channel closed');
            this.onCloseCallback?.();
        };

        channel.onerror = (error) => {
            log.error(SEG.WS, '[WebRTC] Data channel error:', error);
        };
    }

    private async waitForIceGathering(): Promise<void> {
        // If already complete, return immediately
        if (this.pc.iceGatheringState === 'complete') {
            return;
        }

        return new Promise((resolve) => {
            const timeout = setTimeout(() => {
                log.warn(SEG.WS, '[WebRTC] ICE gathering timeout, proceeding with partial candidates');
                resolve();
            }, 5000); // 5 second timeout for ICE gathering

            this.pc.onicegatheringstatechange = () => {
                if (this.pc.iceGatheringState === 'complete') {
                    clearTimeout(timeout);
                    resolve();
                }
            };
        });
    }
}
