/**
 * Test Glyphs - Demonstration of the glyph-primitive vision
 *
 * This file registers test glyphs to demonstrate the morphing behavior
 * where glyphs transform into windows and back.
 *
 * CURRENT STATE:
 * These are showcase glyphs demonstrating the core morphing mechanics.
 * The infrastructure is complete: single DOM element axiom, smooth animations,
 * proximity morphing, and window transformations all work.
 *
 * NEXT STEPS - The Migration Path:
 *
 * 1. GlyphSet - The Universal Container
 *    Instead of separate systems (windows, symbols, segments), we need a
 *    GlyphSet that holds all available glyphs. This becomes the single
 *    source of truth for all interactive visual elements in QNTX.
 *
 * 2. Symbol Palette → Glyph Migration
 *    The symbol palette (seg/sym system) should be the FIRST migration target.
 *    Each symbol becomes a glyph with:
 *    - Collapsed state: the symbol icon itself
 *    - Proximity state: symbol with label/description
 *    - Window state: full symbol details, relationships, attestations
 *
 * 3. Existing Windows → Glyph Migration
 *    Current windows (VidStream, Database, etc.) need to be converted to glyphs:
 *    - Add glyph registration with renderContent()
 *    - Remove old window creation code
 *    - Let the glyph system handle all morphing
 *
 * 4. Unified Interaction Model
 *    Once everything is a glyph:
 *    - Symbol palette becomes just another GlyphRun view
 *    - Windows are just expanded glyphs
 *    - Commands are glyphs that execute on click
 *    - Everything uses the same proximity/morphing behavior
 *
 * 5. Backend Alignment
 *    The glyph concept should eventually extend to the backend:
 *    - Attestations about glyphs
 *    - Glyph state persistence
 *    - Glyph relationships and dependencies
 *
 * The vision: Every visual element in QNTX is a glyph. They all morph
 * the same way, behave the same way, and are reasoned about the same way.
 * This creates conceptual clarity throughout the entire system.
 *
 * In the next session, we'll begin the real migration from seg/sym to glyphs,
 * starting with creating the GlyphSet infrastructure and converting the first
 * symbols into true glyphs.
 */

import { glyphRun } from './components/glyph/run';
import { createCanvasGlyph } from './components/glyph/canvas-glyph';
import { sendMessage } from './websocket';
import { DB } from '@generated/sym.js';
import { log, SEG } from './logger.ts';
import { formatBuildTime } from './components/tooltip.ts';
import type { VersionMessage, SystemCapabilitiesMessage } from '../types/websocket';

// Database stats state
let dbStatsElement: HTMLElement | null = null;
let dbStats: any = null;

// Self diagnostics state
let selfElement: HTMLElement | null = null;
let selfVersion: VersionMessage | null = null;
let selfCapabilities: SystemCapabilitiesMessage | null = null;

export function updateDatabaseStats(stats: any): void {
    dbStats = stats;
    if (dbStatsElement) {
        renderDbStats();
    }
}

export function updateSelfVersion(data: VersionMessage): void {
    selfVersion = data;
    if (selfElement) {
        renderSelf();
    }
}

export function updateSelfCapabilities(data: SystemCapabilitiesMessage): void {
    selfCapabilities = data;
    if (selfElement) {
        renderSelf();
    }
}

function renderDbStats(): void {
    if (!dbStatsElement) return;

    if (!dbStats) {
        dbStatsElement.innerHTML = '<div class="glyph-loading">Loading database statistics...</div>';
        return;
    }

    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    dbStatsElement.innerHTML = `
        <div class="glyph-content">
            <div class="glyph-row">
                <span class="glyph-label">Database Path:</span>
                <span class="glyph-value">${dbStats.path}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Storage Backend:</span>
                <span class="glyph-value">${storageBackend}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Total Attestations:</span>
                <span class="glyph-value">${dbStats.total_attestations.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Actors:</span>
                <span class="glyph-value">${dbStats.unique_actors.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Subjects:</span>
                <span class="glyph-value">${dbStats.unique_subjects.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Contexts:</span>
                <span class="glyph-value">${dbStats.unique_contexts.toLocaleString()}</span>
            </div>
        </div>
    `;
}

