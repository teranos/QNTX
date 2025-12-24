// Shared configuration and state for the graph viewer

import type { AppState, GraphPhysics, GraphStyles, UIText } from '../types/core';

// Buffer limits
export const MAX_LOGS: number = 1000;
export const MAX_PROGRESS: number = 100;

// UI text constants (single source of truth)
// Virtue #3: Semantic Clarity - Use SEG symbols consistently
export const UI_TEXT: UIText & {
    LEGENDA_TITLE: string;
    REVEAL_TOOLTIP: (label: string) => string;
    ISOLATED_NODES: string;
} = {
    // Core UI text from interface
    CLEAR_SESSION: 'Clear Session',
    CONFIRM_CLEAR: 'Are you sure?',
    NO_DATA: 'No data available',
    LOADING: 'Loading...',
    ERROR_PREFIX: 'Error: ',
    CONNECTION_LOST: 'Connection lost',
    CONNECTION_RESTORED: 'Connection restored',

    // Extended UI text specific to this app
    LEGENDA_TITLE: 'Tiles <span style="font-size: 9px; font-weight: normal; color: #666;">(click to toggle)</span>',
    REVEAL_TOOLTIP: (label: string): string => `⚠️ Experimental: Reveal nodes connected to ${label} (feature in development)`,
    ISOLATED_NODES: '⊖ Hide isolated nodes'  // ⊖ = subtract/hide symbol
};

// REMOVED: Node type registry moved to backend (internal/graph/ats_graph.go)
// Backend is now the single source of truth for node types, labels, and colors
// Frontend receives this data via GraphMeta.node_types and just renders it

// Graph physics constants (Virtue #5: Prudence - named constants)
export const GRAPH_PHYSICS: GraphPhysics & {
    LINK_DISTANCE: number;
    CHARGE_STRENGTH: number;
    CHARGE_MAX_DISTANCE: number;
    COLLISION_PADDING: number;
    DEFAULT_NODE_SIZE: number;
    ZOOM_MIN: number;
    ZOOM_MAX: number;
    CENTER_SCALE: number;
    ANIMATION_DURATION: number;
} = {
    LINK_DISTANCE: 100,
    CHARGE_STRENGTH: -300,
    CHARGE_MAX_DISTANCE: 400,
    COLLISION_PADDING: 15,
    DEFAULT_NODE_SIZE: 10,
    ZOOM_MIN: 0.1,
    ZOOM_MAX: 10,
    CENTER_SCALE: 0.7,
    ANIMATION_DURATION: 750,
    // Additional properties for interface compliance
    FORCE_ALPHA_TARGET: 0,
    FORCE_VELOCITY_DECAY: 0.4
};

// Graph visual styling constants
export const GRAPH_STYLES: GraphStyles & {
    // Extended style properties
    NODE_STROKE_COLOR: string;
    NODE_STROKE_WIDTH: number;
    NODE_TEXT_SIZE: number;
    NODE_TEXT_COLOR: string;
    LINK_COLOR: string;
    LINK_OPACITY: number;
    TOOLTIP_PADDING: string;
    TOOLTIP_BG: string;
    TOOLTIP_COLOR: string;
    TOOLTIP_FONT_SIZE: number;
    CONTROL_OFFSET_TOP: number;
    CONTROL_OFFSET_LEFT: number;
    CONTROL_OFFSET_RIGHT: number;
    LEGEND_MARGIN_TOP: number;
    LEGEND_PADDING_TOP: number;
    CONTROL_PADDING: number;
    CONTROL_BG: string;
    CONTROL_BORDER: string;
    META_MAX_WIDTH: number;
    META_FONT_SIZE: number;
    META_COLOR: string;
} = {
    // Interface properties
    NODE_OPACITY: 1.0,
    NODE_STROKE_WIDTH: 2,
    NODE_STROKE_COLOR: '#fff',
    LINK_OPACITY: 0.6,
    LINK_WIDTH: 1,
    LINK_COLOR: '#ddd',
    SELECTED_STROKE_COLOR: '#ff6600',
    SELECTED_STROKE_WIDTH: 3,
    HOVER_OPACITY: 0.8,
    DIMMED_OPACITY: 0.3,

    // Extended properties
    NODE_TEXT_SIZE: 10,
    NODE_TEXT_COLOR: '#000',

    // Tooltip styles
    TOOLTIP_PADDING: '8px 10px',
    TOOLTIP_BG: '#000',
    TOOLTIP_COLOR: '#fff',
    TOOLTIP_FONT_SIZE: 11,

    // Layout spacing
    CONTROL_OFFSET_TOP: 16,
    CONTROL_OFFSET_LEFT: 16,
    CONTROL_OFFSET_RIGHT: 16,
    LEGEND_MARGIN_TOP: 12,
    LEGEND_PADDING_TOP: 12,

    // Controls
    CONTROL_PADDING: 8,
    CONTROL_BG: '#f8f8f8',
    CONTROL_BORDER: '#ddd',

    // Meta overlay
    META_MAX_WIDTH: 300,
    META_FONT_SIZE: 11,
    META_COLOR: '#666'
};

// Shared state
export const state: AppState = {
    currentVerbosity: 2,  // Default: Debug (-vv)
    logBuffer: [],
    progressBuffer: [],
    currentQuery: '',
    currentGraphData: null,
    currentTransform: null
};