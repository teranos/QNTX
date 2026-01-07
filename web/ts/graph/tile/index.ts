// Tile - Primary user interface abstraction for QNTX graph nodes
// Tiles are how users interact with, explore, and understand the knowledge graph

// Re-export all tile-related functionality
export * from './constants.ts';
export * from './state.ts';
export * from './controls.ts';
export * from './rendering.ts';
export * from './interactions.ts';

// TODO: Complete tile module extraction
// export * from './transitions.ts';  - Expand, collapse, fade animations

/**
 * Tile Module Architecture
 *
 * Tiles represent nodes in the QNTX knowledge graph. They are the primary
 * way users interact with attestations, relationships, and metadata.
 *
 * Core Responsibilities:
 * - Visual Rendering: Rect, label, type indicator, metadata preview
 * - State Management: Normal, focused, dimmed, hovered, dragging
 * - User Interactions: Click to focus, hover for details, drag to rearrange
 * - Decorations: Header with command symbols, footer with connection stats
 * - Smooth Transitions: Expand/collapse, fade in/out, position changes
 * - Dimension Control: Default size, zoom-compensated focused size
 *
 * Design Principles:
 * - Single Source of Truth: All tile constants defined once
 * - Functional Style: Pure functions over D3 selections
 * - Clear Separation: Rendering, state, interactions, decorations
 * - Accessible: ARIA labels, keyboard navigation, screen reader support
 */
