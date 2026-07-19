/**
 * QntxClient — Unified Connection Abstraction
 *
 * Facade: re-exports from client/ submodules.
 * No module-scope side effects — avoids circular dependency issues with the bundler.
 * Singleton creation and wiring happens in the leaf submodules themselves.
 */

// ── URL ──
export { backendUrl, backendWsUrl, backendPath } from './client/url';

// ── Connectivity + Auth ──
export { connectivity } from './client/connectivity';
export type { ConnectivityState, ConnectivityManager, Failure, FailureSource } from './client/connectivity';

// ── HTTP ──
export { apiFetch, apiJson } from './client/http';
export { assertOk, jsonBody, stripProtocol, extractHttpStatus } from './http-utils';

// ── WebSocket ──
export { connectWebSocket, sendMessage, registerHandler, unregisterHandler, validateBackendURL, routeMessage } from './client/ws';
