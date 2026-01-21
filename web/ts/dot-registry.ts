/**
 * Dot Registry - Standard QNTX Dots
 *
 * This replaces symbol-palette.ts as the central registry of available features.
 * Each QNTX feature (window, panel, command) is represented as a Dot.
 *
 * Architecture:
 * - Dots are registered with Dock on init
 * - Each dot has symbol, title, and expand behavior
 * - Windows and panels become dot-owned components
 */

import { Dot } from './components/dot';
import { dock } from './components/dock';
import {
    Pulse, Prose, DB,
    CommandToSymbol,
} from '@generated/sym.js';
import { log, SEG } from './logger';

// Import panel/window toggle functions (will be refactored to use Dot system)
import { toggleConfig } from './config-panel.js';
import { toggleAIProvider } from './ai-provider-window.js';
import { togglePulsePanel } from './pulse-panel.js';
import { toggleProsePanel } from './prose/panel.js';
import { toggleGoEditor } from './code/panel.js';
import { togglePythonEditor } from './python/panel.js';
import { togglePluginPanel } from './plugin-panel.js';
import { webscraperPanel } from './webscraper-panel.js';
import { VidStreamWindow } from './vidstream-window.js';
import { toggleJobList } from './hixtory-panel.js';

/**
 * Get symbol for a command (matches symbol-palette.ts)
 */
function getSymbol(cmd: string): string {
    if (cmd === 'pulse') return Pulse;
    if (cmd === 'prose') return Prose;
    if (cmd === 'db') return DB;
    if (cmd === 'go') return 'Go';
    if (cmd === 'py') return 'py';
    if (cmd === 'plugins') return '\u2699';
    if (cmd === 'scraper') return '⛶';
    if (cmd === 'vidstream') return '⮀';
    return CommandToSymbol[cmd] || cmd;
}

/**
 * Initialize dock with standard QNTX dots
 */
export function initializeDock(): void {
    log.debug(SEG.UI, 'Initializing dock with standard dots...');

    // Initialize dock component
    dock.init();

    // ========================================================================
    // EXAMPLE: VidStream as a proper Dot-owned window
    // ========================================================================
    // This shows how VidStream will work once migrated to Dot system
    // (Currently VidStream is created on-demand via symbol-palette.ts)
    /*
    const vidstreamDot = new Dot({
        id: 'vidstream-window',
        symbol: '⮀',
        title: 'VidStream - Real-time Video Inference',
        tooltip: '⮀ VidStream - video inference\n(version info loading...)',
        windowConfig: {
            width: '700px',
            // VidStream-specific window will be created by VidStreamWindow class
            // which will be refactored to accept Window instance
        },
        onExpand: () => {
            log(SEG.VID, 'VidStream dot expanded');
            // TODO: Initialize VidStream when migrated to Dot system
        },
    });
    dock.register(vidstreamDot);
    */

    // ========================================================================
    // Temporary: Register dots that call legacy toggle functions
    // (Will be refactored once panels/windows are migrated to Dot system)
    // ========================================================================

    // Config panel (≡ am)
    const configDot = new Dot({
        id: 'config',
        symbol: getSymbol('am'),
        title: 'Config - System Configuration',
        tooltip: '≡ Config - system configuration',
        onClick: () => toggleConfig(),
    });
    dock.register(configDot);

    // AI Provider (⌬ by)
    const aiProviderDot = new Dot({
        id: 'ai-provider',
        symbol: getSymbol('by'),
        title: 'AI Provider - Actor Configuration',
        tooltip: '⌬ Actor - origin of action',
        onClick: () => toggleAIProvider(),
    });
    dock.register(aiProviderDot);

    // Pulse panel (꩜)
    const pulseDot = new Dot({
        id: 'pulse',
        symbol: Pulse,
        title: 'Pulse - Async Operations',
        tooltip: '꩜ Pulse - async operations',
        onClick: () => togglePulsePanel(),
    });
    dock.register(pulseDot);

    // Database window (⊔ db)
    const dbDot = new Dot({
        id: 'db-stats',
        symbol: DB,
        title: 'Database - Storage Layer',
        tooltip: '⊔ Database - storage layer',
        onClick: async () => {
            const module = await import('./database-stats-window.js');
            module.databaseStatsWindow.toggle();
        },
    });
    dock.register(dbDot);

    // Prose panel (⚇)
    const proseDot = new Dot({
        id: 'prose',
        symbol: Prose,
        title: 'Prose - Documentation',
        tooltip: '⚇ Prose - documentation',
        onClick: () => toggleProsePanel(),
    });
    dock.register(proseDot);

    // Go editor
    const goDot = new Dot({
        id: 'go-editor',
        symbol: 'Go',
        title: 'Go Code Editor',
        tooltip: 'Go - code editor',
        onClick: () => toggleGoEditor(),
    });
    dock.register(goDot);

    // Python editor
    const pyDot = new Dot({
        id: 'py-editor',
        symbol: 'py',
        title: 'Python Editor',
        tooltip: 'py - Python editor',
        onClick: () => togglePythonEditor(),
    });
    dock.register(pyDot);

    // Plugins
    const pluginsDot = new Dot({
        id: 'plugins',
        symbol: '\u2699',
        title: 'Plugins - Domain Extensions',
        tooltip: '⚙ Plugins - domain extensions',
        onClick: () => togglePluginPanel(),
    });
    dock.register(pluginsDot);

    // Web Scraper
    const scraperDot = new Dot({
        id: 'scraper',
        symbol: '⛶',
        title: 'Scraper - Web Extraction',
        tooltip: '⛶ Scraper - web extraction',
        onClick: () => webscraperPanel.toggle(),
    });
    dock.register(scraperDot);

    // VidStream (using legacy toggle for now)
    const vidstreamDot = new Dot({
        id: 'vidstream',
        symbol: '⮀',
        title: 'VidStream - Video Inference',
        tooltip: '⮀ VidStream - video inference',
        onClick: () => {
            // Use legacy VidStreamWindow for now
            // Will be refactored when VidStream is migrated to Dot system
            const module = window as any;
            if (!module.vidstreamWindowInstance) {
                module.vidstreamWindowInstance = new VidStreamWindow();
            }
            module.vidstreamWindowInstance.toggle();
        },
    });
    dock.register(vidstreamDot);

    // Hixtory (⨳ ix)
    const hixtoryDot = new Dot({
        id: 'hixtory',
        symbol: getSymbol('ix'),
        title: 'Ingest - Job History',
        tooltip: '⨳ Ingest - import data',
        onClick: () => toggleJobList(),
    });
    dock.register(hixtoryDot);

    log.debug(SEG.UI, `Dock initialized with ${dock.count} dots`);
}

/**
 * Get a dot from the dock by ID
 */
export function getDot(id: string): Dot | undefined {
    return dock.get(id);
}
