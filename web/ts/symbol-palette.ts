/**
 * Symbol Palette - QNTX SEG Symbol Interface
 *
 * The palette encodes QNTX's architecture as a semantic chain:
 * ⍟ (i)  — Self/operator vantage point
 * ≡ (am) — AM structure/internal interpretation
 * ⨳ (ix) — Ingest/import external data
 * ⋈ (ax) — Expand/surface related context
 * + (as) — Assert/emit attestation
 * = (is) — Identity/equivalence
 * ∈ (of) — Membership/element-of/belonging
 * ⌬ (by) — Actor/catalyst/origin of action
 * ✦ (at) — Event/temporal marker
 * ⟶ (so) — Output/materialized result
 *
 * Future symbols:
 * 𓂀 (proposed) — Knowledge/documentation access
 *
 * Dual-mode support:
 * - Accepts both symbols (⋈) and text commands (ax) in queries
 * - UI displays symbols with hover tooltips showing text labels
 * - Backwards compatible with existing text-based workflows
 */

// Import generated symbol constants and mappings from Go source
import {
    SO,
    Pulse, Prose, DB,
    CommandToSymbol,
} from '@generated/sym.js';
import { uiState } from './state/ui.ts';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';
import { tooltip } from './components/tooltip.ts';

// Import all panel/window modules statically
import { toggleConfig } from './config-panel.js';
import { toggleAIProvider } from './ai-provider-window.js';
import { togglePulsePanel } from './pulse-panel.js';
import { toggleProsePanel } from './prose/panel.js';
import { toggleGoEditor } from './code/panel.js';
import { togglePythonEditor } from './python/panel.js';
import { togglePluginPanel } from './plugin-panel.js';
import { VidStreamWindow } from './vidstream-window.js';
import { toggleJobList } from './hixtory-panel.js';

// Valid palette commands (derived from generated mappings + UI-only commands)
type PaletteCommand = keyof typeof CommandToSymbol | 'pulse' | 'prose' | 'go' | 'py' | 'plugins' | 'vidstream' | 'db' | 'ctp2';

/**
 * Get symbol for a command, with fallback for UI-only commands
 */
function getSymbol(cmd: string): string {
    if (cmd === 'pulse') return Pulse;
    if (cmd === 'prose') return Prose;
    if (cmd === 'db') return DB;
    if (cmd === 'go') return 'Go';
    if (cmd === 'py') return 'py';
    if (cmd === 'plugins') return '\u2699'; // Gear symbol
    if (cmd === 'scraper') return '⛶'; // White draughts king - extraction/capture
    if (cmd === 'vidstream') return '⮀'; // VidStream - video inference
    return CommandToSymbol[cmd] || cmd;
}

// Extend window interface for global functions
interface CommandExplorerPanel {
    toggle: (mode: string) => void;
}

declare global {
    interface Window {
        setActiveModality: (cmd: string) => void;
        commandExplorerPanel?: CommandExplorerPanel;
    }
}

document.addEventListener('DOMContentLoaded', () => {
    initializeSymbolPalette();
    // Restore modality from persisted UI state
    setActiveModality(uiState.getActiveModality());
    // Inject CTP2 SVG glyph
    injectCTP2Glyph();
});

