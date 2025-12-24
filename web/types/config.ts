/**
 * Configuration type definitions for QNTX Web UI
 * Types for application configuration and settings
 */

import { GraphPhysics, GraphStyles, NodeType, UIText } from './core';

// ============================================================================
// Application Configuration
// ============================================================================

/**
 * Main application configuration
 */
export interface AppConfig {
  // UI Configuration
  ui: UIConfig;

  // Graph Configuration
  graph: GraphConfig;

  // WebSocket Configuration
  websocket: WebSocketConnectionConfig;

  // Editor Configuration
  editor: EditorConfig;

  // Feature flags
  features: FeatureFlags;

  // API Configuration
  api: APIConfig;
}

/**
 * UI configuration
 */
export interface UIConfig {
  theme: 'light' | 'dark' | 'auto';
  language: string;
  dateFormat: string;
  timeFormat: string;
  maxLogs: number;
  maxProgress: number;
  animations: boolean;
  compactMode: boolean;
  showDebugInfo: boolean;
  defaultPanelStates: Record<string, boolean>;
  text: UIText;
}

/**
 * Graph configuration
 */
export interface GraphConfig {
  physics: GraphPhysics;
  styles: GraphStyles;
  nodeTypes: NodeType[];
  defaultNodeColor: string;
  defaultLinkColor: string;
  showLabels: boolean;
  labelThreshold: number;
  autoCenter: boolean;
  enableClustering: boolean;
  clusterThreshold: number;
}

/**
 * WebSocket connection configuration
 */
export interface WebSocketConnectionConfig {
  url: string;
  reconnect: boolean;
  reconnectDelay: number;
  maxReconnectAttempts: number;
  heartbeatInterval: number;
  messageTimeout: number;
  binaryType: 'blob' | 'arraybuffer';
}

/**
 * Editor configuration
 */
export interface EditorConfig {
  theme: string;
  fontSize: number;
  fontFamily: string;
  tabSize: number;
  insertSpaces: boolean;
  lineNumbers: boolean;
  lineWrapping: boolean;
  highlightActiveLine: boolean;
  highlightSelectionMatches: boolean;
  autoCloseBrackets: boolean;
  autoCloseTags: boolean;
  foldGutter: boolean;
  gutters: string[];
  keyMap: 'default' | 'vim' | 'emacs';
  autocomplete: AutocompleteConfig;
  linting: LintingConfig;
}

/**
 * Autocomplete configuration
 */
export interface AutocompleteConfig {
  enabled: boolean;
  delay: number;
  minLength: number;
  maxResults: number;
  caseSensitive: boolean;
  fuzzyMatch: boolean;
}

/**
 * Linting configuration
 */
export interface LintingConfig {
  enabled: boolean;
  onType: boolean;
  delay: number;
  severityThreshold: 'error' | 'warning' | 'info' | 'hint';
}

/**
 * Feature flags
 */
export interface FeatureFlags {
  enableLSP: boolean;
  enableAutoSave: boolean;
  enableCollaboration: boolean;
  enableExperimentalFeatures: boolean;
  enableOfflineMode: boolean;
  enableMetrics: boolean;
  enableAnalytics: boolean;
  debugMode: boolean;
}

/**
 * API configuration
 */
export interface APIConfig {
  baseUrl: string;
  timeout: number;
  retryAttempts: number;
  retryDelay: number;
  headers: Record<string, string>;
}

// ============================================================================
// Settings Types
// ============================================================================

/**
 * User settings
 */
export interface UserSettings {
  profile: UserProfile;
  preferences: UserPreferences;
  shortcuts: KeyboardShortcuts;
  workspace: WorkspaceSettings;
}

/**
 * User profile
 */
export interface UserProfile {
  id: string;
  name: string;
  email: string;
  avatar?: string;
  role: string;
  permissions: string[];
}

/**
 * User preferences
 */
export interface UserPreferences {
  theme: 'light' | 'dark' | 'auto';
  language: string;
  notifications: NotificationSettings;
  privacy: PrivacySettings;
}

/**
 * Notification settings
 */
export interface NotificationSettings {
  desktop: boolean;
  sound: boolean;
  email: boolean;
  types: {
    errors: boolean;
    warnings: boolean;
    info: boolean;
    updates: boolean;
    mentions: boolean;
  };
}

