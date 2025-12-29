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

// Make this a module by exporting something
export {};

// Symbol-to-command mapping for dual-mode acceptance
const SYMBOL_TO_CMD: Record<string, string> = {
    '⍟': 'i',
    '≡': 'am',
    '⨳': 'ix',
    '⋈': 'ax',
    '+': 'as',
    '=': 'is',
    '∈': 'of',
    '⌬': 'by',
    '✦': 'at',
    '⟶': 'so'
};

const CMD_TO_SYMBOL: Record<string, string> = {
    'i': '⍟',
    'am': '≡',
    'ix': '⨳',
    'ax': '⋈',
    'as': '+',
    'is': '=',
    'of': '∈',
    'by': '⌬',
    'at': '✦',
    'so': '⟶'
};

// Track current modality
let currentModality: string = 'ax'; // Default to 'ax' modality

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
    setActiveModality(currentModality);
});

function initializeSymbolPalette(): void {
    const cmdCells = document.querySelectorAll('.palette-cell');

    cmdCells.forEach(cell => {
        cell.addEventListener('click', handleSymbolClick);
        cell.addEventListener('mouseenter', showSymbolTooltip);
        cell.addEventListener('mouseleave', hideSymbolTooltip);
    });
}

/**
 * Set active modality - highlights the current symbol
 */
function setActiveModality(cmd: string): void {
    currentModality = cmd;

    // Remove active class from all cells
    document.querySelectorAll('.palette-cell').forEach(cell => {
        cell.classList.remove('active');
    });

    // Add active class to current modality
    const activeCell = document.querySelector(`.palette-cell[data-cmd="${cmd}"]`);
    if (activeCell) {
        activeCell.classList.add('active');
    }

    console.log(`[Symbol Palette] Modality set to: ${cmd}`);
}

// Export for use by other modules
window.setActiveModality = setActiveModality;

/**
 * Handle symbol cell clicks
 */
function handleSymbolClick(e: Event): void {
    const target = e.target as HTMLElement;
    const cmd = target.dataset.cmd;

    if (!cmd) return;

    console.log(`[Symbol Palette] Clicked: ${cmd}`);

    // Set as active modality (color inversion)
    setActiveModality(cmd);

    // Route to appropriate handler
    switch(cmd) {
        case 'i':
            // Self - operator vantage point
            console.log(`[Symbol Palette] i (self) - self introspection`);
            activateSearchMode('i');
            break;
        case 'am':
            // Configuration - system configuration introspection
            console.log(`[Symbol Palette] am (config) - showing configuration`);
            showConfigPanel();
            break;
        case 'ax':
            // Expand - show ax command explorer
            if (window.commandExplorerPanel) {
                window.commandExplorerPanel.toggle('ax');
            } else {
                activateSearchMode('ax');
            }
            break;
        case 'ix':
            // Ingest - show running IX jobs
            activateIngestMode('ix');
            break;
        case 'as':
            // Assert - show query history
            if (window.commandExplorerPanel) {
                window.commandExplorerPanel.toggle('as');
            } else {
                activateAttestationMode('as');
            }
            break;
        case 'is':
            // Identity - insert segment
            insertSegment('is');
            break;
        case 'of':
            // Membership - insert segment
            insertSegment('of');
            break;
        case 'by':
            // Actor - show AI provider panel
            showAIProviderPanel();
            break;
        case 'at':
            // Event - insert segment
            insertSegment('at');
            break;
        case 'so':
            // Therefore - consequent action/trigger
            handleSoCommand('so');
            break;
        case 'pulse':
            // Pulse - show scheduled jobs panel
            console.log(`[Symbol Palette] pulse - showing scheduled jobs`);
            showPulsePanel();
            break;
        case 'prose':
            // Prose - show documentation panel
            console.log(`[Symbol Palette] prose - showing documentation`);
            showProsePanel();
            break;
        case 'go':
            // Go - show Go code editor with gopls integration
            console.log(`[Symbol Palette] go - showing Go code editor`);
            showGoEditor();
            break;
        default:
            console.warn(`[Symbol Palette] Unknown command: ${cmd}`);
    }
}

/**
 * Show tooltip with symbol description
 * Note: Tooltips now handled via CSS ::after pseudo-element using data-label attribute
 */
function showSymbolTooltip(e: Event): void {
    // Tooltip display is now handled purely by CSS using the data-label attribute
    // This function maintained for potential future enhancements
}

function hideSymbolTooltip(e: Event): void {
    // Tooltip hiding is now handled purely by CSS
    // This function maintained for potential future enhancements
}

/**
 * Activate search mode - focus query input
 */
function activateSearchMode(mode: string): void {
    const queryInput = document.getElementById('ats-editor') as HTMLInputElement | null;
    if (queryInput) {
        queryInput.focus();
        queryInput.select();
        console.log(`[Symbol Palette] Activated ${mode} search mode`);
    }
}

/**
 * Show config panel - displays configuration introspection
 */
async function showConfigPanel(): Promise<void> {
    const { toggleConfig } = await import('./config-panel.js');
    toggleConfig();
}

/**
 * Show AI provider panel - displays actor/agent configuration
 */
async function showAIProviderPanel(): Promise<void> {
    const { toggleAIProvider } = await import('./ai-provider-panel.js');
    toggleAIProvider();
}

/**
 * Show pulse panel - displays scheduled jobs dashboard
 */
async function showPulsePanel(): Promise<void> {
    const { togglePulsePanel } = await import('./pulse-panel.js');
    togglePulsePanel();
}

/**
 * Show prose panel - displays documentation viewer/editor
 */
async function showProsePanel(): Promise<void> {
    const { toggleProsePanel } = await import('./prose/panel.js');
    toggleProsePanel();
}

/**
 * Show Go editor - displays Go code editor with gopls LSP integration
 */
async function showGoEditor(): Promise<void> {
    const { toggleGoEditor } = await import('./go-editor-panel.js');
    toggleGoEditor();
}

/**
 * Activate ingestion mode - show running IX jobs panel
 */
async function activateIngestMode(mode: string): Promise<void> {
    // Show job list panel (IMPLEMENTED)
    const { toggleJobList } = await import('./hixtory-panel.js');
    toggleJobList();
    console.log(`[Symbol Palette] Activated ${mode} ingest mode - showing job list`);
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
        console.log(`[Symbol Palette] Activated ${mode} attestation mode`);
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

    console.log(`[Symbol Palette] Inserted segment: "${segment}"`);
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
function handleSoCommand(mode: string): void {
    console.log(`[Symbol Palette] ⟶ (so/therefore) - consequent action triggered`);
    console.log(`[Symbol Palette] Context: ${mode}`);

    // Placeholder for future implementation
    // Possible behaviors:
    // - Context-aware trigger builder (data ingested ⟶ send notification)
    // - Workflow automation (filter matched ⟶ execute action)
    // - Conditional actions (attestation created ⟶ trigger webhook)
    // - Batch operations (query results ⟶ apply transformation)
}