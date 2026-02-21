/**
 * P2P Sync Window Glyph
 *
 * UI for peer-to-peer browser sync over WebRTC.
 * Generate invite links, connect to peers, trigger sync.
 */

import type { Glyph } from './glyph';
import { glyphRun } from './run';
import { generateInviteLink, connectToInvite, acceptAnswer, type P2PConnection } from '../../p2p-connection-manager';
import { log, SEG } from '../../logger';

const P2P_GLYPH_ID = 'p2p-sync';

let activeConnection: P2PConnection | null = null;
let pendingInviteLink: string | null = null;

function renderP2PContent(): HTMLElement {
    const el = document.createElement('div');
    el.className = 'p2p-sync-content';
    el.style.cssText = `
        padding: 20px;
        font-family: monospace;
        font-size: 13px;
        line-height: 1.6;
        color: var(--text-on-dark);
        overflow-y: auto;
        height: 100%;
    `;

    el.innerHTML = `
        <h3 style="margin: 0 0 16px 0; font-size: 16px; color: #60a5fa;">
            Peer-to-Peer Sync
        </h3>

        <div class="p2p-section" style="margin-bottom: 20px;">
            <h4 style="margin: 0 0 8px 0; font-size: 14px;">
                1. Initiate Connection
            </h4>
            <button class="generate-invite-btn" style="
                padding: 8px 16px;
                background: #3b82f6;
                color: white;
                border: none;
                border-radius: 4px;
                cursor: pointer;
                font-family: monospace;
            ">Generate Invite Link</button>
            <div class="invite-link-display" style="
                margin-top: 12px;
                padding: 12px;
                background: rgba(0, 0, 0, 0.3);
                border-radius: 4px;
                display: none;
            ">
                <div style="margin-bottom: 8px; color: #9ca3af;">
                    Share this with your peer:
                </div>
                <textarea class="invite-link-text" readonly style="
                    width: 100%;
                    height: 80px;
                    background: #000;
                    color: #0f0;
                    border: 1px solid #333;
                    border-radius: 4px;
                    padding: 8px;
                    font-family: monospace;
                    font-size: 11px;
                    resize: none;
                "></textarea>
                <button class="copy-invite-btn" style="
                    margin-top: 8px;
                    padding: 6px 12px;
                    background: #10b981;
                    color: white;
                    border: none;
                    border-radius: 4px;
                    cursor: pointer;
                    font-family: monospace;
                    font-size: 12px;
                ">Copy to Clipboard</button>
            </div>
        </div>

        <div class="p2p-section" style="margin-bottom: 20px;">
            <h4 style="margin: 0 0 8px 0; font-size: 14px;">
                2. Connect to Peer
            </h4>
            <textarea class="peer-invite-input" placeholder="Paste peer's invite link here..." style="
                width: 100%;
                height: 80px;
                background: rgba(0, 0, 0, 0.3);
                color: var(--text-on-dark);
                border: 1px solid #444;
                border-radius: 4px;
                padding: 8px;
                font-family: monospace;
                font-size: 11px;
                margin-bottom: 8px;
            "></textarea>
            <button class="connect-to-peer-btn" style="
                padding: 8px 16px;
                background: #8b5cf6;
                color: white;
                border: none;
                border-radius: 4px;
                cursor: pointer;
                font-family: monospace;
            ">Connect</button>
            <div class="answer-display" style="
                margin-top: 12px;
                padding: 12px;
                background: rgba(0, 0, 0, 0.3);
                border-radius: 4px;
                display: none;
            ">
                <div style="margin-bottom: 8px; color: #9ca3af;">
                    Send this answer back to your peer:
                </div>
                <textarea class="answer-text" readonly style="
                    width: 100%;
                    height: 80px;
                    background: #000;
                    color: #0f0;
                    border: 1px solid #333;
                    border-radius: 4px;
                    padding: 8px;
                    font-family: monospace;
                    font-size: 11px;
                    resize: none;
                "></textarea>
                <button class="copy-answer-btn" style="
                    margin-top: 8px;
                    padding: 6px 12px;
                    background: #10b981;
                    color: white;
                    border: none;
                    border-radius: 4px;
                    cursor: pointer;
                    font-family: monospace;
                    font-size: 12px;
                ">Copy to Clipboard</button>
            </div>
        </div>

        <div class="answer-input-section" style="margin-bottom: 20px; display: none;">
            <h4 style="margin: 0 0 8px 0; font-size: 14px;">
                3. Complete Connection
            </h4>
            <textarea class="peer-answer-input" placeholder="Paste peer's answer here..." style="
                width: 100%;
                height: 80px;
                background: rgba(0, 0, 0, 0.3);
                color: var(--text-on-dark);
                border: 1px solid #444;
                border-radius: 4px;
                padding: 8px;
                font-family: monospace;
                font-size: 11px;
                margin-bottom: 8px;
            "></textarea>
            <button class="accept-answer-btn" style="
                padding: 8px 16px;
                background: #f59e0b;
                color: white;
                border: none;
                border-radius: 4px;
                cursor: pointer;
                font-family: monospace;
            ">Complete Connection</button>
        </div>

        <div class="connection-status" style="
            margin-bottom: 20px;
            padding: 12px;
            background: rgba(0, 0, 0, 0.3);
            border-radius: 4px;
            display: none;
        ">
            <div style="color: #10b981; margin-bottom: 8px;">
                ✓ Connected to peer
            </div>
            <button class="sync-now-btn" style="
                padding: 8px 16px;
                background: #ef4444;
                color: white;
                border: none;
                border-radius: 4px;
                cursor: pointer;
                font-family: monospace;
                margin-right: 8px;
            ">Sync Now</button>
            <button class="disconnect-btn" style="
                padding: 8px 16px;
                background: #6b7280;
                color: white;
                border: none;
                border-radius: 4px;
                cursor: pointer;
                font-family: monospace;
            ">Disconnect</button>
        </div>

        <div class="sync-results" style="
            padding: 12px;
            background: rgba(0, 0, 0, 0.3);
            border-radius: 4px;
            display: none;
            color: #9ca3af;
        "></div>
    `;

    // Event listeners
    const generateBtn = el.querySelector('.generate-invite-btn') as HTMLButtonElement;
    const copyInviteBtn = el.querySelector('.copy-invite-btn') as HTMLButtonElement;
    const connectBtn = el.querySelector('.connect-to-peer-btn') as HTMLButtonElement;
    const copyAnswerBtn = el.querySelector('.copy-answer-btn') as HTMLButtonElement;
    const acceptAnswerBtn = el.querySelector('.accept-answer-btn') as HTMLButtonElement;
    const syncBtn = el.querySelector('.sync-now-btn') as HTMLButtonElement;
    const disconnectBtn = el.querySelector('.disconnect-btn') as HTMLButtonElement;

    generateBtn.onclick = async () => {
        try {
            pendingInviteLink = await generateInviteLink();
            const display = el.querySelector('.invite-link-display') as HTMLElement;
            const textarea = el.querySelector('.invite-link-text') as HTMLTextAreaElement;
            textarea.value = pendingInviteLink;
            display.style.display = 'block';

            const answerSection = el.querySelector('.answer-input-section') as HTMLElement;
            answerSection.style.display = 'block';

            log.info(SEG.WS, '[P2P] Invite link generated');
        } catch (err) {
            showError(el, `Failed to generate invite: ${err}`);
        }
    };

    copyInviteBtn.onclick = () => {
        const textarea = el.querySelector('.invite-link-text') as HTMLTextAreaElement;
        navigator.clipboard.writeText(textarea.value);
        log.info(SEG.WS, '[P2P] Invite link copied');
    };

    connectBtn.onclick = async () => {
        try {
            const input = el.querySelector('.peer-invite-input') as HTMLTextAreaElement;
            const inviteJson = input.value.trim();
            if (!inviteJson) return;

            const { answerJson, connection } = await connectToInvite(inviteJson);
            activeConnection = connection;

            const answerDisplay = el.querySelector('.answer-display') as HTMLElement;
            const answerText = el.querySelector('.answer-text') as HTMLTextAreaElement;
            answerText.value = answerJson;
            answerDisplay.style.display = 'block';

            showConnected(el);
            log.info(SEG.WS, '[P2P] Connected to peer invite');
        } catch (err) {
            showError(el, `Failed to connect: ${err}`);
        }
    };

    copyAnswerBtn.onclick = () => {
        const textarea = el.querySelector('.answer-text') as HTMLTextAreaElement;
        navigator.clipboard.writeText(textarea.value);
        log.info(SEG.WS, '[P2P] Answer copied');
    };

    acceptAnswerBtn.onclick = async () => {
        try {
            if (!pendingInviteLink) {
                showError(el, 'No invite link found');
                return;
            }

            const input = el.querySelector('.peer-answer-input') as HTMLTextAreaElement;
            const answerJson = input.value.trim();
            if (!answerJson) return;

            activeConnection = await acceptAnswer(pendingInviteLink, answerJson);
            showConnected(el);
            log.info(SEG.WS, '[P2P] Connection completed');
        } catch (err) {
            showError(el, `Failed to accept answer: ${err}`);
        }
    };

    syncBtn.onclick = async () => {
        if (!activeConnection) return;

        try {
            const resultsDiv = el.querySelector('.sync-results') as HTMLElement;
            resultsDiv.textContent = 'Syncing...';
            resultsDiv.style.display = 'block';
            resultsDiv.style.color = '#9ca3af';

            const { sent, received } = await activeConnection.sync.reconcile();

            resultsDiv.textContent = `Sync complete: sent=${sent}, received=${received}`;
            log.info(SEG.WS, `[P2P] Sync complete: sent=${sent} received=${received}`);
        } catch (err) {
            showError(el, `Sync failed: ${err}`);
        }
    };

    disconnectBtn.onclick = () => {
        if (activeConnection) {
            activeConnection.peer.close();
            activeConnection = null;
        }
        const statusDiv = el.querySelector('.connection-status') as HTMLElement;
        statusDiv.style.display = 'none';
        log.info(SEG.WS, '[P2P] Disconnected');
    };

    return el;
}

function showConnected(el: HTMLElement): void {
    const statusDiv = el.querySelector('.connection-status') as HTMLElement;
    statusDiv.style.display = 'block';
}

function showError(el: HTMLElement, message: string): void {
    const resultsDiv = el.querySelector('.sync-results') as HTMLElement;
    resultsDiv.textContent = `Error: ${message}`;
    resultsDiv.style.display = 'block';
    resultsDiv.style.color = '#ef4444';
    log.error(SEG.WS, `[P2P] ${message}`);
}

/**
 * Open the P2P sync window glyph.
 * Call this from the UI (e.g., button in system drawer).
 */
export function openP2PSyncWindow(): void {
    const glyph: Glyph = {
        id: P2P_GLYPH_ID,
        title: 'P2P Sync',
        renderContent: renderP2PContent,
        initialWidth: '600px',
        initialHeight: '700px',
        onClose: () => {
            log.debug(SEG.GLYPH, '[P2PSync] Window closed');
        },
    };

    glyphRun.add(glyph);
    glyphRun.openGlyph(P2P_GLYPH_ID);
}
