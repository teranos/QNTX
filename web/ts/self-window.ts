/**
 * Self Diagnostic Window (⍟ i)
 *
 * Runtime introspection and diagnostic information.
 * Shows system state that is NOT configuration (am handles config).
 *
 * Displays:
 * - QNTX version, commit, build time
 * - Go version
 * - fuzzy-ax version + backend (rust/go optimization status)
 * - vidstream version + backend (ONNX availability)
 * - System capabilities snapshot
 *
 * TODO: This will be removed in favor of the self-glyph in test-glyphs.ts
 */

import { Window } from './components/window.ts';
import { formatBuildTime, tooltip } from './components/tooltip.ts';
import type { VersionMessage, SystemCapabilitiesMessage } from '../types/websocket';

interface SelfDiagnosticInfo {
    // Core version info
    version?: VersionMessage;
    // System capabilities
    capabilities?: SystemCapabilitiesMessage;
}

class SelfWindow {
    private window: Window;
    private info: SelfDiagnosticInfo = {};

    constructor() {
        this.window = new Window({
            id: 'self-window',
            title: '⍟ Self — System Diagnostic',
            width: '700px',
            height: 'auto',
            onShow: () => this.onShow()
        });

        this.render();
    }

    private onShow(): void {
        // Render current cached info
        this.render();
    }

    toggle(): void {
        this.window.toggle();
    }

    /**
     * Update version info from WebSocket
     */
    updateVersion(data: VersionMessage): void {
        this.info.version = data;
        this.render();
    }

    /**
     * Update system capabilities from WebSocket
     */
    updateCapabilities(data: SystemCapabilitiesMessage): void {
        this.info.capabilities = data;
        this.render();
    }

    private render(): void {
        const content = this.window.getContentElement();

        const version = this.info.version;
        const caps = this.info.capabilities;

        // If no data yet, show loading state
        if (!version && !caps) {
            content.innerHTML = `
                <div class="self-info-loading">
                    <p>Waiting for system info...</p>
                    <p class="self-info-hint">Info is broadcast on WebSocket connection.</p>
                </div>
            `;
            return;
        }

        const sections: string[] = [];

        // QNTX Server version section
        if (version) {
            const buildTimeFormatted = formatBuildTime(version.build_time) || version.build_time || 'unknown';
            const commitShort = version.commit?.substring(0, 7) || 'unknown';

            sections.push(`
                <div class="self-section">
                    <h3 class="self-section-title">QNTX Server</h3>
                    <div class="self-info-row">
                        <span class="self-info-label">Version:</span>
                        <span class="self-info-value">${version.version || 'unknown'}</span>
                    </div>
                    <div class="self-info-row">
                        <span class="self-info-label">Commit:</span>
                        <span class="self-info-value has-tooltip" data-tooltip="${version.commit || 'unknown'}">${commitShort}</span>
                    </div>
                    <div class="self-info-row">
                        <span class="self-info-label">Built:</span>
                        <span class="self-info-value">${buildTimeFormatted}</span>
                    </div>
                    ${version.go_version ? `
                    <div class="self-info-row">
                        <span class="self-info-label">Go:</span>
                        <span class="self-info-value">${version.go_version}</span>
                    </div>
                    ` : ''}
                </div>
            `);
        }

        // System Capabilities section
        if (caps) {
            const parserTooltip = caps.parser_optimized ?
                'Using WebAssembly parser for faster performance and browser compatibility. This enables offline operation and consistent parsing across all platforms.' :
                'Using Go native parser (fallback). Build with WASM support for better performance and browser compatibility.';

            const parserStatus = caps.parser_optimized ?
                `<span class="capability-optimized" title="${parserTooltip}">✓ qntx-core WASM ${caps.parser_size ? `(${caps.parser_size})` : ''}</span>` :
                `<span class="capability-degraded" title="${parserTooltip}">⚠ Go native parser</span>`;

            const fuzzyStatus = caps.fuzzy_optimized ?
                `<span class="capability-optimized" title="Using Rust-based fuzzy matching for better performance">✓ Optimized (Rust)</span>` :
                `<span class="capability-degraded" title="Using Go fallback for fuzzy matching">⚠ Fallback (Go)</span>`;

            const vidstreamStatus = caps.vidstream_optimized ?
                `<span class="capability-optimized" title="Video inference available via ONNX Runtime">✓ Available (ONNX)</span>` :
                `<span class="capability-degraded" title="Video inference unavailable - requires CGO build">⚠ Unavailable (requires CGO)</span>`;

            const storageStatus = caps.storage_optimized ?
                `<span class="capability-optimized" title="Using Rust-based SQLite for better performance">✓ Optimized (Rust)</span>` :
                `<span class="capability-degraded" title="Using Go SQLite driver (fallback)">⚠ Fallback (Go)</span>`;

            sections.push(`
                <div class="self-section">
                    <h3 class="self-section-title">System Capabilities</h3>
                    <div class="self-info-row">
                        <span class="self-info-label">parser:</span>
                        <span class="self-info-value">
                            ${caps.parser_version ? `v${caps.parser_version}` : ''}
                            ${parserStatus}
                        </span>
                    </div>
                    <div class="self-info-row">
                        <span class="self-info-label">fuzzy-ax:</span>
                        <span class="self-info-value">
                            ${caps.fuzzy_version ? `v${caps.fuzzy_version}` : 'unknown'}
                            ${fuzzyStatus}
                        </span>
                    </div>
                    <div class="self-info-row">
                        <span class="self-info-label">vidstream:</span>
                        <span class="self-info-value">
                            ${caps.vidstream_version ? `v${caps.vidstream_version}` : 'unknown'}
                            ${vidstreamStatus}
                        </span>
                    </div>
                    <div class="self-info-row">
                        <span class="self-info-label">storage:</span>
                        <span class="self-info-value">
                            ${caps.storage_version ? `v${caps.storage_version}` : 'unknown'}
                            ${storageStatus}
                        </span>
                    </div>
                </div>
            `);
        }

        content.innerHTML = `
            <div class="self-info">
                ${sections.join('\n')}
            </div>
        `;

        // Setup tooltips
        this.setupTooltips();
    }

    private setupTooltips(): void {
        const content = this.window.getContentElement();
        tooltip.attach(content, '.has-tooltip');
    }
}

export const selfWindow = new SelfWindow();
