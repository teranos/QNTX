/**
 * OpenClaw Canvas — Live workspace observability
 *
 * A fullscreen canvas glyph (lives in the tray like the main canvas) that
 * renders each OpenClaw workspace file as a result-glyph-style card on the
 * spatial grid. Each card: dark terminal background, monospace, header bar
 * with filename and content SHA. Files update live via polling.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import {
    fetchSnapshot,
    pollSnapshot,
    type OpenClawSnapshot,
} from '../../api/openclaw';

/** Bootstrap file display order. */
const FILE_ORDER = [
    'AGENTS.md',
    'SOUL.md',
    'IDENTITY.md',
    'USER.md',
    'MEMORY.md',
    'TOOLS.md',
    'HEARTBEAT.md',
    'BOOT.md',
    'BOOTSTRAP.md',
];

/** Card layout. */
const CARD_WIDTH = 420;
const CARD_HEIGHT = 320;
const CARD_GAP = 24;
const GRID_PADDING = 32;
const COLS = 3;

/** Poll interval. */
const POLL_INTERVAL_MS = 3000;

/**
 * Create the OpenClaw canvas glyph for the tray.
 */
export function createOpenClawGlyph(): Glyph {
    return {
        id: 'openclaw-canvas',
        title: 'OpenClaw',
        manifestationType: 'fullscreen',

        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'openclaw-canvas';
            container.style.width = '100%';
            container.style.height = '100%';
            container.style.position = 'relative';
            container.style.overflow = 'auto';
            container.style.backgroundColor = 'var(--bg-dark-hover)';
            container.style.outline = 'none';

            // Grid overlay (matches main canvas)
            const gridOverlay = document.createElement('div');
            gridOverlay.className = 'canvas-grid-overlay';
            container.appendChild(gridOverlay);

            // Status indicator (top-right, inline styles like result-glyph)
            const status = document.createElement('div');
            status.style.position = 'fixed';
            status.style.top = '8px';
            status.style.right = '48px';
            status.style.padding = '4px 10px';
            status.style.fontSize = '10px';
            status.style.fontFamily = 'monospace';
            status.style.borderRadius = '4px';
            status.style.backgroundColor = 'var(--bg-secondary)';
            status.style.border = '1px solid var(--border-color)';
            status.style.color = 'var(--text-tertiary)';
            status.style.zIndex = '10';
            status.style.pointerEvents = 'none';
            status.textContent = 'Connecting...';
            container.appendChild(status);

            // Track cards and their content SHAs
            const cards = new Map<string, HTMLElement>();
            const outputElements = new Map<string, HTMLElement>();
            const shaCache = new Map<string, string>();

            function cardPosition(idx: number): { x: number; y: number } {
                const col = idx % COLS;
                const row = Math.floor(idx / COLS);
                return {
                    x: GRID_PADDING + col * (CARD_WIDTH + CARD_GAP),
                    y: GRID_PADDING + row * (CARD_HEIGHT + CARD_GAP),
                };
            }

            function ensureCard(filename: string, idx: number): void {
                if (cards.has(filename)) return;

                const { x, y } = cardPosition(idx);

                // Result-glyph-style card
                const card = document.createElement('div');
                card.className = 'openclaw-card';
                card.style.position = 'absolute';
                card.style.left = `${x}px`;
                card.style.top = `${y}px`;
                card.style.width = `${CARD_WIDTH}px`;
                card.style.height = `${CARD_HEIGHT}px`;
                card.style.display = 'flex';
                card.style.flexDirection = 'column';
                card.style.borderRadius = '0 0 4px 4px';
                card.style.border = '1px solid var(--border-on-dark)';
                card.style.overflow = 'hidden';

                // Header (result-glyph style)
                const header = document.createElement('div');
                header.style.padding = '4px 8px';
                header.style.backgroundColor = 'var(--bg-tertiary)';
                header.style.borderBottom = '1px solid var(--border-color)';
                header.style.display = 'flex';
                header.style.alignItems = 'center';
                header.style.justifyContent = 'space-between';
                header.style.fontSize = '11px';
                header.style.color = 'var(--text-secondary)';
                header.style.flexShrink = '0';

                const nameLabel = document.createElement('span');
                nameLabel.textContent = filename;
                nameLabel.style.fontWeight = '600';
                header.appendChild(nameLabel);

                const shaLabel = document.createElement('span');
                shaLabel.className = 'openclaw-card-sha';
                shaLabel.style.fontFamily = 'monospace';
                shaLabel.style.fontSize = '10px';
                shaLabel.style.color = 'var(--text-tertiary)';
                header.appendChild(shaLabel);

                card.appendChild(header);

                // Output body (result-glyph style: dark bg, monospace, pre-wrap)
                const output = document.createElement('div');
                output.style.flex = '1';
                output.style.overflow = 'auto';
                output.style.padding = '8px';
                output.style.fontFamily = 'monospace';
                output.style.fontSize = '12px';
                output.style.whiteSpace = 'pre-wrap';
                output.style.wordBreak = 'break-word';
                output.style.backgroundColor = 'rgba(10, 10, 10, 0.85)';
                output.style.color = 'var(--text-on-dark)';
                output.style.lineHeight = '1.4';
                card.appendChild(output);

                container.appendChild(card);
                cards.set(filename, card);
                outputElements.set(filename, output);
            }

            function updateCard(filename: string, content: string, sha: string, exists: boolean): void {
                const output = outputElements.get(filename);
                if (!output) return;

                // Skip if unchanged
                if (shaCache.get(filename) === sha) return;
                shaCache.set(filename, sha);

                // Update SHA label
                const card = cards.get(filename);
                const shaLabel = card?.querySelector('.openclaw-card-sha') as HTMLElement | null;
                if (shaLabel) {
                    shaLabel.textContent = exists ? sha.substring(0, 8) : '';
                }

                if (!exists) {
                    output.textContent = '(file does not exist)';
                    output.style.color = 'var(--text-secondary)';
                    output.style.fontStyle = 'italic';
                    return;
                }

                output.style.color = 'var(--text-on-dark)';
                output.style.fontStyle = '';
                output.textContent = content;
            }

            function onSnapshot(snapshot: OpenClawSnapshot): void {
                let idx = 0;

                // Bootstrap files
                for (const name of FILE_ORDER) {
                    const file = snapshot.bootstrap_files[name];
                    if (!file) continue;
                    ensureCard(name, idx);
                    updateCard(name, file.content, file.content_sha, file.exists);
                    idx++;
                }

                // Daily memories (most recent 6)
                for (const mem of snapshot.daily_memories.slice(0, 6)) {
                    const filename = `memory/${mem.date}.md`;
                    ensureCard(filename, idx);
                    updateCard(filename, mem.content, mem.content_sha, true);
                    idx++;
                }

                // Status
                const bootstrapCount = Object.values(snapshot.bootstrap_files)
                    .filter(f => f.exists).length;
                const date = new Date(snapshot.taken_at * 1000);
                const time = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
                status.textContent = `${bootstrapCount} files \u00B7 ${snapshot.daily_memories.length} memories \u00B7 ${time}`;
                status.style.color = '#4ade80';
            }

            // Start polling
            const stopPolling = pollSnapshot(POLL_INTERVAL_MS, onSnapshot);

            // Immediate first fetch
            void fetchSnapshot().then(snap => {
                if (snap) onSnapshot(snap);
            });

            // Cleanup on removal
            const observer = new MutationObserver((mutations) => {
                for (const mutation of mutations) {
                    for (const node of mutation.removedNodes) {
                        if (node === container || (node instanceof HTMLElement && node.contains(container))) {
                            stopPolling();
                            observer.disconnect();
                            log.debug(SEG.UI, '[OpenClaw] Canvas removed, polling stopped');
                            return;
                        }
                    }
                }
            });

            requestAnimationFrame(() => {
                if (container.parentElement) {
                    observer.observe(container.parentElement, { childList: true });
                }
            });

            return container;
        },
    };
}