function initializeSymbolPalette(): void {
    const cmdCells = document.querySelectorAll('.palette-cell');

    // Populate symbols from generated sym.ts (single source of truth)
    cmdCells.forEach(cell => {
        const cmd = cell.getAttribute('data-cmd');
        if (cmd && cmd !== 'ctp2') { // Skip ctp2, it uses SVG injection
            cell.textContent = getSymbol(cmd);
        }
    });

    // Mark VidStream as degraded in desktop mode (camera not yet supported)
    const isTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;
    if (isTauri) {
        const vidstreamCell = document.querySelector('.palette-cell[data-cmd="vidstream"]');
        if (vidstreamCell) {
            vidstreamCell.classList.add('degraded');
            vidstreamCell.setAttribute('aria-label', 'VidStream: camera not yet supported in desktop mode (browser only)');
        }
    }

    cmdCells.forEach((cell, index) => {
        cell.addEventListener('click', handleSymbolClick);

        // Add has-tooltip class for proper tooltip system
        cell.classList.add('has-tooltip');

        // Set initial tooltip text (will be updated when system capabilities are received)
        const cmd = cell.getAttribute('data-cmd');
        if (cmd) {
            const initialTooltip = getInitialTooltip(cmd);
            cell.setAttribute('data-tooltip', initialTooltip);
        }

        // Virtue #14: Keyboard Navigation - Full arrow key support for palette traversal
        cell.addEventListener('keydown', (e: Event) => {
            const keyEvent = e as KeyboardEvent;
            let nextElement: Element | null = null;

            switch(keyEvent.key) {
                case 'ArrowRight':
                case 'ArrowDown':
                    nextElement = cmdCells[index + 1] || cmdCells[0]; // Wrap to first
                    break;
                case 'ArrowLeft':
                case 'ArrowUp':
                    nextElement = cmdCells[index - 1] || cmdCells[cmdCells.length - 1]; // Wrap to last
                    break;
                case 'Home':
                    nextElement = cmdCells[0];
                    break;
                case 'End':
                    nextElement = cmdCells[cmdCells.length - 1];
                    break;
            }

            if (nextElement && nextElement instanceof HTMLElement) {
                e.preventDefault();
                nextElement.focus();
            }
        });

        // Set tabindex for first element to enable keyboard entry
        if (index === 0) {
            cell.setAttribute('tabindex', '0');
        } else {
            cell.setAttribute('tabindex', '-1');
        }
    });

    // Attach tooltip system to palette
    const palette = document.querySelector('.palette') as HTMLElement;
    if (palette) {
        tooltip.attach(palette, '.has-tooltip');
    }
}

/**
 * Inject CTP2 SVG glyph into palette cell
 */
async function injectCTP2Glyph(): Promise<void> {
    try {
        const { generateCTP2Glyph } = await import('../ctp2/glyph.js');
        const cell = document.getElementById('ctp2-palette-cell');
        if (cell) {
            cell.innerHTML = generateCTP2Glyph();
        }
    } catch (error: unknown) {
        console.warn('[Symbol Palette] Failed to load CTP2 glyph:', error);
        // Fallback to text
        const cell = document.getElementById('ctp2-palette-cell');
        if (cell) {
            cell.textContent = 'CTP2';
        }
    }
}

/**
 * Get initial tooltip text for a palette command
 * Will be updated with version info when system capabilities are received
 */
function getInitialTooltip(cmd: string): string {
    const tooltips: Record<string, string> = {
        'i': '⍟ Self - system diagnostic',
        'am': '≡ Config - system configuration',
        'ix': '⨳ Ingest - import data',
        'ax': '⋈ Expand - contextual query\n(version info loading...)',
        'as': '+ Assert - emit attestation',
        'is': '= Identity - equivalence',
        'of': '∈ Membership - belonging',
        'by': '⌬ Actor - origin of action',
        'at': '✦ Event - temporal marker',
        'so': '⟶ Therefore - consequent action',
        'pulse': '꩜ Pulse - async operations',
        'db': '⊔ Database - storage layer',
        'prose': '⚇ Prose - documentation',
        'go': 'Go - code editor',
        'py': 'py - Python editor',
        'plugins': '⚙ Plugins - domain extensions',
        'vidstream': '⮀ VidStream - video inference\n(version info loading...)',
        'ctp2': 'CTP2',
    };
    return tooltips[cmd] || cmd;
}

/**
 * Set active modality - highlights the current symbol and persists to UIState
 */
function setActiveModality(cmd: string): void {
    // Persist to centralized UI state
    uiState.setActiveModality(cmd);

    // Remove active class from all cells
    document.querySelectorAll('.palette-cell').forEach(cell => {
        cell.classList.remove('active');
    });

    // Add active class to current modality
    const activeCell = document.querySelector(`.palette-cell[data-cmd="${cmd}"]`);
    if (activeCell) {
        activeCell.classList.add('active');
    }

    log.debug(SEG.SELF, `Modality set to: ${cmd}`);
}

// Export for use by other modules
// Avoid Sin #5: Global Pollution - Only export what's truly needed globally
window.setActiveModality = setActiveModality;

/**
 * Handle symbol cell clicks
 */
