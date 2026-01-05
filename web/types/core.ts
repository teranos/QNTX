/**
 * Core type definitions for QNTX Web UI
 * These are the fundamental data structures used throughout the application
 */

// Import generated graph types from Go source (single source of truth)
import type {
  Graph,
  Node as GeneratedNode,
  Link as GeneratedLink,
  Meta,
  NodeTypeInfo as GeneratedNodeTypeInfo,
  RelationshipTypeInfo as GeneratedRelationshipTypeInfo,
  Stats,
} from '../../types/generated/typescript/graph';

// Re-export generated types with frontend names
export type { Stats };
export type GraphData = Graph;
export type GraphMeta = Meta;

// ============================================================================
// Build Information
// ============================================================================

/**
 * Server build information
 */
export interface BuildInfo {
  version: string;
  commit: string;
  build_time?: string;
}

// ============================================================================
// State Management Types
// ============================================================================

/**
 * Main application state that persists across sessions
 */
export interface AppState {
  currentVerbosity: number;
  logBuffer: (LogEntry | HTMLDivElement)[];  // TODO: Refactor to only use LogEntry
  progressBuffer: (ProgressEvent | HTMLDivElement)[];  // TODO: Refactor to only use ProgressEvent
  currentQuery: string;
  currentGraphData: GraphData | null;
  currentTransform: Transform | null;
}

/**
 * Session data stored in localStorage
 */
export interface SessionData {
  query?: string;
  verbosity?: number;
  graphData?: GraphData | null;
  transform?: Transform | null;
  timestamp: number;
}

/**
 * Log entry structure
 */
export interface LogEntry {
  timestamp: number;
  level: 'info' | 'warn' | 'error' | 'debug';
  message: string;
  source?: string;
}

/**
 * Progress event for long-running operations
 */
export interface ProgressEvent {
  id: string;
  type: string;
  message: string;
  progress?: number;
  total?: number;
  timestamp: number;
}

// ============================================================================
// Graph Data Structures
// ============================================================================

/**
 * Graph node representing an entity
 * Extends generated type with D3 simulation and UI properties
 */
export interface Node extends Omit<GeneratedNode, 'visible'> {
  // D3 simulation properties
  x?: number;
  y?: number;
  vx?: number;
  vy?: number;
  fx?: number | null;  // Fixed x position
  fy?: number | null;  // Fixed y position
  // UI properties
  hidden?: boolean;
  visible?: boolean;  // Phase 2: Backend controls visibility (override required from generated)
  selected?: boolean;
  radius?: number;
}

/**
 * Graph link representing a relationship
 * Extends generated type with D3 simulation and UI properties
 */
export interface Link extends Omit<GeneratedLink, 'source' | 'target' | 'value'> {
  source: string | Node;  // D3 converts string IDs to Node references during simulation
  target: string | Node;  // D3 converts string IDs to Node references during simulation
  weight?: number;        // Alias for D3's 'value' field
  // UI properties
  selected?: boolean;
}

/**
 * Node type information for legend
 * Re-export generated type
 */
export type NodeTypeInfo = GeneratedNodeTypeInfo;

/**
 * Relationship type information for physics configuration
 * Re-export generated type
 */
export type RelationshipTypeInfo = GeneratedRelationshipTypeInfo;

/**
 * Graph transform for zoom and pan
 */
export interface Transform {
  x: number;
  y: number;
  k: number;  // Scale factor
}

// ============================================================================
// Configuration Types
// ============================================================================

/**
 * Node type configuration
 */
export interface NodeType {
  key: string;
  label: string;
  color: string;
  count?: number;
  icon?: string;
}

/**
 * Graph physics configuration for D3 force simulation
 */
export interface GraphPhysics {
  LINK_DISTANCE: number;
  CHARGE_STRENGTH: number;
  CHARGE_MAX_DISTANCE: number;
  COLLISION_PADDING: number;
  DEFAULT_NODE_SIZE: number;
  ZOOM_MIN: number;
  ZOOM_MAX: number;
  CENTER_SCALE: number;
  ANIMATION_DURATION: number;
  FORCE_ALPHA_TARGET: number;
  FORCE_VELOCITY_DECAY: number;
}

/**
 * Graph visual styles
 */
export interface GraphStyles {
  NODE_OPACITY: number;
  NODE_STROKE_WIDTH: number;
  NODE_STROKE_COLOR: string;
  LINK_OPACITY: number;
  LINK_WIDTH: number;
  LINK_COLOR: string;
  SELECTED_STROKE_COLOR: string;
  SELECTED_STROKE_WIDTH: number;
  HOVER_OPACITY: number;
  DIMMED_OPACITY: number;
}

/**
 * UI text configuration
 */
export interface UIText {
  CLEAR_SESSION: string;
  CONFIRM_CLEAR: string;
  NO_DATA: string;
  LOADING: string;
  ERROR_PREFIX: string;
  CONNECTION_LOST: string;
  CONNECTION_RESTORED: string;
}

// ============================================================================
// Component State Types
// ============================================================================

/**
 * Panel visibility state
 */
export interface PanelState {
  visible: boolean;
  expanded?: boolean;
  position?: { x: number; y: number };
  size?: { width: number; height: number };
}

/**
 * Editor state
 */
export interface EditorState {
  content: string;
  cursor?: { line: number; column: number };
  selection?: { start: number; end: number };
  version: number;
}

/**
 * Log message from the backend
 */
export interface LogMessage {
  level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';
  timestamp: string;
  logger: string;
  message: string;
  fields?: Record<string, unknown>;
}

/**
 * Log batch data
 */
export interface LogBatchData {
  data: {
    messages: LogMessage[];
  };
}

// ============================================================================
// Git Integration Types
// ============================================================================

/**
 * Git branch information
 */
export interface GitBranch {
  name: string;
  current: boolean;
  remote?: string;
  ahead?: number;
  behind?: number;
}

/**
 * Git status
 */
export interface GitStatus {
  branch: string;
  dirty: boolean;
  ahead: number;
  behind: number;
  conflicts: string[];
  staged: string[];
  modified: string[];
  untracked: string[];
}

// ============================================================================
// AI Provider Types
// ============================================================================

/**
 * AI provider configuration
 */
export interface AIProvider {
  id: string;
  name: string;
  model: string;
  enabled: boolean;
  apiKey?: string;
  endpoint?: string;
  maxTokens?: number;
  temperature?: number;
}

// ============================================================================
// Utility Types
// ============================================================================

/**
 * Generic result type for async operations
 */
export interface Result<T> {
  success: boolean;
  data?: T;
  error?: string;
}

/**
 * Paginated response
 */
export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  hasMore: boolean;
}