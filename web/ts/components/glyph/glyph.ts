/**
 * Glyph - The universal primitive
 *
 * A glyph is exactly ONE DOM element for its entire lifetime.
 * It can morph between different visual states (dot, proximity, window, canvas, modal, etc.)
 * through smooth animations, but the element identity never changes.
 *
 * All glyphs are container glyphs - they can hold child glyphs in various layout strategies.
 *
 * This file contains just the Glyph interface and shared constants.
 * Manifestation logic lives in ./manifestations/*
 */

export interface Glyph {
    id: string;
    title: string;
    renderContent: () => HTMLElement;    // Function to render content

    // Manifestation configuration
    manifestationType?: 'window' | 'fullscreen' | 'canvas' | 'modal' | 'ax';  // Default: 'window'
    initialWidth?: string;               // Initial dimensions (e.g., "800px")
    initialHeight?: string;
    defaultX?: number;                   // Default position
    defaultY?: number;

    // Lifecycle hooks
    onClose?: () => void;

    // Fractal container support - all glyphs can contain children
    children?: Glyph[];                  // Child glyphs this glyph contains
    layoutStrategy?: 'flow' | 'grid' | 'custom';  // How to layout children (default: flow)
    onSpawnMenu?: () => string[];        // Symbols that can be spawned inside (right-click)

    // Grid position metadata (for grid-layout children)
    gridX?: number;                      // Grid cell X position
    gridY?: number;                      // Grid cell Y position
    symbol?: string;                     // Symbol to display (for grid children)
}

// Function to check if user prefers reduced motion
function getPrefersReducedMotion(): boolean {
    if (typeof window !== 'undefined' && window.matchMedia) {
        return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    }
    return false; // Default to animations enabled in test environment
}

// Animation durations in milliseconds
export const MAXIMIZE_DURATION_MS = 350;  // Base duration for dot → manifestation
export const MINIMIZE_DURATION_MS = 200;  // Base duration for manifestation → dot

// Get actual durations considering reduced motion preference
export function getMaximizeDuration(): number {
    return getPrefersReducedMotion() ? 0 : MAXIMIZE_DURATION_MS;
}

export function getMinimizeDuration(): number {
    return getPrefersReducedMotion() ? 0 : MINIMIZE_DURATION_MS;
}

// Window manifestation constants (used by manifestations/window.ts)
export const DEFAULT_WINDOW_WIDTH = '800px';
export const DEFAULT_WINDOW_HEIGHT = '600px';
export const WINDOW_BORDER_RADIUS = '8px';
export const WINDOW_BOX_SHADOW = '0 8px 32px rgba(0, 0, 0, 0.3)';

// Window chrome dimensions
export const TITLE_BAR_HEIGHT = '32px';
export const TITLE_BAR_PADDING = '0 12px';
export const WINDOW_BUTTON_SIZE = '24px';
export const CONTENT_PADDING = '16px';
