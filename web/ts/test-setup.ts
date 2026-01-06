/**
 * Test setup - initializes DOM environment for tests
 */
import { Window } from 'happy-dom';

// Create happy-dom window and expose globals
const window = new Window();
const document = window.document;

// @ts-ignore - Expose globals for tests
globalThis.window = window;
// @ts-ignore
globalThis.document = document;
// @ts-ignore
globalThis.HTMLElement = window.HTMLElement;
// @ts-ignore
globalThis.localStorage = window.localStorage;