/**
 * Privacy settings
 */
export interface PrivacySettings {
  shareAnalytics: boolean;
  shareCrashReports: boolean;
  saveHistory: boolean;
  savePreferences: boolean;
}

/**
 * Keyboard shortcuts
 */
export interface KeyboardShortcuts {
  global: Record<string, string>;
  editor: Record<string, string>;
  graph: Record<string, string>;
  panels: Record<string, string>;
}

/**
 * Workspace settings
 */
export interface WorkspaceSettings {
  defaultQuery: string;
  defaultVerbosity: number;
  autoLoadLast: boolean;
  maxRecentQueries: number;
  recentQueries: RecentQuery[];
  savedQueries: SavedQuery[];
  layouts: LayoutPreset[];
}

/**
 * Recent query
 */
export interface RecentQuery {
  query: string;
  timestamp: number;
  resultCount?: number;
  executionTime?: number;
}

/**
 * Saved query
 */
export interface SavedQuery {
  id: string;
  name: string;
  description?: string;
  query: string;
  tags: string[];
  created: number;
  modified: number;
  shared: boolean;
}

/**
 * Layout preset
 */
export interface LayoutPreset {
  id: string;
  name: string;
  panels: Record<string, PanelLayout>;
  graphTransform?: { x: number; y: number; k: number };
}

/**
 * Panel layout
 */
export interface PanelLayout {
  visible: boolean;
  position?: { x: number; y: number };
  size?: { width: number; height: number };
  collapsed?: boolean;
  order?: number;
}

// ============================================================================
// Environment Configuration
// ============================================================================

/**
 * Environment configuration
 */
export interface EnvironmentConfig {
  mode: 'development' | 'staging' | 'production';
  debug: boolean;
  apiUrl: string;
  wsUrl: string;
  version: string;
  buildTime: string;
  commit: string;
}

// ============================================================================
// Constants
// ============================================================================

/**
 * Application constants
 */
export interface AppConstants {
  // Limits
  MAX_LOGS: number;
  MAX_PROGRESS: number;
  MAX_QUERY_LENGTH: number;
  MAX_NODES_DISPLAY: number;
  MAX_LINKS_DISPLAY: number;
  MAX_FILE_SIZE: number;
  MAX_UPLOAD_SIZE: number;

  // Timings (ms)
  DEBOUNCE_DELAY: number;
  THROTTLE_DELAY: number;
  ANIMATION_DURATION: number;
  TOOLTIP_DELAY: number;
  NOTIFICATION_DURATION: number;

  // Network
  REQUEST_TIMEOUT: number;
  RECONNECT_DELAY: number;
  MAX_RECONNECT_ATTEMPTS: number;

  // Storage keys
  SESSION_STORAGE_KEY: string;
  LOCAL_STORAGE_PREFIX: string;
  COOKIE_PREFIX: string;

  // Regex patterns
  QUERY_PATTERN: RegExp;
  EMAIL_PATTERN: RegExp;
  URL_PATTERN: RegExp;
}

// ============================================================================
// Export Default Configuration
// ============================================================================

/**
 * Default configuration values
 */
export const defaultConfig: Partial<AppConfig> = {
  ui: {
    theme: 'auto',
    language: 'en',
    dateFormat: 'YYYY-MM-DD',
    timeFormat: 'HH:mm:ss',
    maxLogs: 100,
    maxProgress: 50,
    animations: true,
    compactMode: false,
    showDebugInfo: false,
    defaultPanelStates: {},
    text: {
      CLEAR_SESSION: 'Clear Session',
      CONFIRM_CLEAR: 'Are you sure?',
      NO_DATA: 'No data available',
      LOADING: 'Loading...',
      ERROR_PREFIX: 'Error: ',
      CONNECTION_LOST: 'Connection lost',
      CONNECTION_RESTORED: 'Connection restored'
    }
  },
  features: {
    enableLSP: true,
    enableAutoSave: true,
    enableCollaboration: false,
    enableExperimentalFeatures: false,
    enableOfflineMode: false,
    enableMetrics: true,
    enableAnalytics: false,
    debugMode: false
  }
};