function handleSymbolClick(e: Event): void {
    const target = e.target as HTMLElement;
    const cmd = target.dataset.cmd as PaletteCommand | undefined;

    if (!cmd) return;

    const symbol = getSymbol(cmd);
    log.debug(SEG.SELF, `${symbol} (${cmd}) clicked`);

    // Set as active modality (color inversion)
    setActiveModality(cmd);

    // Route to appropriate handler
    switch(cmd) {
        case 'i':
            // Self - operator vantage point, system diagnostic
            showSelfWindow();
            break;
        case 'am':
            // Configuration - system configuration introspection
            showConfigPanel();
            break;
        case 'ax':
            // Expand - show ax command explorer
            if (window.commandExplorerPanel) {
                window.commandExplorerPanel.toggle(cmd);
            } else {
                activateSearchMode(cmd);
            }
            break;
        case 'ix':
            // Ingest - show running IX jobs
            activateIngestMode(cmd);
            break;
        case 'as':
            // Assert - show query history
            if (window.commandExplorerPanel) {
                window.commandExplorerPanel.toggle(cmd);
            } else {
                activateAttestationMode(cmd);
            }
            break;
        case 'is':
            // Identity - insert segment
            insertSegment(cmd);
            break;
        case 'of':
            // Membership - insert segment
            insertSegment(cmd);
            break;
        case 'by':
            // Actor - show AI provider panel
            showAIProviderPanel();
            break;
        case 'at':
            // Event - insert segment
            insertSegment(cmd);
            break;
        case 'so':
            // Therefore - consequent action/trigger
            handleSoCommand(cmd);
            break;
        case 'pulse':
            // Pulse - show scheduled jobs panel
            showPulsePanel();
            break;
        case 'db':
            // Database - show database statistics window
            showDatabaseWindow();
            break;
        case 'prose':
            // Prose - show documentation panel
            showProsePanel();
            break;
        case 'go':
            // Go - show Go code editor with gopls integration
            showGoEditor();
            break;
        case 'py':
            // Python - show Python code editor/executor
            showPythonEditor();
            break;
        case 'plugins':
            // Plugins - show installed domain plugins
            showPluginPanel();
            break;
        case 'vidstream':
            // VidStream - show video inference window
            log(SEG.VID, 'VidStream button clicked');
            showVidStreamWindow();
            break;
        case 'ctp2':
            // CTP2 - show CTP2 window
            showCTP2Window();
            break;
        default:
            console.warn(`[Symbol Palette] Unknown command: ${cmd}`);
    }
}

/**
 * Activate search mode - focus query input
 */
function activateSearchMode(mode: string): void {
    const queryInput = document.getElementById('ats-editor') as HTMLInputElement | null;
    if (queryInput) {
        queryInput.focus();
        queryInput.select();
        log.debug(SEG.SELF, `${getSymbol(mode)} search mode activated`);
    }
}

/**
 * Show config panel - displays configuration introspection
 */
function showConfigPanel(): void {
    toggleConfig();
}

/**
 * Show AI provider panel - displays actor/agent configuration
 */
function showAIProviderPanel(): void {
    toggleAIProvider();
}

/**
 * Show pulse panel - displays scheduled jobs dashboard
 */
function showPulsePanel(): void {
    togglePulsePanel();
}

/**
 * Show database window - displays database statistics
 */
let databaseWindowInstance: any = null;
async function showDatabaseWindow(): Promise<void> {
    if (!databaseWindowInstance) {
        const module = await import('./database-stats-window.js');
        databaseWindowInstance = module.databaseStatsWindow;
    }
    databaseWindowInstance.toggle();
}

/**
 * Show self window - displays system diagnostic information
 */
let selfWindowInstance: any = null;
async function showSelfWindow(): Promise<void> {
    if (!selfWindowInstance) {
        const module = await import('./self-window.js');
        selfWindowInstance = module.selfWindow;
    }
    selfWindowInstance.toggle();
}

/**
 * Show prose panel - displays documentation viewer/editor
 */
function showProsePanel(): void {
    toggleProsePanel();
}

/**
 * Show Go editor - displays Go code editor with gopls LSP integration
 */
function showGoEditor(): void {
    toggleGoEditor();
}

/**
 * Show Python editor - displays Python code editor with execution support
 */
function showPythonEditor(): void {
    togglePythonEditor();
}

/**
 * Show plugin panel - displays installed domain plugins and their status
 */
function showPluginPanel(): void {
    togglePluginPanel();
}

/**
 * Show CTP2 window
 */
