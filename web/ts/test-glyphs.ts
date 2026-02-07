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

// Database stats state
let dbStatsElement: HTMLElement | null = null;
let dbStats: any = null;

export function updateDatabaseStats(stats: any): void {
    dbStats = stats;
    if (dbStatsElement) {
        renderDbStats();
    }
}

function renderDbStats(): void {
    if (!dbStatsElement) return;

    if (!dbStats) {
        dbStatsElement.innerHTML = '<div class="db-stats-loading">Loading database statistics...</div>';
        return;
    }

    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    dbStatsElement.innerHTML = `
        <div class="db-stats">
            <div class="db-stat-row">
                <span class="db-stat-label">Database Path:</span>
                <span class="db-stat-value">${dbStats.path}</span>
            </div>
            <div class="db-stat-row">
                <span class="db-stat-label">Storage Backend:</span>
                <span class="db-stat-value">${storageBackend}</span>
            </div>
            <div class="db-stat-row">
                <span class="db-stat-label">Total Attestations:</span>
                <span class="db-stat-value">${dbStats.total_attestations.toLocaleString()}</span>
            </div>
            <div class="db-stat-row">
                <span class="db-stat-label">Unique Actors:</span>
                <span class="db-stat-value">${dbStats.unique_actors.toLocaleString()}</span>
            </div>
            <div class="db-stat-row">
                <span class="db-stat-label">Unique Subjects:</span>
                <span class="db-stat-value">${dbStats.unique_subjects.toLocaleString()}</span>
            </div>
            <div class="db-stat-row">
                <span class="db-stat-label">Unique Contexts:</span>
                <span class="db-stat-value">${dbStats.unique_contexts.toLocaleString()}</span>
            </div>
        </div>
    `;
}

// Register test glyphs once DOM is ready
export function registerTestGlyphs(): void {
    // Canvas Glyph - Fractal container with spatial grid
    glyphRun.add(createCanvasGlyph());

    // VidStream Glyph
    glyphRun.add({
        id: 'vidstream-glyph',
        title: 'VidStream',
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '20px';
            content.innerHTML = `
                <h2 style="margin: 0 0 16px 0;">VidStream</h2>
                <p>Video streaming analytics and monitoring.</p>
                <div style="margin-top: 20px; padding: 16px; background: rgba(0,0,0,0.2); border-radius: 4px;">
                    <div>Active Streams: 42</div>
                    <div>Bandwidth: 1.2 GB/s</div>
                    <div>Viewers: 12,483</div>
                </div>
            `;
            return content;
        },
        initialWidth: '400px',
        initialHeight: '300px'
    });

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
        initialWidth: '600px',
        initialHeight: '400px'
    });

    // Self Diagnostics Glyph
    glyphRun.add({
        id: 'self-glyph',
        title: 'Self',
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '20px';
            content.innerHTML = `
                <h2 style="margin: 0 0 16px 0;">Self Diagnostics</h2>
                <p>QNTX system health and performance.</p>
                <div style="margin-top: 20px;">
                    <div style="margin-bottom: 8px; color: #4ade80;">
                        ✓ All systems operational
                    </div>
                    <hr style="margin: 16px 0; opacity: 0.2;">
                    <div style="font-size: 12px; opacity: 0.8;">
                        <div>Memory: 234 MB</div>
                        <div>CPU: 12%</div>
                        <div>Uptime: 3d 14h 22m</div>
                        <div>Version: ${window.location.hostname}</div>
                    </div>
                </div>
            `;
            return content;
        },
        initialWidth: '380px',
        initialHeight: '320px'
    });

    // TODO: Replace console.log with proper logger (log.debug)
    log.debug(SEG.UI, 'Test glyphs registered:', {
        vidstream: 'VidStream monitoring',
        database: 'Database Statistics',
        self: 'Self diagnostics'
    });
}