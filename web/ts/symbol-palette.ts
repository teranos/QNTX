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
    Pulse, Prose,
    CommandToSymbol,
} from '@generated/sym.js';
import { uiState } from './ui-state.ts';
import { log, SEG } from './logger';
import { handleError } from './error-handler.ts';

// Import all panel/window modules statically
import { toggleConfig } from './config-panel.js';
import { toggleAIProvider } from './ai-provider-panel.js';
import { togglePulsePanel } from './pulse-panel.js';
import { toggleProsePanel } from './prose/panel.js';
import { toggleGoEditor } from './code/panel.js';
import { togglePythonEditor } from './python/panel.js';
import { togglePluginPanel } from './plugin-panel.js';
import { webscraperPanel } from './webscraper-panel.js';
import { VidStreamWindow } from './vidstream-window.js';
import { toggleJobList } from './hixtory-panel.js';

// Valid palette commands (derived from generated mappings + UI-only commands)
type PaletteCommand = keyof typeof CommandToSymbol | 'pulse' | 'prose' | 'go' | 'py' | 'plugins' | 'scraper' | 'vidstream';

/**
 * Get symbol for a command, with fallback for UI-only commands
 */
function getSymbol(cmd: string): string {
    if (cmd === 'pulse') return Pulse;
    if (cmd === 'prose') return Prose;
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
});

function initializeSymbolPalette(): void {
    const cmdCells = document.querySelectorAll('.palette-cell');

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
        // Tooltips now handled purely via CSS ::after pseudo-element

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
            // Self - operator vantage point
            activateSearchMode(cmd);
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
        case 'scraper':
            // Web Scraper - show scraping panel
            showWebscraperPanel();
            break;
        case 'vidstream':
            // VidStream - show video inference window
            log(SEG.VID, 'VidStream button clicked');
            showVidStreamWindow();
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
 * Show webscraper panel - UI for web scraping operations
 */
function showWebscraperPanel(): void {
    webscraperPanel.toggle();
}

/**
 * Show VidStream window - real-time video inference (desktop only)
 */
let vidstreamWindowInstance: VidStreamWindow | null = null;
function showVidStreamWindow(): void {
    log(SEG.VID, 'showVidStreamWindow() called');
    try {
        if (!vidstreamWindowInstance) {
            log(SEG.VID, 'Creating new VidStreamWindow instance...');
            vidstreamWindowInstance = new VidStreamWindow();
            log(SEG.VID, 'VidStreamWindow instance created');
        }
        log(SEG.VID, 'Calling toggle()');
        vidstreamWindowInstance.toggle();
    } catch (err) {
        handleError(err, 'Failed to show VidStream window', { context: SEG.VID });
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