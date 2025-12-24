/**
 * D3.js specific type definitions for graph visualization
 * Extends core types with D3 simulation properties
 */

import type * as d3 from 'd3';
import type { Node, Link } from './core';

// ============================================================================
// D3 Extended Types
// ============================================================================

/**
 * D3 Node with simulation properties
 * Extends our Node type with D3's SimulationNodeDatum
 */
export interface D3Node extends Node, d3.SimulationNodeDatum {
  // D3 adds these during simulation
  index?: number;
  vx?: number;
  vy?: number;
  // Custom rendering properties
  radius?: number;
  color?: string;
}

/**
 * D3 Link with simulation properties
 * Extends our Link type with D3's SimulationLinkDatum
 */
export interface D3Link extends Link, d3.SimulationLinkDatum<D3Node> {
  // D3 converts source/target to node references during simulation
  source: string | D3Node;
  target: string | D3Node;
  // Custom rendering properties
  color?: string;
  width?: number;
  opacity?: number;
}

// ============================================================================
// D3 Selection Types
// ============================================================================

/**
 * SVG element selection types
 */
export type SVGSelection = d3.Selection<SVGSVGElement, unknown, HTMLElement, unknown>;
export type GroupSelection = d3.Selection<SVGGElement, unknown, HTMLElement, unknown>;
export type NodeSelection = d3.Selection<SVGCircleElement, D3Node, SVGGElement, unknown>;
export type LinkSelection = d3.Selection<SVGLineElement, D3Link, SVGGElement, unknown>;
export type TextSelection = d3.Selection<SVGTextElement, D3Node, SVGGElement, unknown>;

// ============================================================================
// D3 Force Simulation Types
// ============================================================================

/**
 * Force simulation type
 */
export type ForceSimulation = d3.Simulation<D3Node, D3Link>;

/**
 * Force types used in the simulation
 */
export interface Forces {
  link: d3.ForceLink<D3Node, D3Link>;
  charge: d3.ForceManyBody<D3Node>;
  center: d3.ForceCenter<D3Node>;
  collision: d3.ForceCollide<D3Node>;
  x?: d3.ForceX<D3Node>;
  y?: d3.ForceY<D3Node>;
}

// ============================================================================
// D3 Zoom & Transform Types
// ============================================================================

/**
 * Zoom behavior type
 */
export type ZoomBehavior = d3.ZoomBehavior<SVGSVGElement, unknown>;

/**
 * Zoom transform
 */
export type ZoomTransform = d3.ZoomTransform;

/**
 * Zoom event
 */
export type ZoomEvent = d3.D3ZoomEvent<SVGSVGElement, unknown>;

// ============================================================================
// D3 Drag Types
// ============================================================================

/**
 * Drag behavior for nodes
 */
export type DragBehavior = d3.DragBehavior<SVGCircleElement, D3Node, d3.SubjectPosition>;

/**
 * Drag event
 */
export type DragEvent = d3.D3DragEvent<SVGCircleElement, D3Node, d3.SubjectPosition>;

// ============================================================================
// Graph Renderer State
// ============================================================================

/**
 * Complete graph renderer state
 */
export interface GraphRendererState {
  // Core D3 objects
  simulation: ForceSimulation | null;
  svg: SVGSelection | null;
  g: GroupSelection | null;
  zoom: ZoomBehavior | null;

  // Data
  nodes: D3Node[];
  links: D3Link[];

  // Selections
  nodeSelection: NodeSelection | null;
  linkSelection: LinkSelection | null;
  labelSelection: TextSelection | null;

  // UI State
  hiddenNodes: Set<string>;
  selectedNodes: Set<string>;
  hoveredNode: string | null;

  // Transform state
  currentTransform: ZoomTransform | null;

  // Animation state
  animating: boolean;
  simulationRunning: boolean;
}

// ============================================================================
// Graph Update Options
// ============================================================================

/**
 * Options for updating the graph
 */
export interface GraphUpdateOptions {
  animate?: boolean;
  preservePositions?: boolean;
  centerOnUpdate?: boolean;
  resetZoom?: boolean;
  alphaTarget?: number;
  alphaDecay?: number;
}

// ============================================================================
// Graph Export Types
// ============================================================================

/**
 * Graph export data
 */
export interface GraphExport {
  nodes: Array<{
    id: string;
    label: string;
    type: string;
    x: number;
    y: number;
    metadata?: Record<string, unknown>;
  }>;
  links: Array<{
    source: string;
    target: string;
    type: string;
    label?: string;
  }>;
  transform: {
    x: number;
    y: number;
    k: number;
  };
}

// ============================================================================
// D3 Event Handlers
// ============================================================================

/**
 * Node event handlers
 */
export interface NodeEventHandlers {
  click?: (event: MouseEvent, node: D3Node) => void;
  dblclick?: (event: MouseEvent, node: D3Node) => void;
  contextmenu?: (event: MouseEvent, node: D3Node) => void;
  mouseenter?: (event: MouseEvent, node: D3Node) => void;
  mouseleave?: (event: MouseEvent, node: D3Node) => void;
  dragstart?: (event: DragEvent, node: D3Node) => void;
  drag?: (event: DragEvent, node: D3Node) => void;
  dragend?: (event: DragEvent, node: D3Node) => void;
}

/**
 * Link event handlers
 */
export interface LinkEventHandlers {
  click?: (event: MouseEvent, link: D3Link) => void;
  mouseenter?: (event: MouseEvent, link: D3Link) => void;
  mouseleave?: (event: MouseEvent, link: D3Link) => void;
}

// ============================================================================
// D3 Scale Types
// ============================================================================

/**
 * Color scale for node types
 */
export type ColorScale = d3.ScaleOrdinal<string, string>;

/**
 * Size scale for nodes
 */
export type SizeScale = d3.ScaleLinear<number, number>;

// ============================================================================
// Graph Statistics
// ============================================================================

/**
 * Graph statistics
 */
export interface GraphStatistics {
  nodeCount: number;
  linkCount: number;
  nodeTypes: Map<string, number>;
  linkTypes: Map<string, number>;
  avgDegree: number;
  maxDegree: number;
  components: number;
  density: number;
}

// ============================================================================
// Layout Options
// ============================================================================

/**
 * Graph layout configuration
 */
export interface LayoutConfig {
  type: 'force' | 'radial' | 'hierarchical' | 'circular';
  options?: {
    // Force layout options
    linkDistance?: number | ((link: D3Link) => number);
    chargeStrength?: number | ((node: D3Node) => number);
    collisionRadius?: number | ((node: D3Node) => number);
    centerForce?: number;

    // Radial layout options
    radius?: number;
    angleOffset?: number;

    // Hierarchical layout options
    levelHeight?: number;
    nodeWidth?: number;

    // Circular layout options
    startAngle?: number;
    endAngle?: number;
  };
}