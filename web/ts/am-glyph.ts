/**
 * ≡ — what the node is, and what it was told to be.
 *
 * `sym/symbols.go` calls `am` "Configuration — System settings and state".
 * The node's build, the backends it was compiled against, the key it signs
 * with and the row its status line draws are all that: what the node is. They
 * sat on ⍟ because ⍟ was the only glyph there, and a person opening ⍟ to see
 * themselves read the node's build time first.
 *
 * The settings with their sources do not fit in a window, so they are ≡'s
 * panel and are opened from here.
 */

import { apiFetch } from './client';
import { AM } from './sym';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { formatBuildTime } from './components/tooltip.ts';
import { createGhostButton } from './components/button.ts';
import type { StatusItem } from './brow.ts';
import type { VersionMessage, SystemCapabilitiesMessage } from '../types/websocket';

// What the node has said about itself. Null is nothing asked yet, which draws
// no section rather than an empty one.
let amElement: HTMLElement | null = null;
let amVersion: VersionMessage | null = null;
let amCapabilities: SystemCapabilitiesMessage | null = null;
let amNodeDID: string | null = null;
let amStatusRow: StatusItem[] = [];

/** The build, pushed on the connect frame. */
export function updateAmVersion(data: VersionMessage): void {
    amVersion = data;
    if (amElement) renderAm();
}

/** The backends, pushed on connect and asked for at /am/syscap. */
export function updateAmCapabilities(data: SystemCapabilitiesMessage): void {
    amCapabilities = data;
    if (amElement) renderAm();
}

// The node's did:key, served publicly at /.well-known/did.json. It is the
// anchor ADR-010 says everything else references, so ≡ states it rather than
// leaving the node anonymous to its own operator.
async function loadNodeDID(): Promise<void> {
    try {
        const response = await apiFetch('/.well-known/did.json');
        if (!response.ok) {
            log.warn(SEG.SELF, `[am] DID document unavailable: ${response.status} ${response.statusText}`);
            return;
        }
        const doc = await response.json();
        amNodeDID = typeof doc?.id === 'string' ? doc.id : null;
        if (amElement) renderAm();
    } catch (error: unknown) {
        log.warn(SEG.SELF, `[am] DID document fetch failed: ${error instanceof Error ? error.message : String(error)}`);
    }
}

// The socket pushes the backends on connect, and a caller without one has no
// other way to them. Asking is what makes ≡ drawable before a socket exists.
async function loadSyscap(): Promise<void> {
    if (amCapabilities) return;
    try {
        const response = await apiFetch('/am/syscap');
        if (!response.ok) {
            log.warn(SEG.SELF, `[am] /am/syscap answered ${response.status} ${response.statusText}`);
            return;
        }
        amCapabilities = await response.json() as SystemCapabilitiesMessage;
        if (amElement) renderAm();
    } catch (error: unknown) {
        log.warn(SEG.SELF, `[am] /am/syscap fetch failed: ${error instanceof Error ? error.message : String(error)}`);
    }
}

// The same row the brow draws, on the glyph whose subject it is.
async function loadStatusRow(): Promise<void> {
    try {
        const response = await apiFetch('/am/statusline?format=json');
        if (!response.ok) {
            log.warn(SEG.SELF, `[am] /am/statusline answered ${response.status} ${response.statusText}`);
            return;
        }
        const body = await response.json();
        amStatusRow = Array.isArray(body?.items) ? body.items as StatusItem[] : [];
        if (amElement) renderAm();
    } catch (error: unknown) {
        log.warn(SEG.SELF, `[am] /am/statusline fetch failed: ${error instanceof Error ? error.message : String(error)}`);
    }
}

function row(label: string, value: string): string {
    return `
        <div class="glyph-row">
            <span class="glyph-label">${escapeHtml(label)}</span>
            <span class="glyph-value">${value}</span>
        </div>
    `;
}

function renderAm(): void {
    if (!amElement) return;

    if (!amVersion && !amCapabilities && !amNodeDID) {
        amElement.innerHTML = '<div class="glyph-loading">Waiting for the node to say what it is...</div>';
        return;
    }

    const sections: string[] = [];

    if (amVersion) {
        const built = formatBuildTime(amVersion.build_time) || amVersion.build_time || 'unknown';
        const commit = amVersion.commit?.substring(0, 7) || 'unknown';
        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">QNTX Server</h3>
                ${row('Version:', escapeHtml(amVersion.version || 'unknown'))}
                ${row('Commit:', escapeHtml(commit))}
                ${row('Built:', escapeHtml(built))}
                ${amVersion.go_version ? row('Go:', escapeHtml(amVersion.go_version)) : ''}
            </div>
        `);
    }

    if (amCapabilities) {
        const caps = amCapabilities;
        const parser = caps.parser_optimized
            ? `<span class="glyph-well">✓ ats WASM ${caps.parser_size ? `(${escapeHtml(caps.parser_size)})` : ''}</span>`
            : '<span class="glyph-unwell">⚠ Go native parser</span>';
        const storage = caps.storage_optimized
            ? '<span class="glyph-well">✓ Optimized (Rust)</span>'
            : '<span class="glyph-unwell">⚠ Fallback (Go)</span>';
        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">System Capabilities</h3>
                ${row('parser:', `${caps.parser_version ? `v${escapeHtml(caps.parser_version)} ` : ''}${parser}`)}
                ${row('storage:', storage)}
            </div>
        `);
    }

    // The node signs every attestation with this key, so a reader elsewhere
    // verifies against exactly this string.
    if (amNodeDID) {
        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">Identity</h3>
                ${row('Node DID:', `<span class="glyph-did">${escapeHtml(amNodeDID)}</span>`)}
            </div>
        `);
    }

    if (amStatusRow.length > 0) {
        const items = amStatusRow.map((item) => {
            const state = item.glyph === '+' ? 'glyph-well' : 'glyph-unwell';
            const note = item.note ? ` <span class="glyph-note">${escapeHtml(item.note)}</span>` : '';
            return `<span class="${state}">${escapeHtml(item.glyph)} ${escapeHtml(item.name)}${note}</span>`;
        }).join('');
        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">Status line</h3>
                <div class="glyph-status-row">${items}</div>
            </div>
        `);
    }

    amElement.innerHTML = `
        <div class="glyph-content">
            ${sections.join('\n')}
        </div>
    `;

    // The settings with their sources are ≡'s too, and a tree that scrolls is
    // a panel rather than a card.
    const actions = document.createElement('div');
    actions.className = 'glyph-actions';
    const settings = createGhostButton(` Settings`, async () => {
        const { showConfig } = await import('./config-panel.ts');
        showConfig();
    });
    actions.appendChild(settings.element);
    amElement.appendChild(actions);
}

/** ≡ in the tray: a window, the same form as ⍟. */
export function createAmGlyph() {
    return {
        id: 'am-glyph',
        title: 'am',
        symbol: AM,
        renderContent: () => {
            const content = document.createElement('div');
            amElement = content;
            renderAm();
            if (!amNodeDID) void loadNodeDID();
            if (!amCapabilities) void loadSyscap();
            void loadStatusRow();
            return content;
        },
        initialWidth: '450px',
        initialHeight: '360px',
    };
}
