/**
 * Language Server Protocol (LSP) type definitions
 * Types for ATS language server communication and CodeMirror integration
 */

// ============================================================================
// LSP Configuration
// ============================================================================

/**
 * LSP client configuration
 */
export interface LSPConfig {
  serverUri: string;
  rootUri: string;
  documentUri: string;
  languageId: string;
  workspaceFolders?: WorkspaceFolder[];
  initializationOptions?: unknown;
}

/**
 * Workspace folder
 */
export interface WorkspaceFolder {
  uri: string;
  name: string;
}

// ============================================================================
// Custom ATS Parse Protocol
// ============================================================================

/**
 * Parse request for ATS queries
 */
export interface ParseRequest {
  type: 'parse_request';
  query: string;
  line: number;
  cursor: number;
  timestamp: number;
  requestId?: string;
  context?: {
    previousQuery?: string;
    sessionId?: string;
  };
}

/**
 * Parse response with tokens and diagnostics
 */
export interface ParseResponse {
  type: 'parse_response';
  tokens: SemanticToken[];
  diagnostics: Diagnostic[];
  parse_state?: ATSParseState;
  requestId?: string;
  suggestions?: CompletionItem[];
}

/**
 * ATS parse state
 */
export interface ATSParseState {
  valid: boolean;
  complete: boolean;
  ast?: unknown;
  errors: ParseError[];
  warnings: ParseWarning[];
}

/**
 * Parse error
 */
export interface ParseError {
  message: string;
  range: Range;
  severity: 'error';
  code?: string;
}

/**
 * Parse warning
 */
export interface ParseWarning {
  message: string;
  range: Range;
  severity: 'warning';
  code?: string;
}

// ============================================================================
// Semantic Tokens
// ============================================================================

/**
 * Semantic token for syntax highlighting
 */
export interface SemanticToken {
  text: string;
  semantic_type: SemanticTokenType;
  range: Range;
  modifiers?: SemanticTokenModifier[];
}

/**
 * Semantic token types for ATS language
 */
export type SemanticTokenType =
  | 'keyword'
  | 'operator'
  | 'identifier'
  | 'string'
  | 'number'
  | 'comment'
  | 'variable'
  | 'function'
  | 'type'
  | 'namespace'
  | 'property'
  | 'parameter'
  | 'label'
  | 'punctuation'
  | 'whitespace'
  | 'invalid';

/**
 * Semantic token modifiers
 */
export type SemanticTokenModifier =
  | 'declaration'
  | 'definition'
  | 'readonly'
  | 'static'
  | 'deprecated'
  | 'abstract'
  | 'async'
  | 'modification'
  | 'documentation'
  | 'defaultLibrary';

// ============================================================================
// Positions and Ranges
// ============================================================================

/**
 * Position in a text document
 */
export interface Position {
  line: number;      // 0-based
  column: number;    // 0-based
  offset: number;    // 0-based character offset
}

/**
 * Range in a text document
 */
export interface Range {
  start: Position;
  end: Position;
}

/**
 * Location with URI
 */
export interface Location {
  uri: string;
  range: Range;
}

// ============================================================================
// Diagnostics
// ============================================================================

/**
 * Diagnostic severity levels
 */
export type DiagnosticSeverity = 'error' | 'warning' | 'info' | 'hint';

/**
 * Diagnostic message
 */
export interface Diagnostic {
  message: string;
  severity: DiagnosticSeverity;
  range: Range;
  source?: string;
  code?: string | number;
  tags?: DiagnosticTag[];
  relatedInformation?: DiagnosticRelatedInformation[];
  suggestions?: string[];
  data?: unknown;
}

/**
 * Diagnostic tags
 */
export type DiagnosticTag = 'unnecessary' | 'deprecated';

/**
 * Related diagnostic information
 */
export interface DiagnosticRelatedInformation {
  location: Location;
  message: string;
}

// ============================================================================
// Completions
// ============================================================================

/**
 * Completion item
 */
export interface CompletionItem {
  label: string;
  kind?: CompletionItemKind;
  detail?: string;
  documentation?: string | MarkupContent;
  sortText?: string;
  filterText?: string;
  insertText?: string;
  textEdit?: TextEdit;
  additionalTextEdits?: TextEdit[];
  command?: Command;
  data?: unknown;
}

