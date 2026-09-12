/**
 * QntxClient — Unified Connection Abstraction
 *
 * Facade: re-exports from client/ submodules.
 * No module-scope side effects — avoids circular dependency issues with the bundler.
 * Singleton creation and wiring happens in the leaf submodules themselves.
 */

// ── URL ──
export { backendUrl, backendWsUrl, backendPath } from './client/url';

// importScript is in client/url and is imported from there, not through here.
// A module this facade transitively reaches cannot take a new symbol out of it
// without the cycle biting: re-exporting it turned every canvas test into
// "cannot access X before initialization". It is the one to use for a script —
// see the note on backendPath.

// ── Connectivity + Auth ──
export { connectivity } from './client/connectivity';
export type { Admission, ConnectivityState, ConnectivityManager, Failure, FailureSource } from './client/connectivity';

// ── HTTP ──
export { apiFetch, apiJson } from './client/http';
export { assertOk, jsonBody, stripProtocol, extractHttpStatus } from './http-utils';

// ── WebSocket ──
export { connectWebSocket, sendMessage, registerHandler, unregisterHandler, validateBackendURL, routeMessage } from './client/ws';
