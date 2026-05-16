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
    CommandDescriptions,
} from '@generated/sym.js';
import { uiState } from './state/ui.ts';
import { log, SEG } from './logger';
import { tooltip } from './components/tooltip.ts';

// Import all panel/window modules statically
import { toggleConfig } from './config-panel.js';
// ai-provider-window.ts removed — LLM provider is now a tray glyph
// pulse-panel.ts removed — Pulse is now a tray glyph
import { toggleProsePanel } from './prose/panel.js';
import { togglePythonEditor } from './python/panel.js';
import { glyphRun } from '@qntx/glyphs';

// Valid palette commands (derived from generated mappings + UI-only commands)
type PaletteCommand = keyof typeof CommandToSymbol | 'pulse' | 'prose' | 'go' | 'py' | 'plugins' | 'db';

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
    return CommandToSymbol[cmd] || cmd;
}

declare global {
    interface Window {
        setActiveModality: (cmd: string) => void;
    }
}

document.addEventListener('DOMContentLoaded', () => {
    initializeSymbolPalette();
    // Restore modality from persisted UI state
    setActiveModality(uiState.getActiveModality());
});

function initializeSymbolPalette(): void {
    const cmdCells = document.querySelectorAll('.palette-cell');

    // Populate symbols from generated sym.ts (single source of truth)
    cmdCells.forEach(cell => {
        const cmd = cell.getAttribute('data-cmd');
        if (cmd) {
            cell.textContent = getSymbol(cmd);
        }
    });

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
 * Get initial tooltip text for a palette command
 * Will be updated with version info when system capabilities are received
 */
function getInitialTooltip(cmd: string): string {
    // SEG operators: derive from canonical CommandDescriptions (single source of truth)
    const description = CommandDescriptions[cmd];
    if (description) {
        const symbol = getSymbol(cmd);
        const extra = cmd === 'ax' ? '\n(version info loading...)' : '';
        return `${symbol} ${description}${extra}`;
    }

    // UI-only commands (not SEG operators)
    const uiTooltips: Record<string, string> = {
        'pulse': '꩜ Pulse — Async operations',
        'db': '⊔ Database — Storage layer',
        'prose': '⚇ Prose — Documentation',
        'py': 'py — Python editor',
        'plugins': '⚙ Plugins — Domain extensions',
    };
    return uiTooltips[cmd] || cmd;
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
            glyphRun.openGlyph('self-glyph');
            break;
        case 'am':
            // Configuration - system configuration introspection
            showConfigPanel();
            break;
        case 'ax':
            activateSearchMode(cmd);
            break;
        case 'ix':
            // Ingest - job visibility moved to Pulse
            glyphRun.openGlyph('pulse-glyph');
            break;
        case 'as':
            activateAttestationMode(cmd);
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
            glyphRun.openGlyph('pulse-glyph');
            break;
        case 'db':
            // Database - show database statistics glyph
            glyphRun.openGlyph('database-glyph');
            break;
        case 'sigma':
            // Sigma - show sigma overview panel
            glyphRun.openGlyph('sigma-panel');
            break;
        case 'prose':
            // Prose - show documentation panel
            showProsePanel();
            break;
        case 'py':
            // Python - show Python code editor/executor
            showPythonEditor();
            break;
        case 'plugins':
            // Plugins - show installed domain plugins
            showPluginPanel();
            break;
        default:
            log.warn(SEG.UI, `[Symbol Palette] Unknown command: ${cmd}`);
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
 * Show LLM provider glyph - opens the tray glyph for provider selection
 */
function showAIProviderPanel(): void {
    glyphRun.openGlyph('llm-provider-glyph');
}

/**
 * Show prose panel - displays documentation viewer/editor
 */
function showProsePanel(): void {
    toggleProsePanel();
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
    glyphRun.openGlyph('plugin-glyph');
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