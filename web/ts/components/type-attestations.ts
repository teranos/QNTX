// Type Attestations - Node type visualization and filtering controls

import { appState } from '../state/app.ts';
import { UI_TEXT } from '../config.ts';
import type { GraphData, NodeTypeInfo } from '../../types/core';
import { sendMessage } from '../websocket.ts'; // Phase 2: Send visibility preferences to backend
import { typeDefinitionWindow } from '../type-definition-window.js';

// Re-export graph visibility state from appState for backward compatibility
// All visibility state is now centralized in appState.graphVisibility
export const hiddenNodeTypes = appState.graphVisibility.hiddenNodeTypes;
export const revealRelatedActive = appState.graphVisibility.revealRelatedActive;

// Getter for hideIsolated (re-exporting a primitive requires a getter)
export function getHideIsolated(): boolean {
    return appState.graphVisibility.hideIsolated;
}

// Setter for hideIsolated (needed for checkbox handler)
export function setHideIsolated(value: boolean): void {
    appState.graphVisibility.hideIsolated = value;
}

// Event listener tracking interface
interface EventListenerRecord {
    element: Element;
    event: string;
    handler: EventListener;
}

// Event listener tracking for proper cleanup
const eventListeners = {
    listeners: [] as EventListenerRecord[],
    add: function(element: Element, event: string, handler: EventListener): void {
        element.addEventListener(event, handler);
        this.listeners.push({ element, event, handler });
    },
    removeAll: function(): void {
        this.listeners.forEach(({ element, event, handler }) => {
            element.removeEventListener(event, handler);
        });
        this.listeners = [];
    }
};

// Helper: Normalize node type for comparison (DRY)
function normalizeNodeType(type: string | null | undefined): string {
    return (type || '').trim().toLowerCase();
}

// Helper: Update visual state of type attestation item
function updateTypeAttestationItemVisualState(item: HTMLElement, typeNameSpan: HTMLElement, isHidden: boolean): void {
    item.style.opacity = isHidden ? '0.4' : '1';
    typeNameSpan.style.textDecoration = isHidden ? 'line-through' : 'none';
}

// Helper: Setup type name click handler
function setupTypeNameHandler(
    item: HTMLElement,
    typeNameSpan: HTMLElement
): void {
    typeNameSpan.addEventListener('click', function(e: Event): void {
        e.stopPropagation();

        // Phase 2: Get the actual node type (not the label) from data attribute
        const nodeType = normalizeNodeType(item.getAttribute('data-node-type'));

        // Toggle visibility in local state (for UI responsiveness)
        const isHidden = !hiddenNodeTypes.has(nodeType);
        if (hiddenNodeTypes.has(nodeType)) {
            hiddenNodeTypes.delete(nodeType);
        } else {
            hiddenNodeTypes.add(nodeType);
        }

        updateTypeAttestationItemVisualState(item, typeNameSpan, isHidden);

        // Phase 2: Send visibility preference to backend
        // Backend will apply filter and send back updated graph
        sendMessage({
            type: 'visibility',
            action: 'toggle_node_type',
            node_type: nodeType,
            hidden: isHidden
        });

        // Note: No longer calling renderGraphFn here - backend will send updated graph via WebSocket
    });
}

// Helper: Setup reveal button click handler
function setupRevealButtonHandler(
    item: HTMLElement,
    revealButton: HTMLElement,
    renderGraphFn: (data: GraphData) => void
): void {
    revealButton.addEventListener('click', function(e: Event): void {
        e.stopPropagation();

        // Phase 2: Get the actual node type (not the label) from data attribute
        const nodeType = normalizeNodeType(item.getAttribute('data-node-type'));

        // Toggle reveal related state and change symbol
        if (revealRelatedActive.has(nodeType)) {
            revealRelatedActive.delete(nodeType);
            revealButton.classList.remove('active');
            revealButton.textContent = '⨁'; // Circled plus (inactive)
        } else {
            revealRelatedActive.add(nodeType);
            revealButton.classList.add('active');
            revealButton.textContent = '⊗'; // Circled times (active)
        }

        // Re-render graph
        if (appState.currentGraphData) {
            renderGraphFn(appState.currentGraphData);
        }
    });
}