async function showCTP2Window(): Promise<void> {
    // CTP2 is an optional/private module that may not exist in all environments
    // Comment out the import to prevent build failures when the module is missing
    console.log('CTP2 module not available in this environment (optional/private feature)');
    const statusEl = document.getElementById('status-message');
    if (statusEl) {
        statusEl.textContent = 'CTP2 module not available';
        setTimeout(() => statusEl.textContent = '', 3000);
    }

    // Original implementation for when CTP2 is available:
    // if (!ctp2WindowInstance) {
    //     const module = await import('../ctp2/window.js');
    //     ctp2WindowInstance = new module.CTP2Window();
    // }
    // ctp2WindowInstance.toggle();
}

/**
 * Show VidStream window - real-time video inference (desktop only)
 */
let vidstreamWindowInstance: VidStreamWindow | null = null;
let vidstreamVersion: string | null = null; // Store version before window creation

/**
 * Get VidStream window instance (for system-capabilities updates)
 */
export function getVidStreamWindowInstance(): VidStreamWindow | null {
    return vidstreamWindowInstance;
}

/**
 * Set VidStream version (called from system-capabilities before window exists)
 */
export function setVidStreamVersion(version: string): void {
    vidstreamVersion = version;
    // Also update existing instance if present
    if (vidstreamWindowInstance) {
        vidstreamWindowInstance.updateVersion(version);
    }
}

function showVidStreamWindow(): void {
    log(SEG.VID, 'showVidStreamWindow() called');
    try {
        if (!vidstreamWindowInstance) {
            log(SEG.VID, 'Creating new VidStreamWindow instance...');
            vidstreamWindowInstance = new VidStreamWindow();
            log(SEG.VID, 'VidStreamWindow instance created');

            // Apply stored version if received before window creation
            if (vidstreamVersion) {
                vidstreamWindowInstance.updateVersion(vidstreamVersion);
                log(SEG.VID, `Applied stored version: ${vidstreamVersion}`);
            }
        }
        log(SEG.VID, 'Calling toggle()');
        vidstreamWindowInstance.toggle();
    } catch (error: unknown) {
        handleError(error, 'Failed to show VidStream window', { context: SEG.VID });
    }
}

/**
 * Activate ingestion mode - show running IX jobs panel
 */
function activateIngestMode(mode: string): void {
    toggleJobList();
    log.debug(SEG.SELF, `${getSymbol(mode)} ingest mode - showing job list`);
}

/**
 * Activate attestation entry mode
 */
function activateAttestationMode(mode: string): void {
    const queryInput = document.getElementById('ats-editor') as HTMLInputElement | null;
    if (queryInput) {
        queryInput.focus();
        // Pre-populate with attestation template if empty
        if (!queryInput.value.trim()) {
            queryInput.value = 'is ';
            queryInput.selectionStart = queryInput.value.length;
        }
        log.debug(SEG.SELF, `${getSymbol(mode)} attestation mode activated`);
    }
}

/**
 * Insert .ats segment at cursor position
 */
function insertSegment(segment: string): void {
    const queryInput = document.getElementById('ats-editor') as HTMLInputElement | null;
    if (!queryInput) return;

    queryInput.focus();

    const start = queryInput.selectionStart || 0;
    const end = queryInput.selectionEnd || 0;
    const text = queryInput.value;

    // Add space before segment if needed
    const prefix = start > 0 && text[start - 1] !== ' ' ? ' ' : '';
    const newSegment = prefix + segment + ' ';

    queryInput.value = text.substring(0, start) + newSegment + text.substring(end);
    queryInput.selectionStart = queryInput.selectionEnd = start + newSegment.length;

    log.debug(SEG.SELF, `${getSymbol(segment)} segment inserted`);
}

/**
 * Handle 'so' command - Therefore (consequent action/trigger)
 *
 * "so" represents logical consequence: when data/filters/attestations occur,
 * therefore this action/trigger happens.
 *
 * Intentionally unfinalized. Behavior depends on selection context.
 * Currently logs intent; actual implementation will emerge as use cases clarify.
 */
function handleSoCommand(_cmd: string): void {
    log.debug(SEG.SELF, `${SO} (so/therefore) - consequent action triggered`);

    // Placeholder for future implementation
    // Possible behaviors:
    // - Context-aware trigger builder (data ingested ⟶ send notification)
    // - Workflow automation (filter matched ⟶ execute action)
    // - Conditional actions (attestation created ⟶ trigger webhook)
    // - Batch operations (query results ⟶ apply transformation)
}