function renderSelf(): void {
    if (!selfElement) return;

    if (!selfVersion && !selfCapabilities) {
        selfElement.innerHTML = '<div class="glyph-loading">Waiting for system info...</div>';
        return;
    }

    const sections: string[] = [];

    // QNTX Server version section
    if (selfVersion) {
        const buildTimeFormatted = formatBuildTime(selfVersion.build_time) || selfVersion.build_time || 'unknown';
        const commitShort = selfVersion.commit?.substring(0, 7) || 'unknown';

        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">QNTX Server</h3>
                <div class="glyph-row">
                    <span class="glyph-label">Version:</span>
                    <span class="glyph-value">${selfVersion.version || 'unknown'}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Commit:</span>
                    <span class="glyph-value">${commitShort}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Built:</span>
                    <span class="glyph-value">${buildTimeFormatted}</span>
                </div>
                ${selfVersion.go_version ? `
                <div class="glyph-row">
                    <span class="glyph-label">Go:</span>
                    <span class="glyph-value">${selfVersion.go_version}</span>
                </div>
                ` : ''}
            </div>
        `);
    }

    // System Capabilities section
    if (selfCapabilities) {
        const caps = selfCapabilities;

        const parserStatus = caps.parser_optimized ?
            `<span style="color: #4ade80;">✓ qntx-core WASM ${caps.parser_size ? `(${caps.parser_size})` : ''}</span>` :
            `<span style="color: #fbbf24;">⚠ Go native parser</span>`;

        const fuzzyStatus = caps.fuzzy_optimized ?
            `<span style="color: #4ade80;">✓ Optimized (Rust)</span>` :
            `<span style="color: #fbbf24;">⚠ Fallback (Go)</span>`;

        const vidstreamStatus = caps.vidstream_optimized ?
            `<span style="color: #4ade80;">✓ Available (ONNX)</span>` :
            `<span style="color: #fbbf24;">⚠ Unavailable</span>`;

        const storageStatus = caps.storage_optimized ?
            `<span style="color: #4ade80;">✓ Optimized (Rust)</span>` :
            `<span style="color: #fbbf24;">⚠ Fallback (Go)</span>`;

        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">System Capabilities</h3>
                <div class="glyph-row">
                    <span class="glyph-label">parser:</span>
                    <span class="glyph-value">
                        ${caps.parser_version ? `v${caps.parser_version}` : ''}
                        ${parserStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">fuzzy-ax:</span>
                    <span class="glyph-value">
                        ${caps.fuzzy_version ? `v${caps.fuzzy_version}` : 'unknown'}
                        ${fuzzyStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">vidstream:</span>
                    <span class="glyph-value">
                        ${caps.vidstream_version ? `v${caps.vidstream_version}` : 'unknown'}
                        ${vidstreamStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">storage:</span>
                    <span class="glyph-value">
                        ${caps.storage_version ? `v${caps.storage_version}` : 'unknown'}
                        ${storageStatus}
                    </span>
                </div>
            </div>
        `);
    }

    selfElement.innerHTML = `
        <div class="glyph-content">
            ${sections.join('\n')}
        </div>
    `;
}

// Register test glyphs once DOM is ready
export function registerTestGlyphs(): void {
    // Canvas Glyph - Fractal container with spatial grid
    glyphRun.add(createCanvasGlyph());

    // Database Statistics Glyph
    glyphRun.add({
        id: 'database-glyph',
        title: `${DB} Database Statistics`,
        renderContent: () => {
            const content = document.createElement('div');
            dbStatsElement = content;
            sendMessage({ type: 'get_database_stats' });
            renderDbStats();
            return content;
        },
        initialWidth: '400px',
        initialHeight: '240px',
        defaultX: 100,
        defaultY: 100
    });

    // Self Diagnostics Glyph
    glyphRun.add({
        id: 'self-glyph',
        title: '⍟ Self',
        renderContent: () => {
            const content = document.createElement('div');
            selfElement = content;
            renderSelf();
            return content;
        },
        initialWidth: '450px',
        initialHeight: '320px'
    });

    // TODO: Replace console.log with proper logger (log.debug)
    log.debug(SEG.UI, 'Test glyphs registered:', {
        vidstream: 'VidStream monitoring',
        database: 'Database Statistics',
        self: 'Self diagnostics'
    });
}