// Build legenda HTML from backend node types (backend is single source of truth)
// Frontend no longer maintains hardcoded node type registry
export function buildTypeAttestations(graphData: GraphData | null = null): void {
    const typeAttestationsContainer = document.querySelector('.type-attestations') as HTMLElement | null;
    if (!typeAttestationsContainer) return;

    // Clear existing content except title
    const existingTitle = typeAttestationsContainer.querySelector('.type-attestations-title') as HTMLElement | null;
    typeAttestationsContainer.innerHTML = '';

    // Create title container with + button
    const titleContainer = document.createElement('div');
    titleContainer.style.display = 'flex';
    titleContainer.style.alignItems = 'center';
    titleContainer.style.justifyContent = 'space-between';
    titleContainer.style.marginBottom = '6px';

    // Re-add title or create it
    if (existingTitle) {
        titleContainer.appendChild(existingTitle);
    } else {
        const title = document.createElement('div');
        title.className = 'type-attestations-title';
        title.innerHTML = UI_TEXT.TYPE_ATTESTATIONS_TITLE;
        title.style.margin = '0';  // Remove default margin since container handles spacing
        titleContainer.appendChild(title);
    }

    // Add + button for creating new types (using + as the "as" attestation symbol)
    const addButton = document.createElement('button');
    addButton.textContent = '+';
    addButton.title = 'Attest new type';
    addButton.style.cssText = `
        background: transparent;
        border: 1px solid var(--control-border, #ddd);
        border-radius: 3px;
        cursor: pointer;
        font-size: 14px;
        font-weight: 600;
        padding: 0 6px;
        line-height: 18px;
        color: var(--panel-text-secondary, #666);
        transition: all 0.2s ease;
    `;

    // Hover effect
    addButton.addEventListener('mouseenter', () => {
        addButton.style.background = 'var(--control-bg, #f8f8f8)';
        addButton.style.color = 'var(--accent-color, #0066cc)';
    });
    addButton.addEventListener('mouseleave', () => {
        addButton.style.background = 'transparent';
        addButton.style.color = 'var(--panel-text-secondary, #666)';
    });

    addButton.addEventListener('click', () => {
        typeDefinitionWindow.createNewType();
    });

    titleContainer.appendChild(addButton);
    typeAttestationsContainer.appendChild(titleContainer);

    // Require backend data - fail loud if not provided (Phase 1: enforce backend responsibility)
    if (!graphData?.meta?.node_types || graphData.meta.node_types.length === 0) {
        console.warn('No node type metadata from backend - legend cannot be rendered');
        return;
    }

    // Use node types from backend (includes counts, colors, and labels)
    const typesToRender: NodeTypeInfo[] = graphData.meta.node_types;

    // Build legenda items from backend data using DOM API for security
    typesToRender.forEach((type: NodeTypeInfo) => {
        const item = document.createElement('div');
        item.className = 'type-attestation-item';
        const countInfo = type.count ? ` (${type.count})` : '';
        item.title = `Click name to show/hide ${type.label} nodes${countInfo}`;

        // Phase 2: Store the actual type value (not label) for backend matching
        item.setAttribute('data-node-type', type.type);

        // Build item contents using DOM API for security
        const colorDiv = document.createElement('div');
        colorDiv.className = 'type-attestation-color';
        colorDiv.style.background = type.color ?? '#666666';

        const revealSpan = document.createElement('span');
        revealSpan.className = 'reveal-related';
        revealSpan.title = UI_TEXT.REVEAL_TOOLTIP(type.label);
        revealSpan.textContent = '⨁';

        const typeNameSpan = document.createElement('span');
        typeNameSpan.className = 'type-attestation-name';
        typeNameSpan.textContent = type.label;

        item.appendChild(colorDiv);
        item.appendChild(revealSpan);
        item.appendChild(typeNameSpan);

        if (type.count) {
            const countSpan = document.createElement('span');
            countSpan.className = 'type-attestation-count';
            countSpan.textContent = String(type.count);
            item.appendChild(countSpan);
        }

        // Add edit icon that appears on hover
        const editIcon = document.createElement('span');
        editIcon.className = 'type-edit-icon';
        editIcon.textContent = '✏';
        editIcon.title = `Edit ${type.label} type definition`;
        editIcon.addEventListener('click', (e) => {
            e.stopPropagation();
            typeDefinitionWindow.open(type.type, {
                name: type.type,
                label: type.label,
                color: type.color || '#666666',
                opacity: type.opacity ?? undefined,
                deprecated: type.deprecated || false,
                rich_string_fields: type.rich_string_fields || [],
                array_fields: type.array_fields || []
            });
        });
        item.appendChild(editIcon);

        typeAttestationsContainer.appendChild(item);
    });
}

// Build isolated nodes toggle (DRY - single source of truth)
export function buildIsolatedToggle(): void {
    const controlsContainer = document.getElementById('controls');
    if (!controlsContainer) return;

    // Check if already exists
    if (document.getElementById('isolated-toggle')) return;

    const toggle = document.createElement('label');
    toggle.id = 'isolated-toggle';
    toggle.innerHTML = `
        <input type="checkbox" id="hide-isolated" checked>
        <span>${UI_TEXT.ISOLATED_NODES}</span>
    `;

    controlsContainer.appendChild(toggle);
}

// Initialize legenda click handlers for node type visibility toggle
// renderGraphFn: function to call when graph needs re-rendering
// graphData: optional graph data with meta.node_types from backend
export function initTypeAttestations(
    renderGraphFn: (data: GraphData) => void,
    graphData: GraphData | null = null
): void {
    // Clean up old listeners before adding new ones
    eventListeners.removeAll();

    // Build UI components first (passing graphData for dynamic types)
    buildTypeAttestations(graphData);
    buildIsolatedToggle();

    const legendaItems = document.querySelectorAll('.type-attestation-item');

    legendaItems.forEach((item: Element) => {
        const htmlItem = item as HTMLElement;
        htmlItem.style.transition = 'opacity 0.2s ease';

        const typeNameSpan = item.querySelector('.type-attestation-name') as HTMLElement | null;
        const revealButton = item.querySelector('.reveal-related') as HTMLElement | null;

        if (!typeNameSpan) return;

        // Setup event handlers using helper functions
        setupTypeNameHandler(htmlItem, typeNameSpan);
        if (revealButton) {
            setupRevealButtonHandler(htmlItem, revealButton, renderGraphFn);
        }
    });

    // Initialize isolated node toggle
    const isolatedCheckbox = document.getElementById('hide-isolated') as HTMLInputElement | null;
    if (isolatedCheckbox) {
        const handler = function(this: HTMLInputElement): void {
            setHideIsolated(this.checked);

            // Phase 2: Send visibility preference to backend
            sendMessage({
                type: 'visibility',
                action: 'toggle_isolated',
                hidden: this.checked
            });

            // Note: No longer calling renderGraphFn here - backend will send updated graph
        };
        eventListeners.add(isolatedCheckbox, 'change', handler as EventListener);
    }
}

// Cleanup function for when legenda is destroyed
export function cleanupTypeAttestations(): void {
    eventListeners.removeAll();
}