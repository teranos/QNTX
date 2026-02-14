/**
 * Core type definitions for QNTX Web UI
 */

// ============================================================================
// Build Information
// ============================================================================

export interface BuildInfo {
  version: string;
  commit: string;
  build_time?: string;
}

// ============================================================================
// State Management Types
// ============================================================================

export interface AppState {
  currentVerbosity: number;
  logBuffer: (LogEntry | HTMLDivElement)[];
  progressBuffer: (ProgressEvent | HTMLDivElement)[];
  currentQuery: string;
}

export interface SessionData {
  query?: string;
  verbosity?: number;
  timestamp: number;
}

export interface LogEntry {
  timestamp: number;
  level: 'info' | 'warn' | 'error' | 'debug';
  message: string;
  source?: string;
}

export interface ProgressEvent {
  id: string;
  type: string;
  message: string;
  progress?: number;
  total?: number;
  timestamp: number;
}

// ============================================================================
// Configuration Types
// ============================================================================

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

export interface PanelState {
  visible: boolean;
  expanded?: boolean;
  position?: { x: number; y: number };
  size?: { width: number; height: number };
}

export interface EditorState {
  content: string;
  cursor?: { line: number; column: number };
  selection?: { start: number; end: number };
  version: number;
}

export interface LogMessage {
  level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';
  timestamp: string;
  logger: string;
  message: string;
  fields?: Record<string, unknown>;
}

export interface LogBatchData {
  data: {
    messages: LogMessage[];
  };
}

// ============================================================================
// Git Integration Types
// ============================================================================

export interface GitBranch {
  name: string;
  current: boolean;
  remote?: string;
  ahead?: number;
  behind?: number;
}

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

export interface Result<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  hasMore: boolean;
}