/**
 * Completion item kinds
 */
export enum CompletionItemKind {
  Text = 1,
  Method = 2,
  Function = 3,
  Constructor = 4,
  Field = 5,
  Variable = 6,
  Class = 7,
  Interface = 8,
  Module = 9,
  Property = 10,
  Unit = 11,
  Value = 12,
  Enum = 13,
  Keyword = 14,
  Snippet = 15,
  Color = 16,
  File = 17,
  Reference = 18,
  Folder = 19,
  EnumMember = 20,
  Constant = 21,
  Struct = 22,
  Event = 23,
  Operator = 24,
  TypeParameter = 25,
}

/**
 * Text edit
 */
export interface TextEdit {
  range: Range;
  newText: string;
}

/**
 * Command
 */
export interface Command {
  title: string;
  command: string;
  arguments?: unknown[];
}

// ============================================================================
// Hover
// ============================================================================

/**
 * Hover response
 */
export interface Hover {
  contents: string | MarkupContent | MarkedString | MarkedString[];
  range?: Range;
}

/**
 * Markup content
 */
export interface MarkupContent {
  kind: 'plaintext' | 'markdown';
  value: string;
}

/**
 * Marked string (deprecated in favor of MarkupContent)
 */
export type MarkedString = string | { language: string; value: string };

// ============================================================================
// Signature Help
// ============================================================================

/**
 * Signature help
 */
export interface SignatureHelp {
  signatures: SignatureInformation[];
  activeSignature?: number;
  activeParameter?: number;
}

/**
 * Signature information
 */
export interface SignatureInformation {
  label: string;
  documentation?: string | MarkupContent;
  parameters?: ParameterInformation[];
  activeParameter?: number;
}

/**
 * Parameter information
 */
export interface ParameterInformation {
  label: string | [number, number];
  documentation?: string | MarkupContent;
}

// ============================================================================
// CodeMirror Integration Types
// ============================================================================

/**
 * CodeMirror language server configuration
 */
export interface CodeMirrorLSPConfig {
  serverUri: string;
  rootUri: string;
  documentUri: string;
  languageId: string;
  transport?: 'websocket' | 'worker' | 'iframe';
  workspaceFolders?: WorkspaceFolder[];
  documentText?: string;
}

/**
 * CodeMirror diagnostic adapter
 */
export interface CodeMirrorDiagnostic {
  from: number;
  to: number;
  severity: 'error' | 'warning' | 'info' | 'hint';
  message: string;
  source?: string;
  actions?: CodeMirrorAction[];
}

/**
 * CodeMirror action
 */
export interface CodeMirrorAction {
  name: string;
  apply: (view: unknown) => void;
}

// ============================================================================
// ATS Language-Specific Types
// ============================================================================

/**
 * ATS query structure
 */
export interface ATSQuery {
  subject?: string;
  predicate?: string;
  object?: string;
  context?: string;
  actors?: string[];
  filters?: ATSFilter[];
  limit?: number;
  offset?: number;
}

/**
 * ATS filter
 */
export interface ATSFilter {
  field: string;
  operator: 'eq' | 'ne' | 'gt' | 'lt' | 'gte' | 'lte' | 'in' | 'nin' | 'like' | 'nlike';
  value: unknown;
}

/**
 * ATS symbol information
 */
export interface ATSSymbol {
  name: string;
  kind: 'subject' | 'predicate' | 'object' | 'context' | 'actor' | 'function' | 'variable';
  range: Range;
  children?: ATSSymbol[];
}

// ============================================================================
// WebSocket Message Types for LSP
// ============================================================================

/**
 * LSP request message
 */
export interface LSPRequest {
  jsonrpc: '2.0';
  id: string | number;
  method: string;
  params?: unknown;
}

/**
 * LSP response message
 */
export interface LSPResponse {
  jsonrpc: '2.0';
  id: string | number;
  result?: unknown;
  error?: LSPError;
}

/**
 * LSP error
 */
export interface LSPError {
  code: number;
  message: string;
  data?: unknown;
}

/**
 * LSP notification message
 */
export interface LSPNotification {
  jsonrpc: '2.0';
  method: string;
  params?: unknown;
}